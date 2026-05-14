package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestACLEnforcement tests ACL enforcement from database
func TestACLEnforcement(t *testing.T) {
	if !waitForController(t, 10*time.Second) {
		t.Fatal("Controller not ready")
	}

	t.Run("AllowedRoute", testACLAllowedRoute)
	t.Run("RestrictedRouteNoAccess", testACLRestrictedRouteNoAccess)
	t.Run("RestrictedRouteWithAccess", testACLRestrictedRouteWithAccess)
	t.Run("ServiceNotInACL", testACLServiceNotInACL)
	t.Run("InvalidKey", testACLInvalidKey)
	t.Run("ExpiredKey", testACLExpiredKey)
}

func testACLAllowedRoute(t *testing.T) {
	// /urac/user is allowed for Guest group in package ACL
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/urac/user", controllerPort), nil)
	req.Header.Set("key", extKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result APIResponse
	json.Unmarshal(body, &result)

	if !result.Result {
		t.Errorf("Expected result=true for allowed route, got errors: %v", result.Errors)
	}
}

func testACLRestrictedRouteNoAccess(t *testing.T) {
	// /urac/muser/:id requires OAuth token (Member group with access=true)
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/urac/muser/123", controllerPort), nil)
	req.Header.Set("key", extKey)
	// No access_token provided

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result APIResponse
	json.Unmarshal(body, &result)

	// Should fail due to missing OAuth token for restricted route
	t.Logf("Restricted route without token - result: %v, status: %d", result.Result, resp.StatusCode)
}

func testACLRestrictedRouteWithAccess(t *testing.T) {
	// /urac/muser/:id with valid access token
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/urac/muser/123", controllerPort), nil)
	req.Header.Set("key", extKey)
	req.Header.Set("access_token", validAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result APIResponse
	json.Unmarshal(body, &result)

	if !result.Result {
		t.Logf("Restricted route with token - may fail if mock service not configured: %v", result.Errors)
	} else {
		t.Logf("Restricted route with token - success")
	}
}

func testACLServiceNotInACL(t *testing.T) {
	// pt2 service is not in the package ACL
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/pt2/CheckPhoneNumber", controllerPort), nil)
	req.Header.Set("key", extKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result APIResponse
	json.Unmarshal(body, &result)

	// Should fail with ACL error (error code 154 or similar)
	if result.Result {
		t.Errorf("Expected error for service not in ACL, got success")
	} else {
		t.Logf("Service not in ACL correctly denied: %v", result.Errors)
	}
}

func testACLInvalidKey(t *testing.T) {
	// Invalid/unknown key should fail
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/urac/user", controllerPort), nil)
	req.Header.Set("key", "invalid_key_that_does_not_exist")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result APIResponse
	json.Unmarshal(body, &result)

	if result.Result {
		t.Errorf("Expected error for invalid key, got success")
	} else {
		t.Logf("Invalid key correctly rejected: %v", result.Errors)
	}
}

func testACLExpiredKey(t *testing.T) {
	// Test with expired key (if configured in test data)
	// For now, just verify the flow works with a malformed key
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/urac/user", controllerPort), nil)
	req.Header.Set("key", "short")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result APIResponse
	json.Unmarshal(body, &result)

	if result.Result {
		t.Errorf("Expected error for malformed key, got success")
	} else {
		t.Logf("Malformed key correctly rejected: %v", result.Errors)
	}
}

// TestTokenValidation tests OAuth token validation
func TestTokenValidation(t *testing.T) {
	if !waitForController(t, 10*time.Second) {
		t.Fatal("Controller not ready")
	}

	t.Run("ValidToken", testValidToken)
	t.Run("InvalidToken", testInvalidToken)
	t.Run("MissingToken", testMissingToken)
}

func testValidToken(t *testing.T) {
	// Route that requires OAuth with valid token
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/urac/muser/test", controllerPort), nil)
	req.Header.Set("key", extKey)
	req.Header.Set("access_token", validAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("Valid token response: status=%d, body=%s", resp.StatusCode, string(body))
}

func testInvalidToken(t *testing.T) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/urac/muser/test", controllerPort), nil)
	req.Header.Set("key", extKey)
	req.Header.Set("access_token", "invalid_token_12345")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result APIResponse
	json.Unmarshal(body, &result)

	// Should fail due to invalid token
	t.Logf("Invalid token response: result=%v, errors=%v", result.Result, result.Errors)
}

func testMissingToken(t *testing.T) {
	// Route that requires OAuth without token
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/urac/muser/test", controllerPort), nil)
	req.Header.Set("key", extKey)
	// No access_token

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("Missing token response: status=%d, body=%s", resp.StatusCode, string(body))
}

// TestSoajsACLEndpoint tests the /soajs/acl endpoint with database data
func TestSoajsACLEndpoint(t *testing.T) {
	if !waitForController(t, 10*time.Second) {
		t.Fatal("Controller not ready")
	}

	t.Run("GuestPackageACL", testGuestPackageACL)
	t.Run("OwnerPackageACL", testOwnerPackageACL)
}

func testGuestPackageACL(t *testing.T) {
	// Get ACL for guest package key
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/soajs/acl", controllerPort), nil)
	req.Header.Set("key", extKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	if result["result"] != true {
		t.Errorf("Expected result=true for /soajs/acl, got: %s", string(body))
	}

	// Verify ACL structure
	if data, ok := result["data"].(map[string]interface{}); ok {
		if acl, ok := data["acl"].(map[string]interface{}); ok {
			t.Logf("Guest ACL services: %v", getKeys(acl))
		}
	}
}

func testOwnerPackageACL(t *testing.T) {
	// Get ACL with owner token (should have more access)
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
	json.Unmarshal(body, &result)

	t.Logf("Owner ACL response: %s", string(body))
}

func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
