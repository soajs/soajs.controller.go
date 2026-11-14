// Package uaparser parses a User-Agent as the Node.js gateway does for the device check
// (mw/mt/utils.js parseUserAgent, 4.3.24+): ua-parser-js 1.0.41, then Node.js's mapping of its
// names back to the useragent package names the device rules were written against.
//
// ua-parser-js is run as is, embedded, in a JavaScript VM (goja): 32 of its regular expressions use
// lookaround, which Go's regexp (RE2) cannot compile, so a table port would not parse the same.
package uaparser

import (
	"container/list"
	_ "embed"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// MaxLength is the User-Agent length parsed, in UTF-16 code units, as Node.js caps it.
const MaxLength = 512

// Timeout bounds one parse; a parse that runs longer is an error.
const Timeout = 50 * time.Millisecond

//go:embed ua-parser.js
var source string

// ErrTimeout is returned when a parse runs longer than Timeout.
var ErrTimeout = errors.New("user-agent parse timed out")

// UA is a parsed User-Agent: {family, major, minor, patch, os: {family, major, minor, patch}}.
type UA struct {
	Family string `json:"family"`
	Major  string `json:"major"`
	Minor  string `json:"minor"`
	Patch  string `json:"patch"`
	OS     OS     `json:"os"`
}

// OS is the operating system of a parsed User-Agent.
type OS struct {
	Family string `json:"family"`
	Major  string `json:"major"`
	Minor  string `json:"minor"`
	Patch  string `json:"patch"`
}

// warmUp are parsed once by each new VM.
var warmUp = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Linux; Android 13; SM-S911B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
	"curl/8.4.0",
}

const parseFn = `(function (ua) {
	var r = new module.exports(ua).getResult();
	return [r.browser.name || "", r.browser.version || "", r.os.name || "", r.os.version || ""];
})`

type runtime struct {
	vm    *goja.Runtime
	parse goja.Callable
}

func newRuntime() (*runtime, error) {
	vm := goja.New()
	if _, err := vm.RunString("var module = {exports: {}}; var exports = module.exports;"); err != nil {
		return nil, err
	}
	if _, err := vm.RunString(source); err != nil {
		return nil, err
	}
	v, err := vm.RunString(parseFn)
	if err != nil {
		return nil, err
	}
	fn, ok := goja.AssertFunction(v)
	if !ok {
		return nil, errors.New("ua-parser-js did not load")
	}
	// The first parse compiles the parser's regular expressions; do it here, outside the timeout
	for _, ua := range warmUp {
		if _, err := fn(goja.Undefined(), vm.ToValue(ua)); err != nil {
			return nil, err
		}
	}
	return &runtime{vm: vm, parse: fn}, nil
}

// Parser parses User-Agents with a bounded pool of VMs and a bounded cache of results.
type Parser struct {
	pool    chan *runtime
	size    int
	timeout time.Duration
	made    int
	mu      sync.Mutex

	cacheMu  sync.Mutex
	cache    map[string]*list.Element
	order    *list.List
	capacity int
}

type entry struct {
	key string
	ua  UA
}

// New creates a parser with up to vms VMs, each with the parser loaded once, and a cache of up to
// cacheSize results.
func New(vms, cacheSize int) *Parser {
	return &Parser{
		pool:     make(chan *runtime, vms),
		size:     vms,
		timeout:  Timeout,
		cache:    map[string]*list.Element{},
		order:    list.New(),
		capacity: cacheSize,
	}
}

// Parse parses a User-Agent, capped to MaxLength first.
func (p *Parser) Parse(ua string) (UA, error) {
	ua = capUTF16(ua, MaxLength)
	if cached, ok := p.cached(ua); ok {
		return cached, nil
	}
	rt, err := p.get()
	if err != nil {
		return UA{}, err
	}
	timer := time.AfterFunc(p.timeout, func() { rt.vm.Interrupt(ErrTimeout) })
	v, err := rt.parse(goja.Undefined(), rt.vm.ToValue(ua))
	timer.Stop()
	rt.vm.ClearInterrupt()
	p.put(rt)
	if err != nil {
		var interrupted *goja.InterruptedError
		if errors.As(err, &interrupted) {
			return UA{}, ErrTimeout
		}
		return UA{}, err
	}
	var parts []string
	if err := rt.vm.ExportTo(v, &parts); err != nil || len(parts) != 4 {
		return UA{}, errors.New("unexpected ua-parser-js result")
	}
	result := mapResult(ua, parts[0], parts[1], parts[2], parts[3])
	p.store(ua, result)
	return result, nil
}

func (p *Parser) get() (*runtime, error) {
	select {
	case rt := <-p.pool:
		return rt, nil
	default:
	}
	p.mu.Lock()
	if p.made < p.size {
		p.made++
		p.mu.Unlock()
		rt, err := newRuntime()
		if err != nil {
			p.mu.Lock()
			p.made--
			p.mu.Unlock()
			return nil, err
		}
		return rt, nil
	}
	p.mu.Unlock()
	return <-p.pool, nil
}

func (p *Parser) put(rt *runtime) {
	p.pool <- rt
}

func (p *Parser) cached(key string) (UA, bool) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	if el, ok := p.cache[key]; ok {
		p.order.MoveToFront(el)
		return el.Value.(*entry).ua, true
	}
	return UA{}, false
}

func (p *Parser) store(key string, ua UA) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	if el, ok := p.cache[key]; ok {
		p.order.MoveToFront(el)
		return
	}
	p.cache[key] = p.order.PushFront(&entry{key: key, ua: ua})
	for p.order.Len() > p.capacity {
		last := p.order.Back()
		p.order.Remove(last)
		delete(p.cache, last.Value.(*entry).key)
	}
}

// capUTF16 keeps the first n UTF-16 code units, as String.prototype.substring counts them.
func capUTF16(s string, n int) string {
	units := 0
	for i, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		if units+w > n {
			return s[:i]
		}
		units += w
	}
	return s
}

// Node.js names: the device rules were written against the useragent package names.
var (
	osFamilies      = map[string]string{"Mac OS": "Mac OS X", "Chromium OS": "Chrome OS"}
	androidFamilies = map[string]string{"Firefox": "Firefox Mobile", "Opera": "Opera Mobile", "Chrome WebView": "Chrome Mobile WebView"}
	iosFamilies     = map[string]string{"Chrome": "Chrome Mobile iOS", "Firefox": "Firefox iOS", "WebKit": "Mobile Safari UI/WKWebView"}
	families        = map[string]string{"Yandex": "Yandex Browser"}

	mozillaRe     = regexp.MustCompile(`^Mozilla/`)
	clientRe      = regexp.MustCompile(`^([^/\s]+)/([\d.]+)`)
	edgChromiumRe = regexp.MustCompile(` Edg(A)?/`)
	chromeRe      = regexp.MustCompile(`Chrome/([\d.]+)`)
	edgiOSRe      = regexp.MustCompile(` EdgiOS/`)
	versionRe     = regexp.MustCompile(`Version/([\d.]+)`)
	mobileRe      = regexp.MustCompile(` Mobile `)
)

func mapName(m map[string]string, name string) string {
	if v, ok := m[name]; ok {
		return v
	}
	return name
}

func splitVersion(version string) (string, string, string) {
	parts := strings.Split(version, ".")
	get := func(i int) string {
		if i < len(parts) && parts[i] != "" {
			return parts[i]
		}
		return "0"
	}
	return get(0), get(1), get(2)
}

// mapResult is the Node.js parseUserAgent after the ua-parser-js result.
func mapResult(ua, family, version, osFamily, osVersion string) UA {
	if family == "" {
		// ua-parser-js guesses iOS from Darwin/CFNetwork without a version
		if osFamily == "iOS" && osVersion == "" {
			osFamily = ""
		}
		// non browser clients: curl/8.4.0, MyApp/142 CFNetwork/...
		family, version = "Other", ""
		if !mozillaRe.MatchString(ua) {
			if m := clientRe.FindStringSubmatch(ua); m != nil {
				family, version = m[1], m[2]
			}
		}
	}
	if family == "Edge" {
		if edgChromiumRe.MatchString(ua) {
			family, version = "Chrome", ""
			if m := chromeRe.FindStringSubmatch(ua); m != nil {
				version = m[1]
			}
		} else if edgiOSRe.MatchString(ua) {
			family, version = "Mobile Safari", ""
			if m := versionRe.FindStringSubmatch(ua); m != nil {
				version = m[1]
			}
		}
	}
	if osFamily == "Android" {
		// useragent reported every Android version as 0.0.0
		osVersion = ""
		if family == "Chrome" && mobileRe.MatchString(ua) {
			family = "Chrome Mobile"
		} else {
			family = mapName(androidFamilies, family)
		}
	} else if osFamily == "iOS" {
		if family == "WebKit" {
			version = osVersion
		}
		family = mapName(iosFamilies, family)
	}
	family = mapName(families, family)

	out := UA{Family: family}
	out.Major, out.Minor, out.Patch = splitVersion(version)
	if osFamily == "Windows" {
		// Windows versions are names: 10, 8.1, XP, Vista
		major := osVersion
		if major == "" {
			major = "0"
		}
		out.OS = OS{Family: osFamily, Major: major, Minor: "0", Patch: "0"}
		return out
	}
	out.OS.Major, out.OS.Minor, out.OS.Patch = splitVersion(osVersion)
	out.OS.Family = "Other"
	if osFamily != "" {
		out.OS.Family = mapName(osFamilies, osFamily)
	}
	return out
}
