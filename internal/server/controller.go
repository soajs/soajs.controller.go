package server

import (
	"context"
	"fmt"
	"net/http"
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
	uracuser "github.com/soajs/soajs.controller.go/internal/urac"
	"github.com/soajs/soajs.controller.go/pkg/mongo"
	"github.com/soajs/soajs.controller.go/pkg/types"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// Controller manages the gateway components
type Controller struct {
	cfg          *config.Config
	logger       *zap.Logger
	registry     registry.Registry
	provision    provision.Provision
	awareness    awareness.Awareness
	awarenessEnv awareness.EnvAwareness
	// users reads the logged-in user from coreDB[masterDB] on key APIs
	users *uracuser.Driver
	mongo *mongo.Client // the provision database, from the profile

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

	ctrl.users = uracuser.NewDriver(func(db *dbconfig.DB) (*drv.Database, error) {
		client, err := ctrl.clientFor(db)
		if err != nil {
			return nil, err
		}
		return client.Database(), nil
	}, logger)

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

	// Awareness of the environment's services and of the gateway in the other environments
	c.awareness = awareness.New(c.registry, c.Gateway(), c.logger)
	c.awarenessEnv = awareness.NewEnv(c.registry, c.Gateway(), c.logger)
	if starter, ok := c.awareness.(interface{ Start(context.Context) }); ok {
		starter.Start(context.Background())
	}
	if starter, ok := c.awarenessEnv.(interface{ Start(context.Context) }); ok {
		starter.Start(context.Background())
	}

	// Initialize traffic model based on registry config
	trafficModels := c.trafficModels(regData)
	idempotencyModels := c.idempotencyModels(regData)

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
	r.Use(middleware.AwarenessEnv(c.awarenessEnv))
	r.Use(middleware.Key(c.provision, c.registry, c.cfg.Environment))
	r.Use(middleware.KeyACL())
	r.Use(middleware.GotoService(c.registry, c.awareness, middleware.NewMonitor(c.registry.Get, c.awareness, c.logger), c.provision,
		middleware.GatewayService{Name: c.cfg.ServiceName, Version: c.cfg.ServiceVersion}))
	r.Use(middleware.MT(c.provision, c.registry, c.cfg.Environment))
	r.Use(middleware.OAuth(c.provision, c.registry))
	r.Use(middleware.Urac(c.users, c.provision, c.registry, c.cfg.Environment))
	r.Use(middleware.MTACL(c.registry))
	r.Use(middleware.InjectObj(c.registry, c.awareness)) // the soajsinjectobj of key APIs, at the end of mt
	r.Use(middleware.Idempotency(c.registry, idempotencyModels, c.logger))
	r.Use(middleware.Traffic(c.registry, trafficModels, c.logger))
	r.Use(middleware.LastSeen(c.registry, c.awareness))

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

// Gateway names the gateway: service controller, version 1.
func (c *Controller) Gateway() awareness.Gateway {
	return awareness.Gateway{Name: "controller", Version: "1"}
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

// idempotencyModels are the Idempotency-Key stores (mw/idempotency/model): memory, and mongo in
// the environment's gateway database, else provision, both ready as in Node.js; the custom
// registry gateway idempotency.model picks one.
func (c *Controller) idempotencyModels(regData *types.RegistryData) map[string]middleware.IdempotencyModel {
	models := map[string]middleware.IdempotencyModel{"memory": middleware.NewMemoryIdempotency(context.Background())}
	client, err := c.coreClient(regData, "gateway")
	if err != nil {
		c.logger.Error("gateway database: idempotency with the mongo model is off", zap.Error(err))
		return models
	}
	models["mongo"] = middleware.NewMongoIdempotency(client.Database(), c.logger)
	return models
}

// trafficModels are the throttling stores (mw/traffic/model): memory, and mongo in the
// environment's gateway database, else provision, both ready as in Node.js; the custom registry
// gateway traffic.model picks one per request.
func (c *Controller) trafficModels(regData *types.RegistryData) map[string]middleware.ThrottlingModel {
	models := map[string]middleware.ThrottlingModel{"memory": middleware.NewMemoryThrottling(context.Background(), c.logger)}
	client, err := c.coreClient(regData, "gateway")
	if err != nil {
		c.logger.Error("gateway database: throttling with the mongo model is off", zap.Error(err))
		return models
	}
	models["mongo"] = middleware.NewMongoThrottling(client.Database(), c.logger)
	return models
}
