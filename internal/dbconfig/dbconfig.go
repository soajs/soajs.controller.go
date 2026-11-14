// Package dbconfig holds a database configuration as the registry gives it to the gateway and to
// services (coreDB.<name>), and the profile file the provision database comes from.
package dbconfig

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/mongo/driver/connstring"
)

// Server is one MongoDB host.
type Server struct {
	Host string `json:"host"`
	Port int    `json:"port,omitempty"`
}

// Credentials of a cluster with authentication.
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Location says where in the registry a database configuration comes from.
type Location struct {
	L1         string `json:"l1"`
	L2         string `json:"l2"`
	Env        string `json:"env"`
	Cluster    string `json:"cluster,omitempty"`
	TimeLoaded int64  `json:"timeLoaded"`
}

// DB is a database configuration: coreDB.<name> or tenantMetaDB.<name>.
type DB struct {
	Prefix           string                 `json:"prefix"`
	Cluster          string                 `json:"cluster,omitempty"`
	Protocol         string                 `json:"protocol,omitempty"`
	Servers          []Server               `json:"servers"`
	Credentials      *Credentials           `json:"credentials,omitempty"`
	Streaming        map[string]interface{} `json:"streaming,omitempty"`
	URLParam         map[string]interface{} `json:"URLParam"`
	RegistryLocation *Location              `json:"registryLocation,omitempty"`
	Name             string                 `json:"name"`
}

// DatabaseName is the name MongoDB knows the database by: the prefix and the name, joined.
func (d *DB) DatabaseName() string {
	return d.Prefix + d.Name
}

// ClientOptions builds the MongoDB client options. A URLParam option the Go driver does not know
// is a configuration error.
func (d *DB) ClientOptions() (*options.ClientOptions, error) {
	uri, err := d.URI()
	if err != nil {
		return nil, err
	}
	cs, err := connstring.ParseAndValidate(uri)
	if err != nil {
		return nil, fmt.Errorf("database %s: %w", d.Name, redact(err, d.Credentials))
	}
	if len(cs.UnknownOptions) > 0 {
		var names []string
		for k := range cs.UnknownOptions {
			names = append(names, k)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("database %s: URLParam options unknown to the driver: %s", d.Name, strings.Join(names, ", "))
	}
	return options.Client().ApplyURI(uri), nil
}

// URI builds the connection string. It holds the credentials, so it is never logged.
func (d *DB) URI() (string, error) {
	if len(d.Servers) == 0 {
		return "", fmt.Errorf("database %s: no servers", d.Name)
	}
	protocol := d.Protocol
	if protocol == "" {
		protocol = "mongodb://"
	}
	if protocol != "mongodb://" && protocol != "mongodb+srv://" {
		return "", fmt.Errorf("database %s: protocol must be mongodb:// or mongodb+srv://", d.Name)
	}
	var b strings.Builder
	b.WriteString(protocol)
	if d.Credentials != nil {
		if d.Credentials.Username == "" || d.Credentials.Password == "" {
			return "", fmt.Errorf("database %s: credentials need a username and a password", d.Name)
		}
		b.WriteString(url.QueryEscape(d.Credentials.Username))
		b.WriteString(":")
		b.WriteString(url.QueryEscape(d.Credentials.Password))
		b.WriteString("@")
	}
	for i, s := range d.Servers {
		if s.Host == "" {
			return "", fmt.Errorf("database %s: a server has no host", d.Name)
		}
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(s.Host)
		if s.Port != 0 {
			b.WriteString(":")
			b.WriteString(strconv.Itoa(s.Port))
		}
	}
	b.WriteString("/")
	query, err := urlParams(d.URLParam)
	if err != nil {
		return "", fmt.Errorf("database %s: %w", d.Name, err)
	}
	if query != "" {
		b.WriteString("?")
		b.WriteString(query)
	}
	return b.String(), nil
}

// urlParams turns URLParam into URI options. writeConcern {w, j, wtimeout} is the one object
// option; it becomes w, journal and wtimeoutMS.
func urlParams(params map[string]interface{}) (string, error) {
	values := url.Values{}
	for k, v := range params {
		if strings.EqualFold(k, "writeConcern") {
			wc, ok := v.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("URLParam writeConcern must be an object")
			}
			for wk, wv := range wc {
				name := map[string]string{"w": "w", "j": "journal", "wtimeout": "wtimeoutMS"}[wk]
				if name == "" {
					return "", fmt.Errorf("URLParam writeConcern.%s is unknown", wk)
				}
				s, err := scalar(wv)
				if err != nil {
					return "", fmt.Errorf("URLParam writeConcern.%s: %w", wk, err)
				}
				values.Set(name, s)
			}
			continue
		}
		s, err := scalar(v)
		if err != nil {
			return "", fmt.Errorf("URLParam %s: %w", k, err)
		}
		values.Set(k, s)
	}
	return values.Encode(), nil
}

func scalar(v interface{}) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case bool:
		return strconv.FormatBool(t), nil
	case float64:
		if t != float64(int64(t)) {
			return "", fmt.Errorf("must be an integer")
		}
		return strconv.FormatInt(int64(t), 10), nil
	case int:
		return strconv.Itoa(t), nil
	case int32:
		return strconv.Itoa(int(t)), nil
	case int64:
		return strconv.FormatInt(t, 10), nil
	}
	return "", fmt.Errorf("must be a string, a number or a boolean")
}

// redact keeps credentials out of a driver error.
func redact(err error, c *Credentials) error {
	if c == nil {
		return err
	}
	msg := err.Error()
	for _, s := range []string{c.Password, url.QueryEscape(c.Password)} {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "<password>")
		}
	}
	return fmt.Errorf("%s", msg)
}
