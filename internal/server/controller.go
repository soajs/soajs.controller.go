package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/middleware"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/mongo"
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
	mongo     *mongo.Client
}

// NewController creates a new controller instance
func NewController(cfg *config.Config, logger *zap.Logger) (*Controller, error) {
	ctrl := &Controller{
		cfg:    cfg,
		logger: logger,
	}

	// Initialize MongoDB client
	mongoClient, err := mongo.NewClient(cfg.Mongo)
	if err != nil {
		return nil, err
	}
	ctrl.mongo = mongoClient

	// Initialize registry
	ctrl.registry = registry.NewMongoRegistry(mongoClient, logger)

	// Initialize provision (with optional separate oauth database)
	ctrl.provision = provision.NewProvisionWithOAuthDB(mongoClient, logger, cfg.Environment, cfg.Mongo.OAuthDatabase)

	// Initialize awareness
	ctrl.awareness = awareness.NewAwareness(cfg.Awareness, ctrl.registry, logger)

	// Initialize URAC driver
	ctrl.urac = urac.NewDriverWithConfig(mongoClient, logger, urac.Config{
		HashIterations: 12,
		Environment:    cfg.Environment,
	})

	return ctrl, nil
}

// Setup configures the router with middleware and routes
func (c *Controller) Setup(r *chi.Mux) error {
	// Load registry
	if err := c.registry.Load(c.cfg.Environment); err != nil {
		return err
	}

	// Set key encryption config from registry
	regData := c.registry.Get()
	if regData != nil && regData.ServiceConfig != nil {
		keyConfig := regData.ServiceConfig.Key
		c.provision.SetKeyConfig(keyConfig.Algorithm, keyConfig.Password)
		c.logger.Debug("key config loaded",
			zap.String("algorithm", keyConfig.Algorithm),
		)
	}

	// Load provision data
	if err := c.provision.LoadProvision(); err != nil {
		return err
	}

	c.logger.Info("service provision loaded")

	// Setup base middleware (chi built-in)
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)

	// Setup SOAJS middleware chain
	r.Use(middleware.Context(c.logger, c.cfg.Environment))
	r.Use(middleware.CORS())
	r.Use(middleware.Favicon())
	r.Use(middleware.Response())
	r.Use(middleware.Enhancer())
	r.Use(middleware.IP2BanWithRegistry(c.registry))
	r.Use(middleware.MaintenanceMode(c.registry))
	r.Use(middleware.URL())
	r.Use(middleware.Awareness(c.awareness))
	r.Use(middleware.AwarenessEnv(c.awareness, c.registry, c.cfg.Environment))
	r.Use(middleware.Key(c.provision))
	r.Use(middleware.KeyACL())
	r.Use(middleware.GotoService(c.provision, c.registry, c.awareness))
	r.Use(middleware.MT(c.provision, c.registry, c.urac, c.cfg.Environment))
	r.Use(middleware.SoajsRoutesSetup(c.urac)) // Setup /soajs/* routes to require OAuth
	r.Use(middleware.OAuth(c.provision))
	r.Use(middleware.UserACL())
	r.Use(middleware.SoajsRoutes(c.provision)) // Handle /soajs/* routes after OAuth/UserACL populate context
	r.Use(middleware.Traffic(c.registry))
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
