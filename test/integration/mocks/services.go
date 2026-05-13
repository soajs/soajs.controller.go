package mocks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// MockService represents a mock backend service
type MockService struct {
	Name        string
	ServicePort int
	MaintPort   int
	sServer     *http.Server
	mServer     *http.Server
}

// ServiceResponse represents a standard service response
type ServiceResponse struct {
	Result bool                   `json:"result"`
	Data   map[string]interface{} `json:"data,omitempty"`
}

// MaintenanceResponse represents a maintenance endpoint response
type MaintenanceResponse struct {
	Result  bool        `json:"result"`
	TS      int64       `json:"ts"`
	Service ServiceInfo `json:"service"`
}

type ServiceInfo struct {
	Service string `json:"service"`
	Type    string `json:"type"`
	Route   string `json:"route"`
}

// NewUracMock creates a mock URAC service
func NewUracMock(servicePort, maintPort int) *MockService {
	return &MockService{
		Name:        "urac",
		ServicePort: servicePort,
		MaintPort:   maintPort,
	}
}

// NewOAuthMock creates a mock OAuth service
func NewOAuthMock(servicePort, maintPort int) *MockService {
	return &MockService{
		Name:        "oauth",
		ServicePort: servicePort,
		MaintPort:   maintPort,
	}
}

// NewPassthroughMock creates a mock passthrough service
func NewPassthroughMock(name string, servicePort, maintPort int) *MockService {
	return &MockService{
		Name:        name,
		ServicePort: servicePort,
		MaintPort:   maintPort,
	}
}

// Start starts the mock service
func (m *MockService) Start() error {
	// Service mux
	sMux := http.NewServeMux()
	m.registerServiceRoutes(sMux)

	// Maintenance mux
	mMux := http.NewServeMux()
	m.registerMaintenanceRoutes(mMux)

	// Start service server
	m.sServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", m.ServicePort),
		Handler: sMux,
	}

	// Start maintenance server
	m.mServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", m.MaintPort),
		Handler: mMux,
	}

	// Start servers in goroutines
	go func() {
		if err := m.sServer.ListenAndServe(); err != http.ErrServerClosed {
			fmt.Printf("Mock %s service error: %v\n", m.Name, err)
		}
	}()

	go func() {
		if err := m.mServer.ListenAndServe(); err != http.ErrServerClosed {
			fmt.Printf("Mock %s maintenance error: %v\n", m.Name, err)
		}
	}()

	// Wait for servers to start
	time.Sleep(100 * time.Millisecond)

	fmt.Printf("Mock %s started on ports %d (service) and %d (maintenance)\n",
		m.Name, m.ServicePort, m.MaintPort)

	return nil
}

// Stop stops the mock service
func (m *MockService) Stop() {
	if m.sServer != nil {
		m.sServer.Close()
	}
	if m.mServer != nil {
		m.mServer.Close()
	}
	fmt.Printf("Mock %s stopped\n", m.Name)
}

func (m *MockService) registerServiceRoutes(mux *http.ServeMux) {
	switch m.Name {
	case "urac":
		m.registerUracRoutes(mux)
	case "oauth":
		m.registerOAuthRoutes(mux)
	default:
		m.registerPassthroughRoutes(mux)
	}
}

func (m *MockService) registerUracRoutes(mux *http.ServeMux) {
	// GET /user
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		resp := ServiceResponse{
			Result: true,
			Data: map[string]interface{}{
				"firstname": "antoine",
				"lastname":  "hage",
			},
		}
		writeJSON(w, resp)
	})

	// GET /luser (with delay)
	mux.HandleFunc("/luser", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // Shorter delay for tests
		resp := ServiceResponse{
			Result: true,
			Data: map[string]interface{}{
				"firstname": "antoine",
				"lastname":  "hage",
			},
		}
		writeJSON(w, resp)
	})

	// GET /muser/:id
	mux.HandleFunc("/muser/", func(w http.ResponseWriter, r *http.Request) {
		resp := ServiceResponse{
			Result: true,
			Data: map[string]interface{}{
				"firstname": "antoine",
				"lastname":  "hage",
				"id":        r.URL.Path + "?" + r.URL.RawQuery,
			},
		}
		writeJSON(w, resp)
	})

	// GET /private
	mux.HandleFunc("/private", func(w http.ResponseWriter, r *http.Request) {
		resp := ServiceResponse{
			Result: true,
			Data: map[string]interface{}{
				"firstname": "antoine",
				"lastname":  "hage",
			},
		}
		writeJSON(w, resp)
	})

	// Catch-all for other routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		resp := ServiceResponse{
			Result: true,
			Data: map[string]interface{}{
				"service": "urac",
				"path":    r.URL.Path,
				"method":  r.Method,
			},
		}
		writeJSON(w, resp)
	})
}

func (m *MockService) registerOAuthRoutes(mux *http.ServeMux) {
	// GET /passport/login
	mux.HandleFunc("/passport/login", func(w http.ResponseWriter, r *http.Request) {
		resp := ServiceResponse{
			Result: true,
			Data: map[string]interface{}{
				"firstname": "antoine",
				"lastname":  "hage",
			},
		}
		writeJSON(w, resp)
	})

	// POST /token
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"token_type":    "bearer",
			"access_token":  "mock_access_token_12345",
			"expires_in":    7200,
			"refresh_token": "mock_refresh_token_12345",
		}
		writeJSON(w, resp)
	})

	// Catch-all
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		resp := ServiceResponse{
			Result: true,
			Data: map[string]interface{}{
				"service": "oauth",
				"path":    r.URL.Path,
				"method":  r.Method,
			},
		}
		writeJSON(w, resp)
	})
}

func (m *MockService) registerPassthroughRoutes(mux *http.ServeMux) {
	// GET /CheckPhoneNumber
	mux.HandleFunc("/CheckPhoneNumber", func(w http.ResponseWriter, r *http.Request) {
		resp := ServiceResponse{
			Result: true,
			Data: map[string]interface{}{
				"type": "endpoint",
				"url":  r.URL.String(),
			},
		}
		writeJSON(w, resp)
	})

	// Catch-all
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		resp := ServiceResponse{
			Result: true,
			Data: map[string]interface{}{
				"service": m.Name,
				"path":    r.URL.Path,
				"method":  r.Method,
				"type":    "endpoint",
			},
		}
		writeJSON(w, resp)
	})
}

func (m *MockService) registerMaintenanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		resp := MaintenanceResponse{
			Result: true,
			TS:     time.Now().UnixMilli(),
			Service: ServiceInfo{
				Service: m.Name,
				Type:    "rest",
				Route:   "/heartbeat",
			},
		}
		writeJSON(w, resp)
	})
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// MockServices holds all mock services
type MockServices struct {
	Urac   *MockService
	OAuth  *MockService
	PT     *MockService
	PT2    *MockService
}

// StartAll starts all mock services
func (ms *MockServices) StartAll() error {
	if ms.Urac != nil {
		if err := ms.Urac.Start(); err != nil {
			return err
		}
	}
	if ms.OAuth != nil {
		if err := ms.OAuth.Start(); err != nil {
			return err
		}
	}
	if ms.PT != nil {
		if err := ms.PT.Start(); err != nil {
			return err
		}
	}
	if ms.PT2 != nil {
		if err := ms.PT2.Start(); err != nil {
			return err
		}
	}
	return nil
}

// StopAll stops all mock services
func (ms *MockServices) StopAll() {
	if ms.Urac != nil {
		ms.Urac.Stop()
	}
	if ms.OAuth != nil {
		ms.OAuth.Stop()
	}
	if ms.PT != nil {
		ms.PT.Stop()
	}
	if ms.PT2 != nil {
		ms.PT2.Stop()
	}
}

// NewDefaultMockServices creates mock services with default ports matching Node.js tests
func NewDefaultMockServices() *MockServices {
	return &MockServices{
		Urac:  NewUracMock(4001, 5002),
		OAuth: NewOAuthMock(4004, 5005),
		PT:    NewPassthroughMock("pt", 4002, 5003),
		PT2:   NewPassthroughMock("pt2", 4003, 5004),
	}
}
