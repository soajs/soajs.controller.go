package awareness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/lib"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// kubeTimeout bounds one call to the Kubernetes API.
const kubeTimeout = 10 * time.Second

// KubeConfig is the cluster account of an environment's deployer: its API address, token, TLS
// settings and the environment's namespace.
type KubeConfig struct {
	URL       string
	Token     string
	Namespace string
	// Insecure skips the server certificate check.
	Insecure bool
	CAData   []byte
	CertData []byte
	KeyData  []byte
}

// KubeClient is what awareness asks Kubernetes.
type KubeClient interface {
	ListServices(ctx context.Context, namespace, labelSelector string) ([]corev1.Service, error)
	GetService(ctx context.Context, namespace, name string) (*corev1.Service, error)
}

// KubeConnect opens a client on a cluster.
type KubeConnect func(cfg KubeConfig) (KubeClient, error)

// connectKube connects with the account's token and TLS settings.
func connectKube(cfg KubeConfig) (KubeClient, error) {
	cs, err := kubernetes.NewForConfig(restConfig(cfg))
	if err != nil {
		return nil, err
	}
	return clientset{cs}, nil
}

func restConfig(cfg KubeConfig) *rest.Config {
	return &rest.Config{
		Host:        cfg.URL,
		BearerToken: cfg.Token,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: cfg.Insecure,
			CAData:   cfg.CAData,
			CertData: cfg.CertData,
			KeyData:  cfg.KeyData,
		},
		Timeout: kubeTimeout,
	}
}

type clientset struct{ kubernetes.Interface }

func (c clientset) ListServices(ctx context.Context, namespace, selector string) ([]corev1.Service, error) {
	list, err := c.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (c clientset) GetService(ctx context.Context, namespace, name string) (*corev1.Service, error) {
	return c.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
}

// NewKubeClient wraps a client-go clientset, real or fake.
func NewKubeClient(cs kubernetes.Interface) KubeClient { return clientset{cs} }

// kubeConfig is the deployer's cluster account (ha.js getInfraToken, driver kubernetes connect):
// deployer type container, selected container.kubernetes, with the infra account's configuration
// the registry loaded. Without configuration.ca, the API is at protocol://url:port and, as Node.js
// does, the server certificate is not verified. With it, ca.requestOptions are the client's
// request options: the API at baseUrl, verified against its ca unless strictSSL is false, with
// its client certificate, and its auth.bearer token, else the account's token.
func kubeConfig(reg *types.RegistryData) (KubeConfig, error) {
	if reg == nil || reg.Deployer["type"] != "container" || reg.Deployer["selected"] != "container.kubernetes" {
		return KubeConfig{}, errors.New("Unable to find healthy configuration in registry")
	}
	container, _ := reg.Deployer["container"].(map[string]interface{})
	k8s, _ := container["kubernetes"].(map[string]interface{})
	conf, _ := k8s["configuration"].(map[string]interface{})
	namespace, _ := k8s["namespace"].(string)
	url, _ := conf["url"].(string)
	token, _ := conf["token"].(string)
	if url == "" || token == "" || namespace == "" {
		return KubeConfig{}, errors.New("Configuration requires namespace, token, and url")
	}
	if ca, ok := conf["ca"].(map[string]interface{}); ok {
		opts, _ := ca["requestOptions"].(map[string]interface{})
		cfg := KubeConfig{Token: token, Namespace: namespace}
		cfg.URL, _ = opts["baseUrl"].(string)
		if auth, ok := opts["auth"].(map[string]interface{}); ok {
			cfg.Token, _ = auth["bearer"].(string)
		}
		if strict, ok := opts["strictSSL"].(bool); ok && !strict {
			cfg.Insecure = true
		} else {
			switch c := opts["ca"].(type) {
			case string:
				cfg.CAData = []byte(c)
			case []interface{}:
				var pems []string
				for _, p := range c {
					if s, ok := p.(string); ok {
						pems = append(pems, s)
					}
				}
				cfg.CAData = []byte(strings.Join(pems, "\n"))
			}
		}
		if cert, ok := opts["cert"].(string); ok {
			cfg.CertData = []byte(cert)
		}
		if key, ok := opts["key"].(string); ok {
			cfg.KeyData = []byte(key)
		}
		if cfg.URL == "" || cfg.Token == "" {
			return KubeConfig{}, errors.New("Configuration requires namespace, token, and url")
		}
		return cfg, nil
	}
	protocol, _ := conf["protocol"].(string)
	if protocol == "" {
		protocol = "https"
	}
	port := ""
	if p, ok := conf["port"].(float64); ok && p != 0 {
		port = fmt.Sprintf(":%d", int(p))
	}
	return KubeConfig{URL: protocol + "://" + url + port, Token: token, Namespace: namespace, Insecure: true}, nil
}

// namespaceOf is the environment's namespace: deployer.container.kubernetes.namespace.
func namespaceOf(reg *types.RegistryData) string {
	if reg == nil {
		return ""
	}
	container, _ := reg.Deployer["container"].(map[string]interface{})
	k8s, _ := container["kubernetes"].(map[string]interface{})
	ns, _ := k8s["namespace"].(string)
	return ns
}

// gatewayHost is the Kubernetes service name of the gateway in an environment:
// <env>-<gateway>[-v<version>-service].<namespace>, the suffix with SOAJS_DEPLOY_HA=kubernetes.
// Node.js reads the namespace from the old deployer shape and never finds it; here it is the
// environment's namespace, and without one there is no host.
func gatewayHost(env string, gw Gateway, reg *types.RegistryData) string {
	ns := namespaceOf(reg)
	if ns == "" {
		return ""
	}
	name := gw.Name
	if os.Getenv("SOAJS_DEPLOY_HA") == "kubernetes" {
		name += "-v" + gw.Version + "-service"
	}
	return env + "-" + name + "." + ns
}

// HA is the awareness with SOAJS_DEPLOY_HA (mw/awareness/ha.js): a cache of the cluster IPs of the
// environment's services, rebuilt every cacheTTL, then the Kubernetes API.
type HA struct {
	reg     registry.Registry
	gw      Gateway
	logger  *zap.Logger
	connect KubeConnect
	env     string

	mu    sync.Mutex
	cache map[string]map[string]string
}

// NewHA returns the Kubernetes awareness; connect is the cluster client (nil for client-go).
// Start begins the cache rebuilds.
func NewHA(reg registry.Registry, gw Gateway, logger *zap.Logger, connect KubeConnect) *HA {
	if connect == nil {
		connect = connectKube
	}
	return &HA{reg: reg, gw: gw, logger: logger, connect: connect, env: reg.GetEnvironment(), cache: map[string]map[string]string{}}
}

// Start rebuilds the cache now and then every services.config.awareness.cacheTTL ms. Node.js
// stops rebuilding after a failed rebuild; here the next one is still scheduled.
func (h *HA) Start(ctx context.Context) {
	go func() {
		for {
			h.Rebuild()
			reg := h.reg.Get()
			if reg == nil || reg.ServiceConfig == nil || reg.ServiceConfig.Awareness.CacheTTL <= 0 {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(reg.ServiceConfig.Awareness.CacheTTL) * time.Millisecond):
			}
		}
	}()
}

func (h *HA) client() (KubeClient, KubeConfig, error) {
	cfg, err := kubeConfig(h.reg.Get())
	if err != nil {
		return nil, cfg, err
	}
	c, err := h.connect(cfg)
	return c, cfg, err
}

// Rebuild lists the environment's soajs services (label soajs.content=true) and caches the
// cluster IP of each service and mdaemon by name and version.
func (h *HA) Rebuild() {
	c, cfg, err := h.client()
	if err != nil {
		h.logger.Error(err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), kubeTimeout)
	defer cancel()
	items, err := c.ListServices(ctx, cfg.Namespace, "soajs.content=true")
	if err != nil {
		h.logger.Error("Driver error: " + err.Error())
		return
	}
	if len(items) == 0 {
		h.logger.Error("Item not found!")
		return
	}
	cache := map[string]map[string]string{}
	for _, s := range items {
		labels := s.Labels
		if t := labels["soajs.service.type"]; t != "service" && t != "mdaemon" {
			continue
		}
		name := labels["soajs.service.name"]
		if name == "" || s.Spec.ClusterIP == "" {
			continue
		}
		version := labels["soajs.service.version"]
		if cache[name] == nil {
			cache[name] = map[string]string{}
		}
		cache[name][version] = s.Spec.ClusterIP
	}
	h.mu.Lock()
	h.cache = cache
	h.mu.Unlock()
}

// GetHost returns the gateway's own service name for the gateway; else the cached cluster IP;
// else, with SOAJS_DEPLOY_HA=kubernetes and a version, the service's name in the cluster; else the
// cluster IP from the API, of the latest version when none is given.
func (h *HA) GetHost(serviceName, version string) (*types.HostInfo, error) {
	reg := h.reg.Get()
	if serviceName == "" || serviceName == h.gw.Name {
		host := gatewayHost(h.env, h.gw, reg)
		if host == "" {
			return nil, ErrNoHost
		}
		return hostInfo(reg, h.gw.Name, version, host), nil
	}
	h.mu.Lock()
	host := h.cache[serviceName][version]
	h.mu.Unlock()
	if host != "" {
		return hostInfo(reg, serviceName, version, host), nil
	}
	if os.Getenv("SOAJS_DEPLOY_HA") == "kubernetes" && version != "" {
		return hostInfo(reg, serviceName, version, h.env+"-"+serviceName+"-v"+version+"-service"), nil
	}
	if version == "" {
		v, ok := h.GetLatestVersionFromCluster(serviceName)
		if !ok || v == "" {
			return nil, ErrNoHost
		}
		version = v
	}
	c, cfg, err := h.client()
	if err != nil {
		h.logger.Error(err.Error())
		return nil, ErrNoHost
	}
	ctx, cancel := context.WithTimeout(context.Background(), kubeTimeout)
	defer cancel()
	name := h.env + "-" + serviceName + "-v" + sanitizeVersion(version) + "-service"
	svc, err := c.GetService(ctx, cfg.Namespace, name)
	if err != nil || svc == nil || svc.Spec.ClusterIP == "" {
		if err != nil {
			h.logger.Error("Driver error: " + err.Error())
		}
		return nil, ErrNoHost
	}
	h.mu.Lock()
	if h.cache[serviceName] == nil {
		h.cache[serviceName] = map[string]string{}
	}
	h.cache[serviceName][version] = svc.Spec.ClusterIP
	h.mu.Unlock()
	return hostInfo(reg, serviceName, version, svc.Spec.ClusterIP), nil
}

// GetLatestVersion is the latest version in the cache.
func (h *HA) GetLatestVersion(serviceName string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	latest := ""
	for v := range h.cache[serviceName] {
		if latest == "" || lib.IsLatest(v, latest) {
			latest = v
		}
	}
	return latest
}

// GetLatestVersionFromCluster is the latest soajs.service.version label of the service's
// Kubernetes services.
func (h *HA) GetLatestVersionFromCluster(serviceName string) (string, bool) {
	c, cfg, err := h.client()
	if err != nil {
		h.logger.Error(err.Error())
		return "", true
	}
	ctx, cancel := context.WithTimeout(context.Background(), kubeTimeout)
	defer cancel()
	items, err := c.ListServices(ctx, cfg.Namespace, "soajs.service.name="+serviceName)
	if err != nil {
		h.logger.Error("Driver error: " + err.Error())
		return "", true
	}
	latest := ""
	for _, s := range items {
		if v := s.Labels["soajs.service.version"]; v != "" {
			v = unsanitizeVersion(v)
			if latest == "" || lib.IsLatest(v, latest) {
				latest = v
			}
		}
	}
	if latest == "" {
		h.logger.Error("Unable to get latest version!")
	}
	return latest, true
}

// Stats: the health checks are Kubernetes' in HA mode.
func (h *HA) Stats() map[string]map[string]map[string]*HostStat { return nil }

func sanitizeVersion(v string) string   { return strings.Replace(v, ".", "x", 1) }
func unsanitizeVersion(v string) string { return strings.Replace(v, "x", ".", 1) }
