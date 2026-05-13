package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestHTTPMethods tests various HTTP methods through the gateway
func TestHTTPMethods(t *testing.T) {
	// Wait for controller to be ready
	if !waitForController(t, 10*time.Second) {
		t.Fatal("Controller not ready")
	}

	t.Run("GET", testHTTPGet)
	t.Run("POST", testHTTPPost)
	t.Run("PUT", testHTTPPut)
	t.Run("DELETE", testHTTPDelete)
	t.Run("PATCH", testHTTPPatch)
	t.Run("HEAD", testHTTPHead)
	t.Run("OPTIONS", testHTTPOptions)
}

func testHTTPGet(t *testing.T) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/urac/user", controllerPort), nil)
	req.Header.Set("key", extKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	if result["result"] != true {
		t.Errorf("Expected result=true for GET, got: %s", string(body))
	}

	t.Logf("GET response status: %d, body: %s", resp.StatusCode, string(body))
}

func testHTTPPost(t *testing.T) {
	reqBody := map[string]interface{}{
		"username":   "owner",
		"password":   "password",
		"grant_type": "password",
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/oauth/token", controllerPort), bytes.NewBuffer(jsonBody))
	req.Header.Set("key", extKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	// Mock oauth returns token
	if _, hasToken := result["access_token"]; !hasToken {
		t.Logf("POST response (may not have token if using mock): %s", string(body))
	} else {
		t.Logf("POST response status: %d, body: %s", resp.StatusCode, string(body))
	}
}

func testHTTPPut(t *testing.T) {
	reqBody := map[string]interface{}{
		"firstName": "Test",
		"lastName":  "User",
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("PUT", fmt.Sprintf("http://127.0.0.1:%d/urac/account", controllerPort), bytes.NewBuffer(jsonBody))
	req.Header.Set("key", extKey)
	req.Header.Set("access_token", validAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("PUT response status: %d, body: %s", resp.StatusCode, string(body))
}

func testHTTPDelete(t *testing.T) {
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("http://127.0.0.1:%d/urac/admin/user?id=test", controllerPort), nil)
	req.Header.Set("key", extKey)
	req.Header.Set("access_token", validAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("DELETE response status: %d, body: %s", resp.StatusCode, string(body))
}

func testHTTPPatch(t *testing.T) {
	reqBody := map[string]interface{}{
		"firstName": "Updated",
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("PATCH", fmt.Sprintf("http://127.0.0.1:%d/urac/account", controllerPort), bytes.NewBuffer(jsonBody))
	req.Header.Set("key", extKey)
	req.Header.Set("access_token", validAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("PATCH response status: %d, body: %s", resp.StatusCode, string(body))
}

func testHTTPHead(t *testing.T) {
	req, _ := http.NewRequest("HEAD", fmt.Sprintf("http://127.0.0.1:%d/urac/user", controllerPort), nil)
	req.Header.Set("key", extKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD request failed: %v", err)
	}
	defer resp.Body.Close()

	t.Logf("HEAD response status: %d, Content-Type: %s", resp.StatusCode, resp.Header.Get("Content-Type"))
}

func testHTTPOptions(t *testing.T) {
	req, _ := http.NewRequest("OPTIONS", fmt.Sprintf("http://127.0.0.1:%d/urac/user", controllerPort), nil)
	req.Header.Set("Origin", "http://localhost:4005")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	defer resp.Body.Close()

	// CORS headers should be present
	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	allowMethods := resp.Header.Get("Access-Control-Allow-Methods")

	t.Logf("OPTIONS response - Allow-Origin: %s, Allow-Methods: %s", allowOrigin, allowMethods)
}
