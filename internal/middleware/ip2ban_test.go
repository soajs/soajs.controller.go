package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestIP2Ban_AllowsUnbannedIP(t *testing.T) {
	// Reset ban store
	banStore.mu.Lock()
	banStore.blacklist = make(map[string]bool)
	banStore.whitelist = make(map[string]bool)
	banStore.mu.Unlock()

	logger, _ := zap.NewDevelopment()

	contextMW := Context(logger, "test")
	ip2banMW := IP2Ban()

	handler := contextMW(ip2banMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestIP2Ban_BlocksBannedIP(t *testing.T) {
	// Reset and ban an IP
	banStore.mu.Lock()
	banStore.blacklist = make(map[string]bool)
	banStore.whitelist = make(map[string]bool)
	banStore.blacklist["192.168.1.100"] = true
	banStore.mu.Unlock()

	logger, _ := zap.NewDevelopment()

	contextMW := Context(logger, "test")
	ip2banMW := IP2Ban()

	handler := contextMW(ip2banMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestIP2Ban_WhitelistOverridesBlacklist(t *testing.T) {
	// Reset and add IP to both lists
	banStore.mu.Lock()
	banStore.blacklist = make(map[string]bool)
	banStore.whitelist = make(map[string]bool)
	banStore.blacklist["192.168.1.50"] = true
	banStore.whitelist["192.168.1.50"] = true
	banStore.mu.Unlock()

	logger, _ := zap.NewDevelopment()

	contextMW := Context(logger, "test")
	ip2banMW := IP2Ban()

	handler := contextMW(ip2banMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.50:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d (whitelist should override)", rec.Code, http.StatusOK)
	}
}

func TestBanIP(t *testing.T) {
	// Reset
	banStore.mu.Lock()
	banStore.blacklist = make(map[string]bool)
	banStore.mu.Unlock()

	BanIP("10.0.0.1")

	banStore.mu.RLock()
	banned := banStore.blacklist["10.0.0.1"]
	banStore.mu.RUnlock()

	if !banned {
		t.Error("IP should be banned")
	}
}

func TestUnbanIP(t *testing.T) {
	// Reset and ban
	banStore.mu.Lock()
	banStore.blacklist = make(map[string]bool)
	banStore.blacklist["10.0.0.2"] = true
	banStore.mu.Unlock()

	UnbanIP("10.0.0.2")

	banStore.mu.RLock()
	banned := banStore.blacklist["10.0.0.2"]
	banStore.mu.RUnlock()

	if banned {
		t.Error("IP should not be banned after unban")
	}
}

func TestWhitelistIP(t *testing.T) {
	// Reset
	banStore.mu.Lock()
	banStore.whitelist = make(map[string]bool)
	banStore.mu.Unlock()

	WhitelistIP("10.0.0.3")

	banStore.mu.RLock()
	whitelisted := banStore.whitelist["10.0.0.3"]
	banStore.mu.RUnlock()

	if !whitelisted {
		t.Error("IP should be whitelisted")
	}
}

func TestGetClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18, 150.172.238.178")

	ip := getClientIP(req)

	if ip != "203.0.113.195" {
		t.Errorf("IP = %q, want %q", ip, "203.0.113.195")
	}
}

func TestGetClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Real-IP", "192.168.1.1")

	ip := getClientIP(req)

	if ip != "192.168.1.1" {
		t.Errorf("IP = %q, want %q", ip, "192.168.1.1")
	}
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:8080"

	ip := getClientIP(req)

	if ip != "127.0.0.1" {
		t.Errorf("IP = %q, want %q", ip, "127.0.0.1")
	}
}
