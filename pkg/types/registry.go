package types

import (
	"github.com/soajs/soajs.controller.go/internal/dbconfig"
)

// RegistryData is the registry of one environment, built as the Node.js gateway builds it
// (modules/registry/index.js). Its JSON is what /getRegistry gives services, after filtering.
type RegistryData struct {
	TimeLoaded  int64  `json:"timeLoaded"`
	Name        string `json:"name"`
	Environment string `json:"environment"`
	Domain      string `json:"domain,omitempty"`
	SitePrefix  string `json:"sitePrefix,omitempty"`
	APIPrefix   string `json:"apiPrefix,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Port        int    `json:"port,omitempty"`
	// CoreDB holds provision (from the profile) and every tenantSpecific:false database of the
	// environment, by name.
	CoreDB map[string]*dbconfig.DB `json:"coreDB"`
	// TenantMetaDB holds the tenantSpecific:true databases, named "#TENANT_NAME#_<name>". The
	// gateway does not use them; services do.
	TenantMetaDB  map[string]*dbconfig.DB `json:"tenantMetaDB"`
	ServiceConfig *GlobalServiceConfig    `json:"serviceConfig"`
	// Deployer is the environment's deployer, with the infra account's configuration under
	// container.kubernetes.configuration.
	Deployer map[string]interface{} `json:"deployer"`
	// CustomRegistry holds the custom_registry entries for the environment, by name, each as stored.
	CustomRegistry map[string]interface{} `json:"custom"`
	// Resources holds the resources for the environment by type, then name, each as stored.
	Resources map[string]map[string]map[string]interface{} `json:"resources"`
	// SessionDB is coreDB.session as the Node.js registry gives it (build.sessionDB).
	SessionDB map[string]interface{} `json:"-"`
	// Reloaded is set once the registry was loaded again (reloadRegistry or the automatic
	// reload). Until then the Node.js gateway's database connections have consumed the
	// URLParam options it does not pass on and the extraParam of the configurations it gives
	// services; after a reload they show again.
	Reloaded bool                      `json:"-"`
	Services map[string]*ServiceConfig `json:"services"`
	Daemons  map[string]*DaemonConfig  `json:"daemons"`
}

// GlobalServiceConfig is environment.services.config (docs/data-schema/environment.md).
type GlobalServiceConfig struct {
	Awareness  AwarenessConfig        `json:"awareness"`
	Key        KeyEncryptionConfig    `json:"key"`
	Logger     LoggerConfig           `json:"logger"`
	CORS       CORSConfig             `json:"cors"`
	OAuth      OAuthConfig            `json:"oauth"`
	Ports      PortsConfig            `json:"ports"`
	Cookie     CookieConfig           `json:"cookie"`
	Session    SessionConfig          `json:"session"`
	Throttling map[string]interface{} `json:"throttling,omitempty"`
}

// AwarenessConfig holds the awareness settings. autoRelaodRegistry is spelled as stored.
type AwarenessConfig struct {
	AutoRegisterService bool `json:"autoRegisterService"`
	AutoRelaodRegistry  int  `json:"autoRelaodRegistry"`
	CacheTTL            int  `json:"cacheTTL"`
	HealthCheckInterval int  `json:"healthCheckInterval"`
	MaxLogCount         int  `json:"maxLogCount"`
}

// KeyEncryptionConfig holds the extKey cipher.
type KeyEncryptionConfig struct {
	Algorithm string `json:"algorithm"`
	Password  string `json:"password"`
}

// LoggerConfig holds the logger settings.
type LoggerConfig struct {
	Level     string           `json:"level"`
	Src       bool             `json:"src"`
	Gke       *bool            `json:"gke,omitempty"`
	Force     *bool            `json:"force,omitempty"`
	Formatter *FormatterConfig `json:"formatter,omitempty"`
}

// FormatterConfig holds logger formatter settings.
type FormatterConfig struct {
	LevelInString bool   `json:"levelInString"`
	OutputMode    string `json:"outputMode"`
}

// CORSConfig holds the CORS settings.
type CORSConfig struct {
	Enabled     bool   `json:"enabled"`
	Origin      string `json:"origin"`
	Credentials bool   `json:"credentials"`
	Methods     string `json:"methods"`
	Headers     string `json:"headers"`
	MaxAge      int    `json:"maxage"`
}

// OAuthConfig holds the oauth settings.
type OAuthConfig struct {
	Grants               []string `json:"grants"`
	Debug                bool     `json:"debug"`
	GetUserFromToken     bool     `json:"getUserFromToken"`
	AccessTokenLifetime  int      `json:"accessTokenLifetime"`
	RefreshTokenLifetime int      `json:"refreshTokenLifetime"`
}

// PortsConfig holds the ports.
type PortsConfig struct {
	Controller     int `json:"controller"`
	MaintenanceInc int `json:"maintenanceInc"`
	RandomInc      int `json:"randomInc"`
}

// CookieConfig holds the cookie secret.
type CookieConfig struct {
	Secret string `json:"secret"`
}

// SessionConfig holds the session settings.
type SessionConfig struct {
	Name              string             `json:"name"`
	Secret            string             `json:"secret"`
	Cookie            *SessionCookieConf `json:"cookie,omitempty"`
	Resave            bool               `json:"resave"`
	SaveUninitialized bool               `json:"saveUninitialized"`
	Rolling           bool               `json:"rolling"`
	Unset             string             `json:"unset,omitempty"`
}

// SessionCookieConf holds the session cookie settings.
type SessionCookieConf struct {
	Path     string `json:"path"`
	HTTPOnly bool   `json:"httpOnly"`
	Secure   bool   `json:"secure"`
	MaxAge   *int   `json:"maxAge"`
}

// HostRecord is a document of the hosts collection (docs/data-schema/hosts.md).
type HostRecord struct {
	Env           string `json:"env"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	IP            string `json:"ip"`
	Port          int    `json:"port"`
	Hostname      string `json:"hostname"`
	Type          string `json:"type,omitempty"`
	ServiceHATask string `json:"serviceHATask,omitempty"`
}
