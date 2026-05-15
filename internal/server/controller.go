package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/middleware"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/mongo"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"github.com/soajs/soajs.controller.go/pkg/urac"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// Controller manages the gateway components
type Controller struct {
	cfg        *config.Config
	logger     *zap.Logger
	registry   registry.Registry
	provision  provision.Provision
	awareness  awareness.Awareness
	urac       *urac.Driver
	mongo      *mongo.Client
	oauthMongo *mongo.Client // Separate MongoDB client for OAuth/URAC cluster
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

	// Initialize URAC driver
	ctrl.urac = urac.NewDriverWithConfig(mongoClient, logger, urac.Config{
		HashIterations: 12,
		Environment:    cfg.Environment,
	})

	// Provision will be initialized in Setup() after registry loads
	// so we can get OAuth database from registry resources

	return ctrl, nil
}

// Setup configures the router with middleware and routes
func (c *Controller) Setup(r *chi.Mux) error {
	// Load registry first - this loads resources including OAuth cluster config
	if err := c.registry.Load(c.cfg.Environment); err != nil {
		return err
	}

	regData := c.registry.Get()

	// Check if OAuth is configured on a different cluster
	if regData != nil && regData.CoreDB != nil && regData.CoreDB.OAuth.Name != "" {
		oauthConfig := regData.CoreDB.OAuth

		// Check if OAuth is on a different server than what we're currently connected to
		// The cluster name might be the same, but actual servers could differ
		oauthHost := ""
		if len(oauthConfig.Servers) > 0 {
			oauthHost = oauthConfig.Servers[0].Host
		}

		// The main mongo client is connected via SOAJS_MONGO_URI, check if OAuth servers are different
		currentHost := c.cfg.Mongo.URI
		needsSeparateClient := oauthHost != "" && !strings.Contains(currentHost, oauthHost)

		if needsSeparateClient && len(oauthConfig.Servers) > 0 {
			// OAuth is on a different cluster - create separate client
			c.logger.Info("OAuth configured on separate cluster",
				zap.String("oauthHost", oauthHost),
				zap.String("currentURI", currentHost),
			)

			// Convert types for mongo.NewClientFromDBConfig
			servers := make([]mongo.ServerConfig, len(oauthConfig.Servers))
			for i, s := range oauthConfig.Servers {
				servers[i] = mongo.ServerConfig{Host: s.Host, Port: s.Port}
			}

			var creds *mongo.CredentialsConfig
			if oauthConfig.Credentials.Username != "" {
				creds = &mongo.CredentialsConfig{
					Username: oauthConfig.Credentials.Username,
					Password: oauthConfig.Credentials.Password,
				}
			}

			oauthClient, err := mongo.NewClientFromDBConfig(servers, creds, oauthConfig.Name, oauthConfig.URLParam)
			if err != nil {
				c.logger.Warn("failed to connect to OAuth cluster, falling back to provision cluster",
					zap.Error(err),
				)
				// Fall back to same cluster with different database
				c.provision = provision.NewProvisionWithOAuthDB(c.mongo, c.logger, c.cfg.Environment, oauthConfig.Name)
			} else {
				c.logger.Info("connected to separate OAuth cluster",
					zap.String("database", oauthConfig.Name),
				)
				c.oauthMongo = oauthClient // Store for URAC operations
				c.provision = provision.NewProvisionWithOAuthClient(c.mongo, oauthClient, c.logger, c.cfg.Environment)
			}
		} else {
			// Same cluster, possibly different database
			c.logger.Debug("using OAuth database from registry",
				zap.String("database", oauthConfig.Name),
				zap.String("cluster", oauthConfig.Cluster),
			)
			c.provision = provision.NewProvisionWithOAuthDB(c.mongo, c.logger, c.cfg.Environment, oauthConfig.Name)
		}
	} else {
		// No separate OAuth config - use default
		c.provision = provision.NewProvision(c.mongo, c.logger, c.cfg.Environment)
	}

	// Set key encryption config from registry
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

	// Configure URAC database from registry
	// Priority 1: Check for masterDB in custom registry (soajs.registry.custom.urac.value.masterDB)
	// Priority 2: Use tenantMetaDB["urac"] configuration
	uracDBConfigured := false
	if regData != nil && regData.CustomRegistry != nil {
		if uracEntry, ok := regData.CustomRegistry["urac"]; ok {
			c.logger.Debug("Found urac entry in custom registry",
				zap.String("type", fmt.Sprintf("%T", uracEntry)),
			)
			// Handle both map[string]interface{} and bson.M
			var uracMap map[string]interface{}
			if m, ok := uracEntry.(map[string]interface{}); ok {
				uracMap = m
			} else if m, ok := uracEntry.(bson.M); ok {
				uracMap = m
			}
			if uracMap != nil {
				var value map[string]interface{}
				if v, ok := uracMap["value"].(map[string]interface{}); ok {
					value = v
				} else if v, ok := uracMap["value"].(bson.M); ok {
					value = v
				}
				if value != nil {
					if masterDB, ok := value["masterDB"].(string); ok && masterDB != "" {
						// masterDB refers to a coreDB entry name (e.g., "provision")
						// Use the database name from that coreDB entry
						switch masterDB {
						case "provision":
							if regData.CoreDB != nil && regData.CoreDB.Provision.Name != "" {
								c.urac.SetDatabase(regData.CoreDB.Provision.Name)
								c.logger.Info("URAC database configured from masterDB",
									zap.String("masterDB", masterDB),
									zap.String("database", regData.CoreDB.Provision.Name),
								)
								uracDBConfigured = true
							}
						case "oauth":
							if regData.CoreDB != nil && regData.CoreDB.OAuth.Name != "" {
								c.urac.SetDatabase(regData.CoreDB.OAuth.Name)
								c.logger.Info("URAC database configured from masterDB",
									zap.String("masterDB", masterDB),
									zap.String("database", regData.CoreDB.OAuth.Name),
								)
								uracDBConfigured = true
							}
						default:
							// masterDB refers to a database definition - check TenantMetaDB first
							// then fallback to constructing the name as {prefix}{masterDB}
							if regData.TenantMetaDB != nil {
								if dbConfig, ok := regData.TenantMetaDB[masterDB]; ok && dbConfig.Name != "" {
									// Use the database name from TenantMetaDB (might have prefix)
									dbName := dbConfig.Name
									if strings.Contains(dbName, "#TENANT_NAME#") {
										dbPrefix := dbConfig.Prefix
										if dbPrefix == "" {
											dbPrefix = strings.ToLower(c.cfg.Environment)
										}
										dbName = dbPrefix + masterDB
									}
									c.urac.SetDatabase(dbName)
									c.logger.Info("URAC database configured from TenantMetaDB",
										zap.String("masterDB", masterDB),
										zap.String("database", dbName),
									)
									uracDBConfigured = true
									break
								}
							}
							// Fallback: construct full name as {prefix}{masterDB}
							dbPrefix := strings.ToLower(c.cfg.Environment)
							fullDBName := dbPrefix + masterDB
							c.urac.SetDatabase(fullDBName)
							c.logger.Info("URAC database configured from masterDB (fallback)",
								zap.String("masterDB", masterDB),
								zap.String("database", fullDBName),
							)
							uracDBConfigured = true
						}
					}
				}
			}
		}
	}

	// Priority 2: Fall back to tenantMetaDB["urac"] if masterDB not configured
	if !uracDBConfigured && regData != nil && regData.TenantMetaDB != nil {
		if uracDB, ok := regData.TenantMetaDB["urac"]; ok && uracDB.Name != "" {
			dbName := uracDB.Name
			// Replace #TENANT_NAME# with prefix + "core" for controller-level lookups
			if strings.Contains(dbName, "#TENANT_NAME#") {
				prefix := uracDB.Prefix
				if prefix == "" {
					prefix = "core"
				}
				dbName = prefix + "urac"
			}
			c.urac.SetDatabase(dbName)
			c.logger.Info("URAC database configured from tenantMetaDB",
				zap.String("database", dbName),
			)
		}
	}

	// If OAuth is on a separate cluster and URAC database is configured,
	// use the OAuth MongoDB client for URAC operations (users/groups are on OAuth cluster)
	if c.oauthMongo != nil && c.urac.GetDatabase() != "" {
		c.urac.SetMongoClient(c.oauthMongo)
		c.logger.Info("URAC configured to use OAuth MongoDB cluster",
			zap.String("database", c.urac.GetDatabase()),
		)
	}

	// Initialize traffic model based on registry config
	c.initTrafficModel(regData)

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

// initTrafficModel initializes the traffic throttling model based on registry config
func (c *Controller) initTrafficModel(regData *types.RegistryData) {
	if regData == nil || regData.CustomRegistry == nil {
		return
	}

	// Check custom registry for traffic model config
	// Path: gateway.value.traffic.model
	gateway, ok := regData.CustomRegistry["gateway"]
	if !ok {
		return
	}

	var gatewayValue map[string]interface{}
	switch v := gateway.(type) {
	case map[string]interface{}:
		if val, exists := v["value"]; exists {
			gatewayValue, _ = val.(map[string]interface{})
		} else {
			gatewayValue = v
		}
	case bson.M:
		if val, exists := v["value"]; exists {
			gatewayValue, _ = val.(map[string]interface{})
			if gatewayValue == nil {
				if bsonVal, ok := val.(bson.M); ok {
					gatewayValue = bsonVal
				}
			}
		} else {
			gatewayValue = v
		}
	default:
		return
	}

	traffic, ok := gatewayValue["traffic"].(map[string]interface{})
	if !ok {
		if trafficBson, ok := gatewayValue["traffic"].(bson.M); ok {
			traffic = trafficBson
		} else {
			return
		}
	}

	model, ok := traffic["model"].(string)
	if !ok || model != "mongo" {
		c.logger.Info("Traffic throttling using memory model")
		return
	}

	// Initialize MongoDB throttling model
	// Use the gateway database from CoreDB config
	var gatewayConfig *types.DBConnectionConfig
	if regData.CoreDB != nil && regData.CoreDB.Gateway.Name != "" {
		gatewayConfig = &regData.CoreDB.Gateway
	}

	// Determine database name: prefix + name (e.g., "avdev" + "gateway" = "avdevgateway")
	// But if Name already starts with Prefix, don't duplicate it
	databaseName := ""
	if gatewayConfig != nil && gatewayConfig.Name != "" {
		if gatewayConfig.Prefix != "" && !strings.HasPrefix(gatewayConfig.Name, gatewayConfig.Prefix) {
			databaseName = gatewayConfig.Prefix + gatewayConfig.Name
		} else {
			databaseName = gatewayConfig.Name
		}
	} else {
		// Fallback: construct as {env}gateway (e.g., "avdevgateway")
		databaseName = strings.ToLower(c.cfg.Environment) + "gateway"
	}

	// Check if gateway is on a different cluster
	var mongoClient *mongo.Client
	if gatewayConfig != nil && len(gatewayConfig.Servers) > 0 {
		gatewayHost := gatewayConfig.Servers[0].Host
		currentHost := c.cfg.Mongo.URI
		needsSeparateClient := gatewayHost != "" && !strings.Contains(currentHost, gatewayHost)

		if needsSeparateClient {
			// Gateway is on a different cluster - create separate client
			c.logger.Info("Gateway configured on separate cluster",
				zap.String("gatewayHost", gatewayHost),
			)

			servers := make([]mongo.ServerConfig, len(gatewayConfig.Servers))
			for i, s := range gatewayConfig.Servers {
				servers[i] = mongo.ServerConfig{Host: s.Host, Port: s.Port}
			}

			var creds *mongo.CredentialsConfig
			if gatewayConfig.Credentials.Username != "" {
				creds = &mongo.CredentialsConfig{
					Username: gatewayConfig.Credentials.Username,
					Password: gatewayConfig.Credentials.Password,
				}
			}

			gatewayClient, err := mongo.NewClientFromDBConfig(servers, creds, databaseName, gatewayConfig.URLParam)
			if err != nil {
				c.logger.Warn("failed to connect to gateway cluster, using main cluster",
					zap.Error(err),
				)
				mongoClient = c.mongo
			} else {
				c.logger.Info("connected to separate gateway cluster",
					zap.String("database", databaseName),
				)
				mongoClient = gatewayClient
				// Set database to empty since NewClientFromDBConfig already sets it
				databaseName = ""
			}
		} else {
			mongoClient = c.mongo
		}
	} else {
		mongoClient = c.mongo
	}

	c.logger.Info("Traffic throttling using MongoDB model",
		zap.String("database", databaseName),
	)

	middleware.InitTrafficModel(middleware.TrafficConfig{
		Model:       "mongo",
		MongoClient: mongoClient,
		Database:    databaseName,
		Logger:      c.logger,
	})
}
