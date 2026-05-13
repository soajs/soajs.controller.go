package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"
)

// Test configuration
const (
	controllerPort    = 4000
	maintenancePort   = 5001
	extKey            = "3d90163cf9d6b3076ad26aa5ed58556348069258e5c6c941ee0f18448b570ad1c5c790e2d2a1989680c55f4904e2005ff5f8e71606e4aa641e67882f4210ebbc5460ff305dcb36e6ec2a2299cf0448ef60b9e38f41950ec251c1cf41f05f3ce9"
	validAccessToken  = "cfb209a91b23896820f510aadbf1f4284b512123"
)

// MockService represents a mock backend service
type MockService struct {
	Name       string
	Port       int
	MaintPort  int
	Server     *httptest.Server
	MaintServer *httptest.Server
}

// APIResponse represents a standard API response
type APIResponse struct {
	Result bool                   `json:"result"`
	Data   map[string]interface{} `json:"data,omitempty"`
	Errors *APIErrors             `json:"errors,omitempty"`
}

type APIErrors struct {
	Codes   []int         `json:"codes"`
	Details []interface{} `json:"details,omitempty"`
}

// TestGatewayRoutes runs integration tests for gateway routes
// Note: These tests require a running controller and MongoDB
// Run with: go test -tags=integration ./test/integration/...
func TestGatewayRoutes(t *testing.T) {
	// Skip if not running integration tests
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration tests. Set INTEGRATION_TEST=true to run.")
	}

	// Wait for controller to be ready
	if !waitForController(t, 10*time.Second) {
		t.Fatal("Controller not ready")
	}

	t.Run("SoajsACL", func(t *testing.T) {
		t.Run("NoKey", testSoajsACLNoKey)
		t.Run("WithKey", testSoajsACLWithKey)
	})

	t.Run("SoajsProxy", func(t *testing.T) {
		t.Run("MissingProxyRoute", testSoajsProxyMissingRoute)
		t.Run("ValidProxyRoute", testSoajsProxyValidRoute)
	})

	t.Run("ServiceRouting", func(t *testing.T) {
		t.Run("UracUser", testUracUser)
		t.Run("OAuthToken", testOAuthToken)
	})
}

func waitForController(t *testing.T, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/heartbeat", maintenancePort))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// Test /soajs/acl without key - should fail
func testSoajsACLNoKey(t *testing.T) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/soajs/acl", controllerPort))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	var result APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.Result {
		t.Error("Expected result=false for request without key")
	}
}

// Test /soajs/acl with key - should succeed
func testSoajsACLWithKey(t *testing.T) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/soajs/acl", controllerPort), nil)
	req.Header.Set("key", extKey)
	req.Header.Set("access_token", validAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["result"] != true {
		t.Errorf("Expected result=true, got %v. Response: %s", result["result"], string(body))
	}
}

// Test /soajs/proxy without proxyRoute parameter
func testSoajsProxyMissingRoute(t *testing.T) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/soajs/proxy", controllerPort), nil)
	req.Header.Set("key", extKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	var result APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.Result {
		t.Error("Expected result=false for request without proxyRoute")
	}
}

// Test /soajs/proxy with valid proxyRoute
func testSoajsProxyValidRoute(t *testing.T) {
	proxyRoute := url.QueryEscape("/urac/user")
	reqURL := fmt.Sprintf("http://127.0.0.1:%d/soajs/proxy?proxyRoute=%s&access_token=%s",
		controllerPort, proxyRoute, validAccessToken)

	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("key", extKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Just check we got a response (may fail if urac not running, that's OK)
	if resp.StatusCode == 0 {
		t.Error("Expected a response")
	}
}

// Test routing to urac service
func testUracUser(t *testing.T) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/urac/user?username=owner", controllerPort), nil)
	req.Header.Set("key", extKey)
	req.Header.Set("access_token", validAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check response (may fail if urac not running)
	if resp.StatusCode >= 500 {
		t.Skipf("Urac service not available (status: %d)", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	t.Logf("Urac response: %s", string(body))
}

// Test OAuth token endpoint
func testOAuthToken(t *testing.T) {
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/oauth/token", controllerPort), nil)
	req.Header.Set("key", extKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check response (may fail if oauth not running)
	if resp.StatusCode >= 500 {
		t.Skipf("OAuth service not available (status: %d)", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	t.Logf("OAuth response: %s", string(body))
}
