package middleware

import (
	"testing"
)

func TestShouldTriggerLastSeenCheck(t *testing.T) {
	tests := []struct {
		name        string
		include     map[string]interface{}
		serviceName string
		apiPath     string
		method      string
		want        bool
	}{
		{
			name:        "nil include triggers for all",
			include:     nil,
			serviceName: "chat",
			apiPath:     "/messages",
			method:      "POST",
			want:        true,
		},
		{
			name: "service not in include",
			include: map[string]interface{}{
				"chat": map[string]interface{}{
					"apis": map[string]interface{}{
						"/messages": []interface{}{"post"},
					},
				},
			},
			serviceName: "urac",
			apiPath:     "/user",
			method:      "GET",
			want:        false,
		},
		{
			name: "service with matching API and method",
			include: map[string]interface{}{
				"chat": map[string]interface{}{
					"apis": map[string]interface{}{
						"/messages": []interface{}{"post"},
					},
				},
			},
			serviceName: "chat",
			apiPath:     "/messages",
			method:      "POST",
			want:        true,
		},
		{
			name: "service with matching API but wrong method",
			include: map[string]interface{}{
				"chat": map[string]interface{}{
					"apis": map[string]interface{}{
						"/messages": []interface{}{"post"},
					},
				},
			},
			serviceName: "chat",
			apiPath:     "/messages",
			method:      "GET",
			want:        false,
		},
		{
			name: "service with non-matching API",
			include: map[string]interface{}{
				"chat": map[string]interface{}{
					"apis": map[string]interface{}{
						"/messages": []interface{}{"post"},
					},
				},
			},
			serviceName: "chat",
			apiPath:     "/other",
			method:      "POST",
			want:        false,
		},
		{
			name: "service with bool true triggers all",
			include: map[string]interface{}{
				"chat": true,
			},
			serviceName: "chat",
			apiPath:     "/anything",
			method:      "DELETE",
			want:        true,
		},
		{
			name: "connectspaces active GET",
			include: map[string]interface{}{
				"connectspaces": map[string]interface{}{
					"apis": map[string]interface{}{
						"/active": []interface{}{"get"},
						"/me":     []interface{}{"get"},
					},
				},
			},
			serviceName: "connectspaces",
			apiPath:     "/active",
			method:      "GET",
			want:        true,
		},
		{
			name: "av with path param",
			include: map[string]interface{}{
				"av": map[string]interface{}{
					"apis": map[string]interface{}{
						"/get/calls/room/:roomId": []interface{}{"post"},
					},
				},
			},
			serviceName: "av",
			apiPath:     "/get/calls/room/12345",
			method:      "POST",
			want:        true,
		},
		{
			name: "av with path param wrong method",
			include: map[string]interface{}{
				"av": map[string]interface{}{
					"apis": map[string]interface{}{
						"/get/calls/room/:roomId": []interface{}{"post"},
					},
				},
			},
			serviceName: "av",
			apiPath:     "/get/calls/room/12345",
			method:      "GET",
			want:        false,
		},
		{
			name: "wildcard API matches all paths",
			include: map[string]interface{}{
				"chat": map[string]interface{}{
					"apis": map[string]interface{}{
						"*": true,
					},
				},
			},
			serviceName: "chat",
			apiPath:     "/any/path/here",
			method:      "PUT",
			want:        true,
		},
		{
			name: "method case insensitive",
			include: map[string]interface{}{
				"chat": map[string]interface{}{
					"apis": map[string]interface{}{
						"/messages": []interface{}{"POST"},
					},
				},
			},
			serviceName: "chat",
			apiPath:     "/messages",
			method:      "post",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldTriggerLastSeenCheck(tt.include, tt.serviceName, tt.apiPath, tt.method, nil)
			if got != tt.want {
				t.Errorf("shouldTriggerLastSeenCheck() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsLastSeenMethodMatch(t *testing.T) {
	tests := []struct {
		name         string
		methodConfig interface{}
		method       string
		want         bool
	}{
		{
			name:         "bool true matches all",
			methodConfig: true,
			method:       "DELETE",
			want:         true,
		},
		{
			name:         "bool false matches none",
			methodConfig: false,
			method:       "GET",
			want:         false,
		},
		{
			name:         "array with matching method",
			methodConfig: []interface{}{"get", "post"},
			method:       "GET",
			want:         true,
		},
		{
			name:         "array without matching method",
			methodConfig: []interface{}{"get", "post"},
			method:       "DELETE",
			want:         false,
		},
		{
			name:         "nil config",
			methodConfig: nil,
			method:       "GET",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLastSeenMethodMatch(tt.methodConfig, tt.method)
			if got != tt.want {
				t.Errorf("isLastSeenMethodMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetLastSeenRouteRegExp(t *testing.T) {
	tests := []struct {
		route   string
		path    string
		matches bool
	}{
		{"/user/:id", "/user/123", true},
		{"/user/:id", "/user/abc-def", true},
		{"/user/:id", "/user/", false},
		{"/user/:id/profile", "/user/123/profile", true},
		{"/get/calls/room/:roomId", "/get/calls/room/xyz123", true},
		{"/get/calls/room/:roomId", "/get/calls/room/", false},
	}

	for _, tt := range tests {
		t.Run(tt.route+"->"+tt.path, func(t *testing.T) {
			re := getLastSeenRouteRegExp(tt.route)
			if re == nil {
				t.Fatal("Failed to compile regex")
			}
			got := re.MatchString(tt.path)
			if got != tt.matches {
				t.Errorf("route %q matching %q = %v, want %v", tt.route, tt.path, got, tt.matches)
			}
		})
	}
}

func TestGetLastSeenConfig(t *testing.T) {
	tests := []struct {
		name           string
		customRegistry map[string]interface{}
		wantActive     bool
		wantNil        bool
	}{
		{
			name:           "nil registry",
			customRegistry: nil,
			wantNil:        true,
		},
		{
			name: "active true",
			customRegistry: map[string]interface{}{
				"gateway": map[string]interface{}{
					"value": map[string]interface{}{
						"lastSeen": map[string]interface{}{
							"active": true,
						},
					},
				},
			},
			wantActive: true,
		},
		{
			name: "active false",
			customRegistry: map[string]interface{}{
				"gateway": map[string]interface{}{
					"value": map[string]interface{}{
						"lastSeen": map[string]interface{}{
							"active": false,
						},
					},
				},
			},
			wantActive: false,
		},
		{
			name: "with include config",
			customRegistry: map[string]interface{}{
				"gateway": map[string]interface{}{
					"value": map[string]interface{}{
						"lastSeen": map[string]interface{}{
							"active": true,
							"include": map[string]interface{}{
								"chat": map[string]interface{}{
									"apis": map[string]interface{}{
										"/messages": []interface{}{"post"},
									},
								},
							},
						},
					},
				},
			},
			wantActive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := getLastSeenConfig(tt.customRegistry)
			if tt.wantNil {
				if config != nil {
					t.Error("Expected nil config")
				}
				return
			}
			if config == nil {
				t.Fatal("Expected non-nil config")
			}
			if config.Active != tt.wantActive {
				t.Errorf("Active = %v, want %v", config.Active, tt.wantActive)
			}
		})
	}
}
