package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/internal/soajserr"
	"github.com/soajs/soajs.controller.go/pkg/security"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// proxyTransport sends requests to services. There is no overall timeout: the request timeout is
// the idle monitor of redirect.
var proxyTransport http.RoundTripper = &http.Transport{
	DialContext:         (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 10,
	IdleConnTimeout:     90 * time.Second,
}

// GotoService finds the service of the request and how to call it, as the Node.js gotoService
// does (mw/gotoService/index.js, lib/extractBuildParameters.js):
//   - the service must be in the registry with a port, and with hosts unless in HA (else 130);
//   - its version is the requested one, or the one keyACL took from the package, else the latest
//     awareness knows, else in HA the latest in the cluster (else 130);
//   - a version with extKeyRequired needs a key that decrypts (else 132);
//   - the oauth passport login is passed through (simpleRTS), a soajsroaming request is answered
//     with the injectObj (roaming), any other is redirected to the service.
//
// The gateway's own routes: /soajs/acl and /soajs/mcp answer 132 without a key, else are checked
// as APIs of the gateway and answered with the caller's ACL, and MCP; /soajs/proxy forwards the request to its
// proxyRoute, a key that is sent having to decrypt (else 132); any other /soajs route is not a
// service (130).
func GotoService(reg registry.Registry, aw awareness.Awareness, mon *Monitor, prov provision.Provision, gw GatewayService) Middleware {
	mcp := NewMCP(reg, gw)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			soajsCtx := GetSOAJSContext(r.Context())
			if soajsCtx == nil || soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil ||
				soajsCtx.Controller.ServiceParams.URLInfo == nil || soajsCtx.Controller.ServiceParams.URLInfo.ServiceName == "" {
				next.ServeHTTP(w, r)
				return
			}
			serviceParams := soajsCtx.Controller.ServiceParams
			serviceName := serviceParams.URLInfo.ServiceName

			// the gateway's routes, on the path segments as Node.js reads them (serviceInfo)
			if segments := strings.Split(serviceParams.URLInfo.OriginalPath, "/"); len(segments) > 2 && segments[1] == "soajs" {
				switch segments[2] {
				case "acl":
					if r.Header.Get("key") == "" {
						WriteCode(w, 132)
						return
					}
					mimicGatewayAPI(serviceParams, gw, "/soajs/acl")
					soajsCtx.Controller.Gotoservice = func(w http.ResponseWriter, r *http.Request) {
						soajsACL(w, soajsCtx)
					}
					next.ServeHTTP(w, r)
					return
				case "mcp":
					if !mcp.route(w, r, soajsCtx) {
						return
					}
					next.ServeHTTP(w, r)
					return
				case "proxy":
					if !proxyRoute(w, r, soajsCtx, reg) {
						return
					}
					if r.Header.Get("soajsroaming") != "" {
						soajsCtx.Controller.Gotoservice = func(w http.ResponseWriter, r *http.Request) {
							roaming(w, r, soajsCtx, reg)
						}
					} else {
						soajsCtx.Controller.Gotoservice = func(w http.ResponseWriter, r *http.Request) {
							soajsProxy(w, r, soajsCtx, reg, prov)
						}
					}
					next.ServeHTTP(w, r)
					return
				}
			}
			if serviceName == "soajs" {
				WriteCode(w, 130)
				return
			}
			info := serviceParams.URLInfo

			regData := reg.Get()
			if regData == nil {
				WriteCode(w, 130)
				return
			}
			svc := regData.Services[serviceName]
			if svc == nil || svc.Port == 0 || (os.Getenv("SOAJS_DEPLOY_HA") == "" && svc.Hosts == nil) {
				soajsCtx.Log.Error("Service [" + serviceName + "] couldn't be matched to a service or the service entry in registry is missing [port || hosts]")
				WriteCode(w, 130)
				return
			}
			version := info.ServiceVersion
			if version == "" {
				version = aw.GetLatestVersion(serviceName)
				if version == "" {
					if v, ok := aw.GetLatestVersionFromCluster(serviceName); ok {
						version = v
					}
				}
				if version == "" {
					WriteCode(w, 130)
					return
				}
				info.ServiceVersion = version
			}
			if v := svc.Versions[version]; v != nil && v.ExtKeyRequired {
				key := r.Header.Get("key")
				if key == "" {
					key = r.URL.Query().Get("key")
				}
				if key == "" {
					WriteCode(w, 132)
					return
				}
				keyConfig := security.KeyConfig{Algorithm: regData.ServiceConfig.Key.Algorithm, Password: regData.ServiceConfig.Key.Password}
				if _, err := security.DecryptKey(key, keyConfig); err != nil {
					soajsCtx.Log.Warn(err.Error())
					WriteCode(w, 132)
					return
				}
			}

			segments := strings.Split(info.OriginalPath, "/")
			passportLogin := len(segments) > 3 && segments[1] == "oauth" && segments[2] == "passport" && segments[3] == "login"
			switch {
			case passportLogin:
				soajsCtx.Controller.Gotoservice = func(w http.ResponseWriter, r *http.Request) {
					redirect(w, r, soajsCtx, reg, aw, nil, true)
				}
			case r.Header.Get("soajsroaming") != "":
				soajsCtx.Controller.Gotoservice = func(w http.ResponseWriter, r *http.Request) {
					roaming(w, r, soajsCtx, reg)
				}
			default:
				soajsCtx.Controller.Gotoservice = func(w http.ResponseWriter, r *http.Request) {
					redirect(w, r, soajsCtx, reg, aw, mon, false)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// InjectObj replaces any soajsinjectobj the request came with: on a key's API it is the one the
// gateway builds (mw/mt/index.js), elsewhere there is none. A header larger than
// SOAJS_MAX_INJECT_HEADER_SIZE answers 135.
func InjectObj(reg registry.Registry, aw awareness.Awareness) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Del("soajsinjectobj")
			soajsCtx := GetSOAJSContext(r.Context())
			if soajsCtx != nil && soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil &&
				soajsCtx.Controller.ServiceParams.KeyAPI && !soajsCtx.Controller.ServiceParams.GatewayAPI &&
				soajsCtx.Key != nil && soajsCtx.Controller.ServiceParams.URLInfo != nil {
				h, ok := BuildInjectObj(soajsCtx, reg, aw, &httpRequestInfo{userAgent: r.Header.Get("User-Agent"), clientIP: getClientIP(r)})
				if !ok {
					WriteCode(w, 135)
					return
				}
				r.Header.Set("soajsinjectobj", h)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// roaming answers a soajsroaming request (mw/gotoService/roaming.js): when the custom registry
// oauth value.roaming.services.<service>.whitelistips holds the client IP, 200 with the
// request's soajsinjectobj as a header and {"result":true,"data":true}; else 165.
func roaming(w http.ResponseWriter, r *http.Request, soajsCtx *SOAJSContext, reg registry.Registry) {
	service := soajsCtx.Controller.ServiceParams.URLInfo.ServiceName
	var custom map[string]interface{}
	if regData := reg.Get(); regData != nil {
		custom = regData.CustomRegistry
	}
	oauth, _ := custom["oauth"].(map[string]interface{})
	value, _ := oauth["value"].(map[string]interface{})
	roamingConfig, _ := value["roaming"].(map[string]interface{})
	services, _ := roamingConfig["services"].(map[string]interface{})
	if config, ok := services[service].(map[string]interface{}); ok {
		ip := getClientIP(r)
		if list, ok := config["whitelistips"].([]interface{}); ok {
			for _, e := range list {
				if e == ip {
					if h := r.Header.Get("soajsinjectobj"); h != "" {
						w.Header().Set("soajsinjectobj", h)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"result":true,"data":true}`))
					return
				}
			}
		}
		soajsCtx.Log.Debug("Detected soajsroaming but the client IP is not in whitelistips", zap.String("service", service))
	} else {
		soajsCtx.Log.Debug("Detected soajsroaming but the service is not under custom registry oauth.roaming.services", zap.String("service", service))
	}
	WriteCode(w, 165)
}

// requestMonitor is the request timeout of redirect (lib/preRedirect.js): after
// requestTimeout*100 ms without traffic on the client connection, the service's heartbeat is
// checked; a healthy service renews the wait, until requestTimeoutRenewal timeouts, then 134; a
// heartbeat that fails answers 133. A streamed response does not count its timeouts. Node.js adds
// a listener on each renewal, so its renewals run out sooner; here each timeout counts once.
type requestMonitor struct {
	mu       sync.Mutex
	w        http.ResponseWriter
	last     time.Time
	ended    bool
	headers  bool
	stream   bool
	renewals int
	cancel   context.CancelFunc
	done     chan struct{}
}

func (m *requestMonitor) touch() {
	m.mu.Lock()
	m.last = time.Now()
	m.mu.Unlock()
}

func (m *requestMonitor) isEnded() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ended
}

func (m *requestMonitor) end() {
	m.mu.Lock()
	m.ended = true
	m.mu.Unlock()
}

// respond answers with a code once (controllerResponse): with its status when nothing was sent
// yet, else as the end of the body already sent.
func (m *requestMonitor) respond(code int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ended {
		return
	}
	m.ended = true
	if !m.headers {
		m.headers = true
		_ = WriteCode(m.w, code)
		return
	}
	noteCode(m.w, code)
	_, _ = m.w.Write(codeBody(code))
}

// codeBody is the body WriteCode answers for a code.
func codeBody(code int) []byte {
	e := soajserr.Get(code)
	if e.Msg == "" {
		status := e.Status
		if status == 0 {
			status = http.StatusInternalServerError
		}
		raw, _ := json.Marshal(struct {
			Code   int `json:"code"`
			Status int `json:"status"`
		}{code, status})
		return raw
	}
	raw, _ := encodeJSON(SOAJSResponse{Result: false, Errors: NewSOAJSError(code, e.Msg)})
	return raw
}

var streamRe = regexp.MustCompile(`(?i)stream`)

// writeHeader sends the service's status and headers as they come.
func (m *requestMonitor) writeHeader(status int, h http.Header) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ended {
		return false
	}
	dst := m.w.Header()
	for k, vv := range h {
		dst[k] = append([]string(nil), vv...)
	}
	m.stream = streamRe.MatchString(h.Get("Content-Type"))
	m.headers = true
	m.w.WriteHeader(status)
	m.last = time.Now()
	return true
}

// Write passes the service's body on as it comes, flushed.
func (m *requestMonitor) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ended {
		return 0, io.ErrClosedPipe
	}
	n, err := m.w.Write(p)
	if f, ok := m.w.(http.Flusher); ok {
		f.Flush()
	}
	m.last = time.Now()
	return n, err
}

type monitoredBody struct {
	io.ReadCloser
	m   *requestMonitor
	rec *monitorRecord
	req *http.Request
}

func (b monitoredBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.m.touch()
		if b.rec != nil {
			b.rec.captureBody(b.req, p[:n])
		}
	}
	if err == io.EOF && b.rec != nil {
		b.rec.bodyEnd()
	}
	return n, err
}

func (m *requestMonitor) run(log *zap.Logger, idle time.Duration, renewals int, heartbeat func() bool) {
	for {
		m.mu.Lock()
		wait := idle - time.Since(m.last)
		m.mu.Unlock()
		if wait > 0 {
			select {
			case <-m.done:
				return
			case <-time.After(wait):
			}
			continue
		}
		if m.isEnded() {
			return
		}
		m.mu.Lock()
		m.renewals++
		count := m.renewals
		m.mu.Unlock()
		log.Warn("Request is taking too much time ...")
		// answer before cancelling the call to the service: the cancelled call returns at once,
		// and its error path (135) must find the request ended
		if count >= renewals {
			log.Error("Request time exceeded the requestTimeoutRenewal")
			m.respond(134)
			m.cancel()
			return
		}
		if !heartbeat() {
			log.Error("Service heartbeat is not responding")
			m.respond(133)
			m.cancel()
			return
		}
		m.mu.Lock()
		if m.stream {
			m.renewals--
		}
		m.last = time.Now()
		m.mu.Unlock()
	}
}

// redirect calls the service (lib/preRedirect.js, redirectToService.js; simpleRTS.js when
// simple): a healthy host from awareness (else 133); the request as received, with the raw path
// and query after the service and the request's headers, the gateway's soajsinjectobj among them,
// and the soajsauth session cookie when the gateway authorizes; the service's status, headers and
// body as they come. A call that fails answers 135. simpleRTS sets Host to the service's host.
func redirect(w http.ResponseWriter, r *http.Request, soajsCtx *SOAJSContext, reg registry.Registry, aw awareness.Awareness, mon *Monitor, simple bool) {
	regData := reg.Get()
	params := soajsCtx.Controller.ServiceParams
	name, version := params.URLInfo.ServiceName, params.URLInfo.ServiceVersion
	gw := regData.Services["controller"]
	if gw == nil {
		WriteCode(w, 131)
		return
	}
	svc := regData.Services[name]
	hostInfo, err := aw.GetHost(name, version)
	if err != nil {
		soajsCtx.Log.Error("Unable to find any healthy host for service [" + name + "@" + version + "]")
		WriteCode(w, 133)
		return
	}
	host := hostInfo.Host

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	m := &requestMonitor{w: w, cancel: cancel, done: make(chan struct{}), last: time.Now()}
	defer close(m.done)
	if !renewReqMonitorOff(regData) {
		requestTO := svc.RequestTimeout
		if requestTO == 0 {
			requestTO = gw.RequestTimeout
		}
		requestTOR := svc.RequestTimeoutRenewal
		if requestTOR == 0 {
			requestTOR = gw.RequestTimeoutRenewal
		}
		go m.run(soajsCtx.Log, time.Duration(requestTO)*100*time.Millisecond, requestTOR, func() bool {
			return serviceHeartbeat(regData, svc, host)
		})
	}

	// the soamonitor record of a redirect (not simpleRTS)
	var rec *monitorRecord
	if !simple && mon != nil {
		rec = newMonitorRecord(regData, r, params, host)
	}
	send := func() {
		if rec.wantsResponse() {
			mon.Send(rec, r.Header.Get("soajsinjectobj"))
		}
	}

	target := "http://" + net.JoinHostPort(host, strconv.Itoa(svc.Port)) + params.URLInfo.URL
	var body io.Reader = http.NoBody
	if r.Body != nil && r.Body != http.NoBody {
		mb := monitoredBody{ReadCloser: r.Body, m: m}
		if rec.wantsBody(r.Method) {
			mb.rec, mb.req = rec, r
		}
		body = mb
	}
	out, err := http.NewRequestWithContext(ctx, r.Method, target, body)
	if err != nil {
		soajsCtx.Log.Error(err.Error())
		m.respond(135)
		return
	}
	out.ContentLength = r.ContentLength
	out.Header = r.Header.Clone()
	out.Host = r.Host
	if simple {
		out.Host = host
	}
	if gw.Authorization != nil && *gw.Authorization {
		authorize(r, out, regData)
	}

	// from here the service's answer completes or releases the Idempotency-Key and fills the
	// cache (redirect only)
	var idem *IdempotencyContext
	var cache *CacheContext
	if !simple {
		idem, cache = soajsCtx.Idempotency, soajsCtx.Cache
		if idem != nil {
			idem.proxied = true
		}
	}
	resp, err := proxyTransport.RoundTrip(out)
	if err != nil {
		if !m.isEnded() {
			soajsCtx.Log.Error(err.Error() + " with [" + name + "@" + version + "]")
		}
		m.respond(135)
		idem.release()
		send()
		return
	}
	defer resp.Body.Close()
	if !m.writeHeader(resp.StatusCode, resp.Header) {
		idem.release()
		return
	}
	var answer *RecordedResponse
	if idem != nil || cache != nil {
		answer = &RecordedResponse{StatusCode: resp.StatusCode, Headers: recordedHeaders(resp.Header)}
	}
	// recorded keeps the service's full answer: completes the Idempotency-Key, fills the cache
	recorded := func(body []byte) {
		if answer == nil {
			return
		}
		answer.Body = utf8Body(body)
		idem.finish(answer)
		cache.save(answer)
	}
	if rec.wantsResponse() {
		rec.responseStart(resp.Header.Get("Content-Type"))
	}
	if r.Method == http.MethodHead {
		m.end()
		recorded(nil)
		send()
		return
	}
	var dst io.Writer = m
	if rec.wantsResponse() {
		dst = capturingWriter{m, rec}
	}
	var rw *recordingWriter
	if answer != nil {
		rw = &recordingWriter{w: dst, m: m}
		dst = rw
	}
	_, err = io.Copy(dst, resp.Body)
	switch {
	case err != nil && !m.isEnded():
		soajsCtx.Log.Error(err.Error() + " with [" + name + "@" + version + "]")
		m.respond(135)
		idem.release()
	case err != nil:
		// the request was ended (request timeout): the service did not answer in full
		idem.release()
		m.end()
	case rw != nil && rw.err != nil:
		// the client is gone; the service answered in full, its answer is recorded
		soajsCtx.Log.Error(rw.err.Error() + " with [" + name + "@" + version + "]")
		m.respond(135)
		recorded(rw.body)
	default:
		m.end()
		if rw != nil {
			recorded(rw.body)
		}
	}
	send()
}

// recordingWriter records the service's answer for the Idempotency-Key and the cache. When the
// client can no longer be written to, it keeps reading the answer to record it, unless the request
// was ended.
type recordingWriter struct {
	w    io.Writer
	m    *requestMonitor
	body []byte
	err  error
}

func (i *recordingWriter) Write(p []byte) (int, error) {
	i.body = append(i.body, p...)
	if i.err != nil {
		return len(p), nil
	}
	if _, err := i.w.Write(p); err != nil {
		if i.m.isEnded() {
			return 0, err
		}
		i.err = err
	}
	return len(p), nil
}

// capturingWriter passes the answer on and records it for the monitor.
type capturingWriter struct {
	m   *requestMonitor
	rec *monitorRecord
}

func (c capturingWriter) Write(p []byte) (int, error) {
	n, err := c.m.Write(p)
	if n > 0 {
		c.rec.captureResponse(p[:n])
	}
	return n, err
}

func renewReqMonitorOff(reg *types.RegistryData) bool {
	gateway, _ := reg.CustomRegistry["gateway"].(map[string]interface{})
	value, _ := gateway["value"].(map[string]interface{})
	gotoService, _ := value["gotoService"].(map[string]interface{})
	return truthy(gotoService["renewReqMonitorOff"])
}

// serviceHeartbeat checks the service on its maintenance readiness when it declares one, else on
// /heartbeat at the maintenance port: healthy on a 200 within 5 seconds.
func serviceHeartbeat(reg *types.RegistryData, svc *types.ServiceConfig, host string) bool {
	inc := 0
	if reg.ServiceConfig != nil {
		inc = reg.ServiceConfig.Ports.MaintenanceInc
	}
	port, path := svc.Port+inc, "/heartbeat"
	if mt := svc.Maintenance; mt != nil && mt.Readiness != "" && mt.Port.Type != "" {
		path = mt.Readiness
		if mt.Port.Type != "maintenance" {
			port = svc.Port
		}
	}
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, strconv.Itoa(port)) + path)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// authorize is lib/isRequestAuthorized.js: the cookie header is always sent, and a soajsauth
// header, else query parameter, becomes the session cookie.
func authorize(r, out *http.Request, reg *types.RegistryData) {
	cookie := out.Header.Get("Cookie")
	out.Header.Set("Cookie", cookie)
	auth := r.Header.Get("soajsauth")
	if auth == "" {
		auth = r.URL.Query().Get("soajsauth")
	}
	if auth == "" || reg.ServiceConfig == nil {
		return
	}
	session := reg.ServiceConfig.Session
	if c := sessionCookie(auth, session.Secret, session.Name); c != "" {
		out.Header.Set("Cookie", strings.Join(append(strings.Split(cookie, ";"), c), ";"))
	}
}

// sessionCookie is soajs.core security authorization.setCookie: soajsauth is
// "Basic base64(soajs:<masked session id>)", the unmasked id signed as express-session does.
func sessionCookie(auth, secret, name string) string {
	decoded, err := url.PathUnescape(auth)
	if err != nil {
		return ""
	}
	parts := strings.Split(decoded, "Basic ")
	if len(parts) < 2 || parts[1] == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		if raw, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(parts[1], "=")); err != nil {
			return ""
		}
	}
	ascii := string(raw)
	if !strings.HasPrefix(ascii, "soajs:") {
		return ""
	}
	id, ok := umask(strings.Split(ascii, "soajs:")[1])
	if !ok || id == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	return name + "=s:" + id + "." + strings.TrimRight(base64.StdEncoding.EncodeToString(mac.Sum(nil)), "=")
}

// umask is authorization.umask: a known cypher style decyphers a 32-character id, an unknown style
// is only dropped from the id.
func umask(masked string) (string, bool) {
	if len(masked) < 3 {
		return "", false
	}
	style, sid := masked[:3], masked[3:]
	swaps := map[string][4]int{"C01": {3, 11, 7, 15}, "C02": {4, 12, 9, 15}, "C03": {5, 13, 11, 15}}
	p, ok := swaps[style]
	if !ok {
		return sid, true
	}
	if len(sid) != 32 {
		return "", false
	}
	b := []byte(sid)
	b[p[0]], b[p[1]] = sid[p[1]], sid[p[0]]
	b[p[2]], b[p[3]] = sid[p[3]], sid[p[2]]
	return string(b), true
}
