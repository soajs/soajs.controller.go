package awareness

import (
	"encoding/json"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// RegistryWithStats is the registry's services and daemons as plain JSON values, each with the
// awarenessStats of its hosts: what Node.js holds on the registry and reports on /awarenessStat
// and in the controllers document.
func RegistryWithStats(reg *types.RegistryData, stats map[string]map[string]map[string]*HostStat) (services, daemons map[string]interface{}) {
	services = plainWithStats(reg.Services, stats["services"])
	daemons = plainWithStats(reg.Daemons, stats["daemons"])
	return services, daemons
}

func plainWithStats(entries interface{}, stats map[string]map[string]*HostStat) map[string]interface{} {
	out := map[string]interface{}{}
	raw, err := json.Marshal(entries)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	for name, hosts := range stats {
		entry, ok := out[name].(map[string]interface{})
		if !ok {
			continue
		}
		raw, _ := json.Marshal(hosts)
		var plain map[string]interface{}
		_ = json.Unmarshal(raw, &plain)
		entry["awarenessStats"] = plain
	}
	return out
}

// ControllerData is the data of the gateway's controllers document (server/controller.js
// getAwarenessInfo, modules/registry/mongo.js addUpdateEnvControllers): the registry's services
// and daemons with their awarenessStats, host IPs written with "_dot_" for ".", and versions
// sanitized, as mongo keys cannot hold dots. On termination only the gateway's entry is kept,
// with this gateway's host marked unhealthy, and daemons are empty (Node.js drops the field,
// which the controllers schema requires).
func ControllerData(reg *types.RegistryData, stats map[string]map[string]map[string]*HostStat, gatewayName, ip string, terminate bool, now time.Time) map[string]interface{} {
	services, daemons := RegistryWithStats(reg, stats)
	if terminate {
		for name, s := range services {
			if name != gatewayName {
				delete(services, name)
				continue
			}
			entry, _ := s.(map[string]interface{})
			hosts, _ := entry["awarenessStats"].(map[string]interface{})
			if stat, ok := hosts[ip].(map[string]interface{}); ok {
				stat["healthy"] = false
				stat["lastCheck"] = now.UnixMilli()
			}
		}
		daemons = map[string]interface{}{}
	}
	for _, group := range []map[string]interface{}{services, daemons} {
		for _, s := range group {
			entry, _ := s.(map[string]interface{})
			renameKeys(entry["awarenessStats"], func(k string) string { return strings.ReplaceAll(k, ".", "_dot_") })
			renameKeys(entry["hosts"], sanitizeVersion)
			renameKeys(entry["versions"], sanitizeVersion)
		}
	}
	return map[string]interface{}{"services": services, "daemons": daemons}
}

func renameKeys(v interface{}, rename func(string) string) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if n := rename(k); n != k {
			m[n] = m[k]
			delete(m, k)
		}
	}
}

// ControllerIP is the IP the controllers document is kept under: the gateway's IP with
// SOAJS_MANUAL, 127.0.0.1 otherwise, as Node.js does.
func ControllerIP(serviceIP string) string {
	if os.Getenv("SOAJS_MANUAL") == "" {
		return "127.0.0.1"
	}
	return serviceIP
}

// ServiceIP is the IP the gateway registers (lib/ip.js): SOAJS_SRVIP; 127.0.0.1 when
// SOAJS_SRV_AUTOREGISTERHOST is set to anything but "true"; else the IPv4 of eth0, en0, eth1,
// en1, then of any other interface, else 127.0.0.1. Empty with SOAJS_DEPLOY_HA and no
// SOAJS_SRVIP: the gateway does not register in HA mode.
func ServiceIP() string {
	ip := os.Getenv("SOAJS_SRVIP")
	ha := os.Getenv("SOAJS_DEPLOY_HA") != ""
	if v, ok := os.LookupEnv("SOAJS_SRV_AUTOREGISTERHOST"); ok && v != "true" && !ha {
		ip = "127.0.0.1"
	}
	if ip != "" || ha {
		return ip
	}
	return hostIP()
}

func hostIP() string {
	ips := map[string]string{}
	var others []string
	preferred := []string{"eth0", "en0", "eth1", "en1"}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			if _, seen := ips[iface.Name]; !seen && !containsString(preferred, iface.Name) {
				others = append(others, iface.Name)
			}
			ips[iface.Name] = ipnet.IP.To4().String()
		}
	}
	for _, name := range append(preferred, others...) {
		if ip := ips[name]; ip != "" {
			return ip
		}
	}
	return "127.0.0.1"
}

// Register records the gateway's host and, after healthCheckInterval, its controllers document
// (server/controller.js start), without SOAJS_DEPLOY_HA. Node.js registers whatever
// autoRegisterService says.
// It reports whether the host was registered.
func Register(reg registry.Registry, aw Awareness, gw Gateway, port int, ip string, logger *zap.Logger) bool {
	if os.Getenv("SOAJS_DEPLOY_HA") != "" || ip == "" {
		return false
	}
	hostname, _ := os.Hostname()
	err := reg.RegisterHost(types.HostRecord{
		Env: reg.GetEnvironment(), Name: gw.Name, Version: gw.Version, IP: ip, Port: port, Hostname: strings.ToLower(hostname),
	})
	if err != nil {
		logger.Error("Unable to register host IP [" + ip + "] for service [" + gw.Name + "@" + gw.Version + "]")
		return false
	}
	logger.Info("Host IP [" + ip + "] for service [" + gw.Name + "@" + gw.Version + "] successfully registered.")
	interval := 0
	if r := reg.Get(); r != nil && r.ServiceConfig != nil {
		interval = r.ServiceConfig.Awareness.HealthCheckInterval
	}
	time.AfterFunc(time.Duration(interval)*time.Millisecond, func() {
		if err := WriteControllerDoc(reg, aw, gw, ip, false); err != nil {
			logger.Error(err.Error())
		}
	})
	return true
}

// WriteControllerDoc writes the gateway's controllers document; terminate marks it stopping.
func WriteControllerDoc(reg registry.Registry, aw Awareness, gw Gateway, ip string, terminate bool) error {
	r := reg.Get()
	if r == nil {
		return nil
	}
	now := time.Now()
	data := ControllerData(r, aw.Stats(), gw.Name, ip, terminate, now)
	return reg.UpsertEnvController(reg.GetEnvironment(), ControllerIP(ip), now.UnixMilli(), data)
}
