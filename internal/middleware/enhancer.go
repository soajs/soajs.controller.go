package middleware

import (
	"net/http"
	"strings"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

// Enhancer middleware enriches the request with device and geo information
func Enhancer() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			if soajsCtx != nil {
				// Parse device info from User-Agent
				soajsCtx.Device = parseDeviceInfo(r.Header.Get("User-Agent"))

				// Parse geo info from headers (if behind a proxy/CDN)
				soajsCtx.Geo = parseGeoInfo(r)
			}

			next.ServeHTTP(w, r)
		})
	}
}

func parseDeviceInfo(userAgent string) *types.DeviceInfo {
	device := &types.DeviceInfo{
		UserAgent: userAgent,
	}

	ua := strings.ToLower(userAgent)

	// Detect device type
	switch {
	case strings.Contains(ua, "mobile"):
		device.Type = "mobile"
	case strings.Contains(ua, "tablet"):
		device.Type = "tablet"
	default:
		device.Type = "desktop"
	}

	// Detect OS
	switch {
	case strings.Contains(ua, "windows"):
		device.OS = "windows"
	case strings.Contains(ua, "mac"):
		device.OS = "macos"
	case strings.Contains(ua, "linux"):
		device.OS = "linux"
	case strings.Contains(ua, "android"):
		device.OS = "android"
	case strings.Contains(ua, "iphone"), strings.Contains(ua, "ipad"):
		device.OS = "ios"
	}

	// Detect browser
	switch {
	case strings.Contains(ua, "chrome"):
		device.Browser = "chrome"
	case strings.Contains(ua, "firefox"):
		device.Browser = "firefox"
	case strings.Contains(ua, "safari"):
		device.Browser = "safari"
	case strings.Contains(ua, "edge"):
		device.Browser = "edge"
	}

	return device
}

func parseGeoInfo(r *http.Request) *types.GeoInfo {
	geo := &types.GeoInfo{}

	// Check common geo headers from CDNs/proxies
	if country := r.Header.Get("CF-IPCountry"); country != "" {
		geo.Country = country
	} else if country := r.Header.Get("X-Country-Code"); country != "" {
		geo.Country = country
	}

	if city := r.Header.Get("CF-IPCity"); city != "" {
		geo.City = city
	} else if city := r.Header.Get("X-City"); city != "" {
		geo.City = city
	}

	return geo
}
