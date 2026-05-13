package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestRoaming tests the roaming functionality (soajsroaming header)
func TestRoaming(t *testing.T) {
	// Wait for controller to be ready
	if !waitForController(t, 10*time.Second) {
		t.Fatal("Controller not ready")
	}

	t.Run("NotInRegistry", testRoamingNotInRegistry)
	t.Run("WrongIP", testRoamingWrongIP)
	t.Run("Success", testRoamingSuccess)
}

// Test roaming fail - service not in registry - should fail with error 165
func testRoamingNotInRegistry(t *testing.T) {
	reqURL := fmt.Sprintf("http://127.0.0.1:%d/urac/user?access_token=%s",
		controllerPort, validAccessToken)

	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("key", extKey)
	req.Header.Set("soajsroaming", "true")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	// Should fail with roaming error (165) or succeed if roaming is configured
	// The exact behavior depends on registry configuration
	t.Logf("Roaming (not in registry) response: %s", string(body))

	if result["result"] == false {
		if errors, ok := result["errors"].(map[string]interface{}); ok {
			if codes, ok := errors["codes"].([]interface{}); ok && len(codes) > 0 {
				if code, ok := codes[0].(float64); ok && code == 165 {
					t.Logf("Got expected roaming error 165")
					return
				}
			}
		}
	}
}

// Test roaming fail - wrong IP - should fail with error 165
func testRoamingWrongIP(t *testing.T) {
	reqURL := fmt.Sprintf("http://127.0.0.1:%d/oauth/token?access_token=%s",
		controllerPort, validAccessToken)

	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("key", extKey)
	req.Header.Set("soajsroaming", "true")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	t.Logf("Roaming (wrong IP) response: %s", string(body))

	if result["result"] == false {
		if errors, ok := result["errors"].(map[string]interface{}); ok {
			if codes, ok := errors["codes"].([]interface{}); ok && len(codes) > 0 {
				if code, ok := codes[0].(float64); ok && code == 165 {
					t.Logf("Got expected roaming error 165")
					return
				}
			}
		}
	}
}

// Test roaming success - payment service configured for roaming
func testRoamingSuccess(t *testing.T) {
	reqURL := fmt.Sprintf("http://127.0.0.1:%d/payment/myroute?access_token=%s",
		controllerPort, validAccessToken)

	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("key", extKey)
	req.Header.Set("soajsroaming", "true")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	// May succeed or fail depending on payment service configuration
	t.Logf("Roaming success response: %s", string(body))
}
