package types

// RegistryData holds the complete registry state
type RegistryData struct {
	Services       map[string]*ServiceConfig  `json:"services"`
	Daemons        map[string]*DaemonConfig   `json:"daemons"`
	CoreDB         *CoreDBConfig              `json:"coreDB"`
	ServiceConfig  *GlobalServiceConfig       `json:"serviceConfig"`
	CustomRegistry map[string]interface{}     `json:"customRegistry"`
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
	MaintenanceMode bool            `json:"maintenanceMode"`
	Logger          LoggerConfig    `json:"logger"`
	Ports           PortsConfig     `json:"ports"`
	Awareness       AwarenessConfig `json:"awareness"`
	OAuth           OAuthConfig     `json:"oauth"`
}

// LoggerConfig holds logging configuration
type LoggerConfig struct {
	Level     string `json:"level"`
	Formatter string `json:"formatter"`
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
	Code          string              `json:"code" bson:"code"`
	Description   string              `json:"description" bson:"description"`
	Domain        string              `json:"domain" bson:"domain"`
	ServiceConfig GlobalServiceConfig `json:"services" bson:"services"`
	CoreDB        CoreDBConfig        `json:"coreDB" bson:"coreDB"`
}

// ControllerUpdate holds controller update data
type ControllerUpdate struct {
	IP   string      `json:"ip"`
	TS   int64       `json:"ts"`
	Data interface{} `json:"data"`
	Env  string      `json:"env"`
}
