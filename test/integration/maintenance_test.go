package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// MaintenanceResponse represents a maintenance endpoint response
type MaintenanceResponse struct {
	Result  bool        `json:"result"`
	TS      int64       `json:"ts"`
	Service ServiceInfo `json:"service"`
	Data    interface{} `json:"data,omitempty"`
}

type ServiceInfo struct {
	Service string `json:"service"`
	Version string `json:"version"`
	Type    string `json:"type"`
	Route   string `json:"route"`
}

// TestMaintenanceEndpoints tests the maintenance endpoints
func TestMaintenanceEndpoints(t *testing.T) {
	// Skip if not running integration tests
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration tests. Set INTEGRATION_TEST=true to run.")
	}

	// Wait for controller to be ready
	if !waitForController(t, 10*time.Second) {
		t.Fatal("Controller not ready")
	}

	t.Run("Heartbeat", testHeartbeat)
	t.Run("GetRegistry", testGetRegistry)
	t.Run("AwarenessStat", testAwarenessStat)
	t.Run("LoadProvision", testLoadProvision)
	t.Run("ReloadRegistry", testReloadRegistry)
}

func testHeartbeat(t *testing.T) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/heartbeat", maintenancePort))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result MaintenanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !result.Result {
		t.Error("Expected result=true")
	}

	if result.Service.Service != "soajs.controller" {
		t.Errorf("Expected service=soajs.controller, got %s", result.Service.Service)
	}

	if result.Service.Route != "/heartbeat" {
		t.Errorf("Expected route=/heartbeat, got %s", result.Service.Route)
	}

	t.Logf("Heartbeat response: result=%v, service=%s", result.Result, result.Service.Service)
}

func testGetRegistry(t *testing.T) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/getRegistry?name=urac&type=service", maintenancePort))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result MaintenanceResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !result.Result {
		t.Errorf("Expected result=true, response: %s", string(body))
	}

	// Check that data contains expected fields
	if result.Data != nil {
		data, ok := result.Data.(map[string]interface{})
		if ok {
			if _, hasServices := data["services"]; !hasServices {
				t.Error("Expected 'services' in registry data")
			}
			if _, hasAwareness := data["awareness"]; !hasAwareness {
				t.Error("Expected 'awareness' in registry data")
			}
		}
	}

	t.Logf("GetRegistry response: result=%v", result.Result)
}

func testAwarenessStat(t *testing.T) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/awarenessStat", maintenancePort))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result MaintenanceResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !result.Result {
		t.Errorf("Expected result=true, response: %s", string(body))
	}

	// Check that data contains services
	if result.Data != nil {
		data, ok := result.Data.(map[string]interface{})
		if ok {
			if services, hasServices := data["services"]; hasServices {
				servicesMap, _ := services.(map[string]interface{})
				t.Logf("AwarenessStat: %d services registered", len(servicesMap))
			}
		}
	}
}

func testLoadProvision(t *testing.T) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/loadProvision", maintenancePort))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
		return
	}

	var result MaintenanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !result.Result {
		t.Error("Expected result=true")
	}

	t.Logf("LoadProvision response: result=%v", result.Result)
}

func testReloadRegistry(t *testing.T) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/reloadRegistry", maintenancePort))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
		return
	}

	body, _ := io.ReadAll(resp.Body)
	var result MaintenanceResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !result.Result {
		t.Errorf("Expected result=true, response: %s", string(body))
	}

	t.Logf("ReloadRegistry response: result=%v", result.Result)
}
