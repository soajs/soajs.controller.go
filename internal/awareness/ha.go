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

// GetHost returns a service host from Kubernetes
func (a *HAAwareness) GetHost(serviceName, version string) (*types.HostInfo, error) {
	if a.k8sClient == nil {
		return a.DefaultAwareness.GetHost(serviceName, version)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get environment from registry (matches Node.js behavior)
	env := "dev"
	if a.registry != nil {
		env = strings.ToLower(a.registry.GetEnvironment())
	}

	// Build service name matching Node.js convention: {env}-{serviceName}-v{version}-service
	// Example: dashboard-urac-v1-service
	svcName := fmt.Sprintf("%s-%s-v%s-service", env, serviceName, version)

	a.logger.Debug("looking up k8s service",
		zap.String("service", svcName),
		zap.String("namespace", a.namespace),
	)

	svc, err := a.k8sClient.CoreV1().Services(a.namespace).Get(ctx, svcName, metav1.GetOptions{})
	if err != nil {
		a.logger.Debug("k8s service not found, falling back to default awareness",
			zap.String("service", svcName),
			zap.Error(err),
		)
		// Fall back to default awareness
		return a.DefaultAwareness.GetHost(serviceName, version)
	}

	// Get cluster IP and port
	host := svc.Spec.ClusterIP
	var port int32
	if len(svc.Spec.Ports) > 0 {
		port = svc.Spec.Ports[0].Port
	}

	a.logger.Debug("found k8s service",
		zap.String("service", svcName),
		zap.String("host", host),
		zap.Int32("port", port),
	)

	return &types.HostInfo{
		Name:    serviceName,
		Version: version,
		Host:    host,
		Port:    int(port),
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
