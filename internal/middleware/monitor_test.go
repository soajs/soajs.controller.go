package middleware

import (
	"testing"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

func TestIsTokenRenewRequest(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		urlInfo     *types.URLInfo
		want        bool
	}{
		{
			name:        "oauth token path",
			serviceName: "oauth",
			urlInfo:     &types.URLInfo{ServicePath: "/token"},
			want:        true,
		},
		{
			name:        "oauth refresh token path",
			serviceName: "oauth",
			urlInfo:     &types.URLInfo{ServicePath: "/refresh/token"},
			want:        true,
		},
		{
			name:        "authenticator token path",
			serviceName: "authenticator",
			urlInfo:     &types.URLInfo{ServicePath: "/token"},
			want:        true,
		},
		{
			name:        "authenticator renew path",
			serviceName: "authenticator",
			urlInfo:     &types.URLInfo{ServicePath: "/renew"},
			want:        true,
		},
		{
			name:        "oauth non-renew path",
			serviceName: "oauth",
			urlInfo:     &types.URLInfo{ServicePath: "/authorize"},
			want:        false,
		},
		{
			name:        "other service token path",
			serviceName: "urac",
			urlInfo:     &types.URLInfo{ServicePath: "/token"},
			want:        false,
		},
		{
			name:        "nil urlInfo",
			serviceName: "oauth",
			urlInfo:     nil,
			want:        false,
		},
		{
			name:        "oauth access token path",
			serviceName: "oauth",
			urlInfo:     &types.URLInfo{ServicePath: "/access/token"},
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTokenRenewRequest(tt.serviceName, tt.urlInfo)
			if got != tt.want {
				t.Errorf("isTokenRenewRequest(%q, %v) = %v, want %v",
					tt.serviceName, tt.urlInfo, got, tt.want)
			}
		})
	}
}

func TestGetGotoServiceConfig(t *testing.T) {
	tests := []struct {
		name    string
		regData *types.RegistryData
		want    bool // whether RenewReqMonitorOff should be true
	}{
		{
			name:    "nil registry",
			regData: nil,
			want:    false,
		},
		{
			name: "renewReqMonitorOff true",
			regData: &types.RegistryData{
				CustomRegistry: map[string]interface{}{
					"gateway": map[string]interface{}{
						"value": map[string]interface{}{
							"gotoService": map[string]interface{}{
								"renewReqMonitorOff": true,
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "renewReqMonitorOff false",
			regData: &types.RegistryData{
				CustomRegistry: map[string]interface{}{
					"gateway": map[string]interface{}{
						"value": map[string]interface{}{
							"gotoService": map[string]interface{}{
								"renewReqMonitorOff": false,
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "no gotoService config",
			regData: &types.RegistryData{
				CustomRegistry: map[string]interface{}{
					"gateway": map[string]interface{}{
						"value": map[string]interface{}{},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := GetGotoServiceConfig(tt.regData)
			if tt.want {
				if config == nil || !config.RenewReqMonitorOff {
					t.Errorf("Expected RenewReqMonitorOff to be true")
				}
			} else {
				if config != nil && config.RenewReqMonitorOff {
					t.Errorf("Expected RenewReqMonitorOff to be false or config to be nil")
				}
			}
		})
	}
}
