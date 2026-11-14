package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/dbconfig"
	"github.com/soajs/soajs.controller.go/internal/middleware"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/mongo"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"github.com/soajs/soajs.controller.go/pkg/urac"
	"go.uber.org/zap"
)

// Controller manages the gateway components
type Controller struct {
	cfg       *config.Config
	logger    *zap.Logger
	registry  registry.Registry
	provision provision.Provision
	awareness awareness.Awareness
	urac      *urac.Driver
	mongo     *mongo.Client // the provision database, from the profile

	clientsMu sync.Mutex
	clients   map[string]*mongo.Client // one connection per cluster, by connection string
}

// NewController reads the profile, connects to the provision database and creates the gateway
// components.
func NewController(cfg *config.Config, logger *zap.Logger) (*Controller, error) {
	if cfg.Profile == "" {
		return nil, fmt.Errorf("SOAJS_PROFILE is required")
	}
	profile, err := dbconfig.LoadProfile(cfg.Profile)
	if err != nil {
		return nil, err
	}
	ctrl := &Controller{
		cfg:     cfg,
		logger:  logger,
		clients: map[string]*mongo.Client{},
	}
	provisionDB := profile.ProvisionDB(strings.ToLower(cfg.Environment), 0)
	ctrl.mongo, err = ctrl.clientFor(provisionDB)
	if err != nil {
		return nil, fmt.Errorf("provision database: %w", err)
	}

	ctrl.registry = registry.NewMongoRegistry(ctrl.mongo.Database(), profile, logger)

	// Initialize awareness
	baseAwareness := awareness.NewAwareness(cfg.Awareness, ctrl.registry, logger)

	// Check if running in HA mode (any value means HA is enabled, matching Node.js behavior)
	deployHA := os.Getenv("SOAJS_DEPLOY_HA")
	if deployHA != "" {
		logger.Info("HA mode enabled, initializing HA awareness",
			zap.String("SOAJS_DEPLOY_HA", deployHA))
		haAwareness, err := awareness.NewHAAwareness(baseAwareness)
		if err != nil {
			logger.Warn("failed to initialize HA awareness, falling back to default",
				zap.Error(err),
			)
			ctrl.awareness = baseAwareness
		} else {
			ctrl.awareness = haAwareness
		}
	} else {
		ctrl.awareness = baseAwareness
	}

	// The urac database is set in Setup, from the registry's masterDB
	ctrl.urac = urac.NewDriverWithConfig(ctrl.mongo, logger, urac.Config{
		HashIterations: 12,
		Environment:    cfg.Environment,
	})

	return ctrl, nil
}

// clientFor returns a client on a database configuration's database, sharing one connection per
// cluster.
func (c *Controller) clientFor(db *dbconfig.DB) (*mongo.Client, error) {
	uri, err := db.URI()
	if err != nil {
		return nil, err
	}
	c.clientsMu.Lock()
	defer c.clientsMu.Unlock()
	if client, ok := c.clients[uri]; ok {
		return client.WithDatabase(db.DatabaseName()), nil
	}
	opts, err := db.ClientOptions()
	if err != nil {
		return nil, err
	}
	client, err := mongo.Connect(opts, db.DatabaseName())
	if err != nil {
		return nil, fmt.Errorf("database %s: %w", db.Name, err)
	}
	c.clients[uri] = client
	return client, nil
}

// coreClient returns a client on coreDB[name], or on provision when the environment does not
// declare that database.
func (c *Controller) coreClient(regData *types.RegistryData, name string) (*mongo.Client, error) {
	if db, ok := regData.CoreDB[name]; ok {
		return c.clientFor(db)
	}
	return c.mongo, nil
}

// Setup configures the router with middleware and routes
func (c *Controller) Setup(r *chi.Mux) error {
	if err := c.registry.Load(c.cfg.Environment); err != nil {
		return err
	}
	regData := c.registry.Get()

	// Tokens live in the environment's oauth database when it declares one, else in provision
	if _, ok := regData.CoreDB["oauth"]; ok {
		oauthClient, err := c.clientFor(regData.CoreDB["oauth"])
		if err != nil {
			return fmt.Errorf("oauth database: %w", err)
		}
		c.provision = provision.NewProvisionWithOAuthClient(c.mongo, oauthClient, c.logger, c.cfg.Environment)
	} else {
		c.provision = provision.NewProvision(c.mongo, c.logger, c.cfg.Environment)
	}
	keyConfig := regData.ServiceConfig.Key
	c.provision.SetKeyConfig(keyConfig.Algorithm, keyConfig.Password)

	if err := c.provision.LoadProvision(); err != nil {
		return err
	}
	c.logger.Info("service provision loaded")

	c.configureUrac(regData)

	// Initialize traffic model based on registry config
	c.initTrafficModel(regData)

	// Setup base middleware (chi built-in)
	r.Use(chimw.RequestID)
	r.Use(chimw.Recoverer)

	// Setup SOAJS middleware chain
	r.Use(middleware.Context(c.logger, c.cfg.Environment))
	r.Use(middleware.CORS(c.registry))
	r.Use(middleware.Favicon())
	r.Use(middleware.Response())
	r.Use(middleware.Enhancer())
	r.Use(middleware.IP2BanWithRegistry(c.registry))
	r.Use(middleware.MaintenanceMode(c.registry))
	r.Use(middleware.URL(c.registry))
	r.Use(middleware.Awareness(c.awareness))
	r.Use(middleware.AwarenessEnv(c.awareness, c.registry, c.cfg.Environment))
	r.Use(middleware.Key(c.provision, c.registry, c.cfg.Environment))
	r.Use(middleware.KeyACL())
	r.Use(middleware.GotoService(c.provision, c.registry, c.awareness))
	r.Use(middleware.MT(c.provision, c.registry, c.urac, c.cfg.Environment))
	r.Use(middleware.SoajsRoutesSetup(c.urac)) // Setup /soajs/* routes to require OAuth
	r.Use(middleware.OAuth(c.provision))
	r.Use(middleware.UserACL())
	r.Use(middleware.SoajsRoutes(c.provision)) // Handle /soajs/* routes after OAuth/UserACL populate context
	r.Use(middleware.Traffic(c.registry))
	r.Use(middleware.LastSeen(c.registry, c.awareness))
	r.Use(middleware.Monitor(c.registry, c.awareness))

	// Final handler - proxy to service
	r.HandleFunc("/*", c.handleRequest)

	return nil
}

// handleRequest is the final handler that proxies requests to backend services
func (c *Controller) handleRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	soajsCtx := middleware.GetSOAJSContext(ctx)

	if soajsCtx == nil || soajsCtx.Controller == nil || soajsCtx.Controller.Gotoservice == nil {
		middleware.WriteError(w, http.StatusBadGateway, config.ErrCodeServiceNotFound, "Service not configured")
		return
	}

	// Execute the proxy
	soajsCtx.Controller.Gotoservice(w, r)
}

// Registry returns the registry instance
func (c *Controller) Registry() registry.Registry {
	return c.registry
}

// Provision returns the provision instance
func (c *Controller) Provision() provision.Provision {
	return c.provision
}

// Awareness returns the awareness instance
func (c *Controller) Awareness() awareness.Awareness {
	return c.awareness
}

// ReloadProvision reloads the provision data
func (c *Controller) ReloadProvision() error {
	return c.provision.LoadProvision()
}

// ReloadRegistry reloads the registry data
func (c *Controller) ReloadRegistry() error {
	return c.registry.Reload()
}

// configureUrac points the urac driver at coreDB[masterDB], masterDB being the custom registry
// urac value.masterDB (docs/data-schema/urac.md). Without it, urac lookups are not configured.
func (c *Controller) configureUrac(regData *types.RegistryData) {
	masterDB := ""
	if entry, ok := regData.CustomRegistry["urac"].(map[string]interface{}); ok {
		if value, ok := entry["value"].(map[string]interface{}); ok {
			masterDB, _ = value["masterDB"].(string)
		}
	}
	db, ok := regData.CoreDB[masterDB]
	if masterDB == "" || !ok {
		c.logger.Error("configuration error: the custom registry urac masterDB is missing or is not a core database of the environment",
			zap.String("masterDB", masterDB))
		return
	}
	client, err := c.clientFor(db)
	if err != nil {
		c.logger.Error("configuration error: unable to connect to the urac database", zap.String("masterDB", masterDB), zap.Error(err))
		return
	}
	c.urac.SetMongoClient(client)
	c.urac.SetDatabase(db.DatabaseName())
}

// initTrafficModel selects the throttling store: the custom registry gateway traffic.model, kept
// in the environment's gateway database, else in provision.
func (c *Controller) initTrafficModel(regData *types.RegistryData) {
	gateway, _ := regData.CustomRegistry["gateway"].(map[string]interface{})
	value, _ := gateway["value"].(map[string]interface{})
	traffic, _ := value["traffic"].(map[string]interface{})
	if model, _ := traffic["model"].(string); model != "mongo" {
		return
	}
	client, err := c.coreClient(regData, "gateway")
	if err != nil {
		c.logger.Error("gateway database: throttling stays in memory", zap.Error(err))
		return
	}
	middleware.InitTrafficModel(middleware.TrafficConfig{
		Model:       "mongo",
		MongoClient: client,
		Database:    "",
		Logger:      c.logger,
	})
}
