package mav

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// macProxyStateFile guarda el estado previo del proxy dentro del run.
//
// Va a disco y no en memoria porque `network start` y `network stop` son dos
// invocaciones distintas: sin esto, parar la captura no sabria a que devolver
// el sistema y lo dejaria apuntando a un proxy muerto -- que es peor que no
// haber capturado, porque rompe la red de la maquina entera hasta que alguien
// se da cuenta.
const macProxyStateFile = "network-proxy.json"

type macProxyState struct {
	Service string `json:"service"`
	Enabled bool   `json:"enabled"`
	Server  string `json:"server"`
	Port    string `json:"port"`
}

// macNetworkService averigua por que servicio sale el trafico.
//
// No vale con coger el primero de `-listallnetworkservices`: esa lista incluye
// los interfaces virtuales que dejan las VMs y los tuneles, y fijar el proxy en
// el equivocado no da error -- simplemente no captura nada. Se parte de la ruta
// por defecto, que es la unica fuente que dice por donde sale el trafico de
// verdad.
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
		// El nombre del servicio esta en la linea ANTERIOR, con la forma
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

// readMacProxy lee el estado actual para poder devolverlo tal cual.
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

// pointMacAtProxy apunta el sistema al proxy y deja escrito a que devolverlo.
//
// Se fijan los dos, HTTP y HTTPS: una app que solo hable https -- que son casi
// todas -- ignoraria el primero, y el sintoma seria una captura vacia sin un
// solo error.
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

// restoreMacProxy devuelve el sistema a como estaba.
//
// Se llama tambien desde `mav stop`, no solo desde `network stop`: si el run
// muere por el camino, dejar la maquina apuntando a un proxy que ya no existe
// la deja sin red. Es idempotente a proposito -- restaurar dos veces tiene que
// ser inofensivo.
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

// macProxyCATrusted dice si el CA de mitmproxy esta en el llavero del sistema.
//
// Sin el, mitmdump sigue grabando pero el trafico https sale como tuneles
// CONNECT sin contenido: una captura que parece funcionar y no sirve de nada.
func (c CLI) macProxyCATrusted(ctx context.Context) bool {
	res := c.Runner.Run(ctx, "security", "find-certificate", "-c", "mitmproxy", "-a", "/Library/Keychains/System.keychain")
	return strings.Contains(res.Stdout, "mitmproxy")
}
