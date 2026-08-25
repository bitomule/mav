package mav

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// macProxyStateFile stores the previous proxy state inside the run.
//
// It goes to disk and not memory because `network start` and `network stop`
// are two separate invocations: without this, stopping the capture would
// not know what to restore the system to and would leave it pointing at a
// dead proxy, which is worse than not having captured, because it breaks
// the whole machine's network until somebody notices.
const macProxyStateFile = "network-proxy.json"

type macProxyState struct {
	Service string `json:"service"`
	Enabled bool   `json:"enabled"`
	Server  string `json:"server"`
	Port    string `json:"port"`
}

// macNetworkService finds out which service the traffic leaves through.
//
// Taking the first entry of `-listallnetworkservices` is not enough: that
// list includes the virtual interfaces left behind by VMs and tunnels, and
// setting the proxy on the wrong one gives no error, it simply captures
// nothing. The starting point is the default route, the only source that
// says where the traffic really goes out.
func (c CLI) macNetworkService(ctx context.Context) string {
	route := c.Runner.Run(ctx, "route", "-n", "get", "default")
	device := ""
	for _, line := range strings.Split(route.Stdout, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "interface:" {
			device = fields[1]
		}
	}
	if device == "" {
		return ""
	}
	order := c.Runner.Run(ctx, "networksetup", "-listnetworkserviceorder")
	lines := strings.Split(order.Stdout, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "Device: "+device+")") {
			continue
		}
		// The service name is on the PREVIOUS line, in the form
		// "(4) Ethernet".
		if i == 0 {
			return ""
		}
		name := lines[i-1]
		if idx := strings.Index(name, ") "); idx >= 0 {
			return strings.TrimSpace(name[idx+2:])
		}
	}
	return ""
}

// readMacProxy reads the current state so it can be restored as is.
func (c CLI) readMacProxy(ctx context.Context, service string) macProxyState {
	state := macProxyState{Service: service}
	res := c.Runner.Run(ctx, "networksetup", "-getwebproxy", service)
	for _, line := range strings.Split(res.Stdout, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Enabled":
			state.Enabled = strings.EqualFold(value, "yes")
		case "Server":
			state.Server = value
		case "Port":
			state.Port = value
		}
	}
	return state
}

// pointMacAtProxy points the system at the proxy and records what to
// restore it to.
//
// Both HTTP and HTTPS are set: an app that only speaks https, which is
// almost all of them, would ignore the former, and the symptom would be an
// empty capture without a single error.
func (c CLI) pointMacAtProxy(ctx context.Context, run RunState, port int) (macProxyState, bool) {
	service := c.macNetworkService(ctx)
	if service == "" {
		return macProxyState{}, false
	}
	previous := c.readMacProxy(ctx, service)
	data, err := json.Marshal(previous)
	if err != nil {
		return macProxyState{}, false
	}
	if err := os.WriteFile(filepath.Join(run.Dir, macProxyStateFile), data, 0o644); err != nil {
		return macProxyState{}, false
	}
	host := "127.0.0.1"
	value := strconv.Itoa(port)
	if res := c.Runner.Run(ctx, "networksetup", "-setwebproxy", service, host, value); res.Err != nil {
		return macProxyState{}, false
	}
	if res := c.Runner.Run(ctx, "networksetup", "-setsecurewebproxy", service, host, value); res.Err != nil {
		return macProxyState{}, false
	}
	return previous, true
}

// restoreMacProxy returns the system to how it was.
//
// It is also called from `mav stop`, not only from `network stop`: if the
// run dies along the way, leaving the machine pointing at a proxy that no
// longer exists leaves it without network. It is idempotent on purpose,
// restoring twice has to be harmless.
func (c CLI) restoreMacProxy(ctx context.Context, run RunState) bool {
	path := filepath.Join(run.Dir, macProxyStateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var previous macProxyState
	if err := json.Unmarshal(data, &previous); err != nil || previous.Service == "" {
		return false
	}
	defer os.Remove(path)
	if previous.Enabled && previous.Server != "" {
		c.Runner.Run(ctx, "networksetup", "-setwebproxy", previous.Service, previous.Server, previous.Port)
		c.Runner.Run(ctx, "networksetup", "-setsecurewebproxy", previous.Service, previous.Server, previous.Port)
		return true
	}
	c.Runner.Run(ctx, "networksetup", "-setwebproxystate", previous.Service, "off")
	c.Runner.Run(ctx, "networksetup", "-setsecurewebproxystate", previous.Service, "off")
	return true
}

// macProxyCATrusted says whether the mitmproxy CA is in the system keychain.
//
// Without it, mitmdump keeps recording but the https traffic comes out as
// CONNECT tunnels with no content: a capture that looks like it works and
// is useless.
func (c CLI) macProxyCATrusted(ctx context.Context) bool {
	res := c.Runner.Run(ctx, "security", "find-certificate", "-c", "mitmproxy", "-a", "/Library/Keychains/System.keychain")
	return strings.Contains(res.Stdout, "mitmproxy")
}
