package awareness

import (
	"fmt"
	"net/http"
	"time"

	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// CustomAwareness implements custom service discovery mode
type CustomAwareness struct {
	*DefaultAwareness
	httpClient *http.Client
}

// NewCustomAwareness creates a custom awareness instance
func NewCustomAwareness(base *DefaultAwareness) *CustomAwareness {
	return &CustomAwareness{
		DefaultAwareness: base,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// HealthCheck performs HTTP health check on a service
func (a *CustomAwareness) HealthCheck(serviceName, host string) (*HealthStatus, error) {
	url := fmt.Sprintf("http://%s/heartbeat", host)

	start := time.Now()
	resp, err := a.httpClient.Get(url)
	latency := time.Since(start)

	status := &HealthStatus{
		LastCheck: time.Now(),
		Latency:   latency,
	}

	if err != nil {
		status.Healthy = false
		status.Error = err.Error()
		return status, nil
	}
	defer resp.Body.Close()

	status.Healthy = resp.StatusCode == http.StatusOK

	return status, nil
}

// RegisterService registers a service and performs initial health check
func (a *CustomAwareness) RegisterService(info types.ServiceInfo) error {
	// Perform initial health check
	host := fmt.Sprintf("%s:%d", info.Host, info.Port)
	status, err := a.HealthCheck(info.Name, host)
	if err != nil {
		a.logger.Warn("initial health check failed",
			zap.String("service", info.Name),
			zap.String("host", host),
			zap.Error(err),
		)
	}

	// Register regardless of health status
	if err := a.DefaultAwareness.RegisterService(info); err != nil {
		return err
	}

	// Update health status if check was successful
	if status != nil {
		key := info.Name + ":" + info.Version
		a.mu.Lock()
		for _, entry := range a.hosts[key] {
			if entry.info.Host == info.Host && entry.info.Port == info.Port {
				entry.healthy = status.Healthy
				entry.lastCheck = status.LastCheck
				entry.latency = status.Latency
				break
			}
		}
		a.mu.Unlock()
	}

	return nil
}
