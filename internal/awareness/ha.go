package awareness

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// HAAwareness implements Kubernetes-based service discovery
type HAAwareness struct {
	*DefaultAwareness
	k8sClient *kubernetes.Clientset
	namespace string
}

// NewHAAwareness creates a Kubernetes-aware awareness instance
func NewHAAwareness(base *DefaultAwareness) (*HAAwareness, error) {
	// Try to get in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		base.logger.Warn("not running in Kubernetes cluster, HA awareness disabled",
			zap.Error(err),
		)
		return &HAAwareness{
			DefaultAwareness: base,
		}, nil
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Get namespace from Kubernetes service account (standard location)
	namespace := "soajs"
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		namespace = strings.TrimSpace(string(data))
	}

	return &HAAwareness{
		DefaultAwareness: base,
		k8sClient:        clientset,
		namespace:        namespace,
	}, nil
}

// GetHost returns a service host from Kubernetes using DNS naming convention
// This matches Node.js behavior: just build the DNS hostname directly
// Format: {env}-{serviceName}-v{version}-service
func (a *HAAwareness) GetHost(serviceName, version string) (*types.HostInfo, error) {
	// Get environment from registry (matches Node.js behavior)
	env := "dev"
	if a.registry != nil {
		env = strings.ToLower(a.registry.GetEnvironment())
	}

	// Get service config from registry for port and latest version
	port := 0
	latestVersion := ""
	if a.registry != nil {
		regData := a.registry.Get()
		if regData != nil && regData.Services != nil {
			if svc, ok := regData.Services[serviceName]; ok {
				port = svc.Port
				// Get latest version from service hosts
				if svc.Hosts != nil && svc.Hosts.Latest != "" {
					latestVersion = svc.Hosts.Latest
				}
				// Fallback: find latest version from Versions map
				if latestVersion == "" && len(svc.Versions) > 0 {
					for v := range svc.Versions {
						if latestVersion == "" || v > latestVersion {
							latestVersion = v
						}
					}
				}
			}
		}
	}

	// Use latest version if version not provided or is default "1"
	// This matches Node.js behavior where it queries for latest version
	if version == "" || version == "1" {
		if latestVersion != "" {
			version = latestVersion
		} else if version == "" {
			version = "1"
		}
	}

	// Default port if not found in registry
	if port == 0 {
		port = 4000 // Default SOAJS service port
	}

	// Build Kubernetes DNS hostname matching Node.js convention
	// Node.js: return cb(env + "-" + serviceName + "-v" + version + "-service");
	hostname := fmt.Sprintf("%s-%s-v%s-service", env, serviceName, version)

	a.logger.Debug("built k8s service hostname",
		zap.String("serviceName", serviceName),
		zap.String("version", version),
		zap.String("latestVersion", latestVersion),
		zap.String("hostname", hostname),
		zap.Int("port", port),
	)

	return &types.HostInfo{
		Name:    serviceName,
		Version: version,
		Host:    hostname,
		Port:    port,
	}, nil
}

// GetStats returns awareness stats including Kubernetes info
func (a *HAAwareness) GetStats() *AwarenessStats {
	stats := a.DefaultAwareness.GetStats()

	if a.k8sClient != nil {
		// Add Kubernetes service info
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		services, err := a.k8sClient.CoreV1().Services(a.namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "soajs.service=true",
		})
		if err == nil {
			for _, svc := range services.Items {
				key := svc.Labels["soajs.service.name"] + ":" + svc.Labels["soajs.service.version"]
				if _, exists := stats.Services[key]; !exists {
					stats.Services[key] = &ServiceStats{
						Hosts: []HostStats{{
							Host:    svc.Spec.ClusterIP,
							Port:    int(svc.Spec.Ports[0].Port),
							Healthy: true,
						}},
						Healthy: 1,
					}
				}
			}
		}
	}

	return stats
}
