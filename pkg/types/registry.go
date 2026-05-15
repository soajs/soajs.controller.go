package types

import "time"

// RegistryData holds the complete registry state
type RegistryData struct {
	TimeLoaded     int64                           `json:"timeLoaded"`
	Name           string                          `json:"name"`
	Environment    string                          `json:"environment"`
	Domain         string                          `json:"domain,omitempty"`
	SitePrefix     string                          `json:"sitePrefix,omitempty"`
	APIPrefix      string                          `json:"apiPrefix,omitempty"`
	Protocol       string                          `json:"protocol,omitempty"`
	Port           int                             `json:"port,omitempty"`
	ProfileOnly    bool                            `json:"profileOnly,omitempty"`
	Services       map[string]*ServiceConfig       `json:"services"`
	Daemons        map[string]*DaemonConfig        `json:"daemons"`
	CoreDB         *CoreDBConfig                   `json:"coreDB"`
	TenantMetaDB   map[string]*DBConnectionConfig  `json:"tenantMetaDB,omitempty"`
	ServiceConfig  *GlobalServiceConfig            `json:"serviceConfig"`
	CustomRegistry map[string]interface{}          `json:"custom,omitempty"`
	Resources      map[string]map[string]*Resource `json:"resources"`
	Hosts          map[string][]*HostRecord        `json:"hosts,omitempty"`
	Deployer       *DeployerConfig                 `json:"deployer"`
}

// NewRegistryData creates a new registry data instance
func NewRegistryData(env string) *RegistryData {
	return &RegistryData{
		TimeLoaded:     time.Now().UnixMilli(),
		Name:           env,
		Environment:    env,
		Services:       make(map[string]*ServiceConfig),
		Daemons:        make(map[string]*DaemonConfig),
		CustomRegistry: make(map[string]interface{}),
		Resources:      make(map[string]map[string]*Resource),
		Hosts:          make(map[string][]*HostRecord),
	}
}

// Resource represents a resource configuration
type Resource struct {
	ID         string                 `json:"_id" bson:"_id"`
	Name       string                 `json:"name" bson:"name"`
	Type       string                 `json:"type" bson:"type"`
	Created    string                 `json:"created" bson:"created"`
	Plugged    bool                   `json:"plugged" bson:"plugged"`
	Shared     bool                   `json:"shared" bson:"shared"`
	SharedEnvs map[string]bool        `json:"sharedEnvs" bson:"sharedEnvs"`
	Config     map[string]interface{} `json:"config" bson:"config"`
}

// CustomEntry represents a custom registry entry
type CustomEntry struct {
	ID         string                 `json:"_id" bson:"_id"`
	Name       string                 `json:"name" bson:"name"`
	Created    string                 `json:"created" bson:"created"`
	Plugged    bool                   `json:"plugged" bson:"plugged"`
	Shared     bool                   `json:"shared" bson:"shared"`
	SharedEnvs map[string]bool        `json:"sharedEnvs" bson:"sharedEnvs"`
	Value      interface{}            `json:"value" bson:"value"`
}

// HostRecord represents a host record in the database
type HostRecord struct {
	ID            string    `json:"_id" bson:"_id"`
	Env           string    `json:"env" bson:"env"`
	Name          string    `json:"name" bson:"name"`
	Version       string    `json:"version" bson:"version"`
	IP            string    `json:"ip" bson:"ip"`
	Hostname      string    `json:"hostname" bson:"hostname"`
	Port          int       `json:"port" bson:"port"`
	ServiceHATask string    `json:"serviceHATask,omitempty" bson:"serviceHATask,omitempty"`
	LastCheck     time.Time `json:"lastCheck" bson:"lastCheck"`
	Healthy       bool      `json:"healthy" bson:"healthy"`
}

// DeployerConfig holds deployer configuration
type DeployerConfig struct {
	Type      string                 `json:"type" bson:"type"`
	Selected  string                 `json:"selected" bson:"selected"`
	Container map[string]interface{} `json:"container" bson:"container"`
}

// MarketplaceItem represents a marketplace service/daemon
type MarketplaceItem struct {
	ID            string                  `json:"_id" bson:"_id"`
	Type          string                  `json:"type" bson:"type"`
	Name          string                  `json:"name" bson:"name"`
	Description   string                  `json:"description" bson:"description"`
	Configuration *ItemConfiguration      `json:"configuration" bson:"configuration"`
	Versions      []*ItemVersion          `json:"versions" bson:"versions"`
}

// ItemConfiguration holds marketplace item configuration
type ItemConfiguration struct {
	Port              int    `json:"port" bson:"port"`
	RequestTimeout    int    `json:"requestTimeout" bson:"requestTimeout"`
	RequestTimeoutRenewal int `json:"requestTimeoutRenewal" bson:"requestTimeoutRenewal"`
	Group             string `json:"group" bson:"group"`
	Subtype           string `json:"subtype" bson:"subtype"`
}

// ItemVersion holds version info for marketplace items
type ItemVersion struct {
	Version         string             `json:"version" bson:"version"`
	ExtKeyRequired  bool               `json:"extKeyRequired" bson:"extKeyRequired"`
	OAuth           bool               `json:"oauth" bson:"oauth"`
	Urac            bool               `json:"urac" bson:"urac"`
	UracProfile     bool               `json:"urac_Profile" bson:"urac_Profile"`
	UracACL         bool               `json:"urac_ACL" bson:"urac_ACL"`
	UracConfig      bool               `json:"urac_Config" bson:"urac_Config"`
	UracGroupConfig bool               `json:"urac_GroupConfig" bson:"urac_GroupConfig"`
	TenantProfile   bool               `json:"tenant_Profile" bson:"tenant_Profile"`
	ProvisionACL    bool               `json:"provision_ACL" bson:"provision_ACL"`
	APIs            []APIInfo          `json:"apis" bson:"apis"`
	InterConnect    []InterConnectItem `json:"interConnect" bson:"interConnect"`
}

// CoreDBConfig holds core database configuration
type CoreDBConfig struct {
	Provision DBConnectionConfig `json:"provision"`
	OAuth     DBConnectionConfig `json:"oauth"`
	Gateway   DBConnectionConfig `json:"gateway"`
}

// DBConnectionConfig holds database connection settings
type DBConnectionConfig struct {
	Cluster    string            `json:"cluster"`
	URLParam   map[string]string `json:"URLParam"`
	Servers    []ServerConfig    `json:"servers"`
	Credentials CredentialsConfig `json:"credentials"`
	Streaming  interface{}       `json:"streaming"`
	Name       string            `json:"name"`
	Prefix     string            `json:"prefix"`
}

// ServerConfig holds server connection info
type ServerConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// CredentialsConfig holds database credentials
type CredentialsConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// GlobalServiceConfig holds global service settings
type GlobalServiceConfig struct {
	MaintenanceMode bool                   `json:"maintenanceMode,omitempty"`
	Logger          LoggerConfig           `json:"logger"`
	Ports           PortsConfig            `json:"ports"`
	Awareness       AwarenessConfig        `json:"awareness"`
	OAuth           OAuthConfig            `json:"oauth,omitempty"`
	Key             KeyEncryptionConfig    `json:"key"`
	Cookie          CookieConfig           `json:"cookie,omitempty"`
	Session         SessionConfig          `json:"session,omitempty"`
	CORS            CORSConfig             `json:"cors,omitempty"`
	Throttling      map[string]interface{} `json:"throttling,omitempty"`
}

// CookieConfig holds cookie settings
type CookieConfig struct {
	Secret string `json:"secret"`
}

// SessionConfig holds session settings
type SessionConfig struct {
	Name              string            `json:"name"`
	Secret            string            `json:"secret"`
	Cookie            SessionCookieConf `json:"cookie"`
	Resave            bool              `json:"resave"`
	SaveUninitialized bool              `json:"saveUninitialized"`
	Rolling           bool              `json:"rolling"`
	Unset             string            `json:"unset"`
}

// SessionCookieConf holds session cookie settings
type SessionCookieConf struct {
	Path     string      `json:"path"`
	HTTPOnly bool        `json:"httpOnly"`
	Secure   bool        `json:"secure"`
	MaxAge   interface{} `json:"maxAge"`
}

// CORSConfig holds CORS settings
type CORSConfig struct {
	Credentials interface{} `json:"credentials" bson:"credentials"` // Can be bool or string
	Enabled     bool        `json:"enabled"`
	Headers     string      `json:"headers"`
	MaxAge      int         `json:"maxage"`
	Methods     string      `json:"methods"`
	Origin      string      `json:"origin"`
}

// ServiceKeyConfig holds service key configuration
type ServiceKeyConfig struct {
	Key KeyEncryptionConfig `json:"key" bson:"key"`
}

// KeyEncryptionConfig holds key encryption settings
type KeyEncryptionConfig struct {
	Algorithm string `json:"algorithm" bson:"algorithm"`
	Password  string `json:"password" bson:"password"`
}

// LoggerConfig holds logging configuration
type LoggerConfig struct {
	Src       bool            `json:"src"`
	Level     string          `json:"level"`
	Formatter FormatterConfig `json:"formatter"`
}

// FormatterConfig holds logger formatter settings
type FormatterConfig struct {
	LevelInString bool   `json:"levelInString"`
	OutputMode    string `json:"outputMode"`
}

// PortsConfig holds port configuration
type PortsConfig struct {
	Controller      int `json:"controller"`
	MaintenanceInc  int `json:"maintenanceInc"`
	RandomInc       int `json:"randomInc"`
}

// AwarenessConfig holds awareness configuration
type AwarenessConfig struct {
	HealthCheckInterval int  `json:"healthCheckInterval"`
	AutoReloadRegistry  bool `json:"autoReloadRegistry"`
	MaxLogCount         int  `json:"maxLogCount"`
	AutoRegisterService bool `json:"autoRegisterService"`
}

// OAuthConfig holds OAuth configuration
type OAuthConfig struct {
	Grants         []string `json:"grants"`
	Debug          bool     `json:"debug"`
	AccessTokenLifetime int `json:"accessTokenLifetime"`
	RefreshTokenLifetime int `json:"refreshTokenLifetime"`
}

// EnvironmentDoc represents an environment document
type EnvironmentDoc struct {
	Code          string                 `json:"code" bson:"code"`
	Description   string                 `json:"description" bson:"description"`
	Domain        string                 `json:"domain" bson:"domain"`
	SitePrefix    string                 `json:"sitePrefix" bson:"sitePrefix"`
	APIPrefix     string                 `json:"apiPrefix" bson:"apiPrefix"`
	Protocol      string                 `json:"protocol" bson:"protocol"`
	Port          int                    `json:"port" bson:"port"`
	ServiceConfig EnvironmentServices    `json:"services" bson:"services"`
	DBs           EnvironmentDBs         `json:"dbs" bson:"dbs"`
	Deployer      *DeployerConfig        `json:"deployer" bson:"deployer"`
}

// EnvironmentServices holds services configuration from environment doc
type EnvironmentServices struct {
	Controller ControllerConfig     `json:"controller" bson:"controller"`
	Config     GlobalServiceConfig  `json:"config" bson:"config"`
}

// ControllerConfig holds controller-specific configuration
type ControllerConfig struct {
	Authorization         bool `json:"authorization" bson:"authorization"`
	RequestTimeout        int  `json:"requestTimeout" bson:"requestTimeout"`
	RequestTimeoutRenewal int  `json:"requestTimeoutRenewal" bson:"requestTimeoutRenewal"`
}

// EnvironmentDBs holds database configuration
type EnvironmentDBs struct {
	Config    DBConfigPrefix           `json:"config" bson:"config"`
	Databases map[string]DBReference   `json:"databases" bson:"databases"`
}

// DBConfigPrefix holds DB prefix configuration
type DBConfigPrefix struct {
	Prefix string `json:"prefix" bson:"prefix"`
}

// DBReference holds database reference with cluster name
type DBReference struct {
	Cluster        string `json:"cluster" bson:"cluster"`
	TenantSpecific bool   `json:"tenantSpecific" bson:"tenantSpecific"`
}

// ControllerUpdate holds controller update data
type ControllerUpdate struct {
	IP   string      `json:"ip"`
	TS   int64       `json:"ts"`
	Data interface{} `json:"data"`
	Env  string      `json:"env"`
}
