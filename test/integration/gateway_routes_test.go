package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/soajs/soajs.controller.go/test/integration/mocks"
)

// Test configuration
const (
	controllerPort   = 4000
	maintenancePort  = 5001
	extKey           = "3d90163cf9d6b3076ad26aa5ed58556348069258e5c6c941ee0f18448b570ad1c5c790e2d2a1989680c55f4904e2005ff5f8e71606e4aa641e67882f4210ebbc5460ff305dcb36e6ec2a2299cf0448ef60b9e38f41950ec251c1cf41f05f3ce9"
	validAccessToken = "cfb209a91b23896820f510aadbf1f4284b512123"
)

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

var mockServices *mocks.MockServices

// TestMain sets up and tears down mock services
func TestMain(m *testing.M) {
	// Skip if not running integration tests
	if os.Getenv("INTEGRATION_TEST") != "true" {
		fmt.Println("Skipping integration tests. Set INTEGRATION_TEST=true to run.")
		os.Exit(0)
	}

	// Start mock services
	mockServices = mocks.NewDefaultMockServices()
	if err := mockServices.StartAll(); err != nil {
		fmt.Printf("Failed to start mock services: %v\n", err)
		os.Exit(1)
	}

	// Wait a bit for services to be ready
	time.Sleep(500 * time.Millisecond)

	// Run tests
	code := m.Run()

	// Stop mock services
	mockServices.StopAll()

	os.Exit(code)
}

// TestGatewayRoutes runs integration tests for gateway routes
func TestGatewayRoutes(t *testing.T) {
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
		t.Run("FullURI", testSoajsProxyFullURI)
	})

	t.Run("ServiceRouting", func(t *testing.T) {
		t.Run("UracUser", testUracUser)
		t.Run("UracMuser", testUracMuser)
		t.Run("OAuthPassport", testOAuthPassport)
		t.Run("PTEndpoint", testPTEndpoint)
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

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	t.Logf("Proxy response: %s", string(body))
}

// Test /soajs/proxy with full URI (like http://127.0.0.1:4002/CheckPhoneNumber)
func testSoajsProxyFullURI(t *testing.T) {
	proxyRoute := url.QueryEscape("http://127.0.0.1:4002/CheckPhoneNumber")
	reqURL := fmt.Sprintf("http://127.0.0.1:%d/soajs/proxy?proxyRoute=%s&access_token=%s",
		controllerPort, proxyRoute, validAccessToken)

	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("key", extKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	t.Logf("Proxy full URI response: %s", string(body))
}

// Test routing to urac /user endpoint
func testUracUser(t *testing.T) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/urac/user", controllerPort), nil)
	req.Header.Set("key", extKey)

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

	// Should get response from mock urac service
	if result["result"] != true {
		t.Errorf("Expected result=true, got: %s", string(body))
	}

	data, ok := result["data"].(map[string]interface{})
	if ok {
		if data["firstname"] != "antoine" {
			t.Errorf("Expected firstname=antoine, got %v", data["firstname"])
		}
	}

	t.Logf("Urac /user response: %s", string(body))
}

// Test routing to urac /muser/:id endpoint
func testUracMuser(t *testing.T) {
	reqURL := fmt.Sprintf("http://127.0.0.1:%d/urac/muser/12?access_token=%s",
		controllerPort, validAccessToken)

	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("key", extKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	t.Logf("Urac /muser response: %s", string(body))
}

// Test routing to oauth /passport/login endpoint
func testOAuthPassport(t *testing.T) {
	reqURL := fmt.Sprintf("http://127.0.0.1:%d/oauth/passport/login?access_token=%s",
		controllerPort, validAccessToken)

	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("key", extKey)

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
		t.Errorf("Expected result=true, got: %s", string(body))
	}

	t.Logf("OAuth /passport/login response: %s", string(body))
}

// Test routing to passthrough service
func testPTEndpoint(t *testing.T) {
	reqURL := fmt.Sprintf("http://127.0.0.1:%d/pt/CheckPhoneNumber?access_token=%s",
		controllerPort, validAccessToken)

	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("key", extKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	if result["result"] == true {
		data, ok := result["data"].(map[string]interface{})
		if ok && data["type"] != "endpoint" {
			t.Errorf("Expected type=endpoint, got %v", data["type"])
		}
	}

	t.Logf("PT /CheckPhoneNumber response: %s", string(body))
}
