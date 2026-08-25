package mav

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const routeOutput = "   route to: default\ndestination: default\n  interface: en0\n"
const serviceOrder = "An asterisk (*) denotes that a network service is disabled.\n(1) Tunel\n(Hardware Port: tun, Device: utun0)\n\n(4) Ethernet\n(Hardware Port: Ethernet, Device: en0)\n"

// TestMacNetworkServiceFollowsTheDefaultRoute: coger el primer servicio de la
// lista parece equivalente y no lo es -- ahi salen los interfaces virtuales que
// dejan las VMs y los tuneles. Fijar el proxy en el equivocado NO da error:
// simplemente no captura nada, que es el fallo mas caro de diagnosticar.
func TestMacNetworkServiceFollowsTheDefaultRoute(t *testing.T) {
	runner := &scriptedRunner{outputs: map[string]string{
		"route -n get default":                  routeOutput,
		"networksetup -listnetworkserviceorder": serviceOrder,
	}}
	cli := CLI{Runner: runner, Root: t.TempDir()}
	if got := cli.macNetworkService(context.Background()); got != "Ethernet" {
		t.Fatalf("debe seguir la ruta por defecto, no el orden de la lista: %q", got)
	}
}

// TestMacProxyRestoresWhatItFound fija la propiedad que hace esto seguro:
// parar la captura devuelve la maquina a como estaba. Sin ello, un run que
// muere deja el sistema apuntando a un proxy que ya no existe -- o sea, sin red
// y sin ninguna pista de por que.
func TestMacProxyRestoresWhatItFound(t *testing.T) {
	root := t.TempDir()
	run := RunState{ID: "r1", Dir: root}
	runner := &scriptedRunner{outputs: map[string]string{
		"route -n get default":                  routeOutput,
		"networksetup -listnetworkserviceorder": serviceOrder,
		"networksetup -getwebproxy Ethernet":    "Enabled: Yes\nServer: 10.0.0.1\nPort: 3128\n",
	}}
	cli := CLI{Runner: runner, Root: root}
	if _, ok := cli.pointMacAtProxy(context.Background(), run, 8080); !ok {
		t.Fatal("deberia haber fijado el proxy")
	}
	if !strings.Contains(strings.Join(runner.commands, "|"), "-setsecurewebproxy Ethernet 127.0.0.1 8080") {
		t.Fatalf("el https tambien: una app que solo hable https ignoraria el otro: %v", runner.commands)
	}
	if _, err := os.Stat(filepath.Join(root, macProxyStateFile)); err != nil {
		t.Fatal("el estado previo tiene que ir a disco: start y stop son procesos distintos")
	}
	runner.commands = nil
	if !cli.restoreMacProxy(context.Background(), run) {
		t.Fatal("deberia restaurar")
	}
	joined := strings.Join(runner.commands, "|")
	if !strings.Contains(joined, "-setwebproxy Ethernet 10.0.0.1 3128") {
		t.Fatalf("habia un proxy antes y hay que devolverlo, no apagarlo: %v", runner.commands)
	}
	if cli.restoreMacProxy(context.Background(), run) {
		t.Fatal("restaurar dos veces tiene que ser inofensivo")
	}
}

// TestMacProxyTurnsOffWhenThereWasNoProxy: si no habia proxy, restaurar es
// apagarlo. Dejarlo puesto con el puerto de una captura muerta seria peor que
// no haber capturado.
func TestMacProxyTurnsOffWhenThereWasNoProxy(t *testing.T) {
	root := t.TempDir()
	run := RunState{ID: "r1", Dir: root}
	runner := &scriptedRunner{outputs: map[string]string{
		"route -n get default":                  routeOutput,
		"networksetup -listnetworkserviceorder": serviceOrder,
		"networksetup -getwebproxy Ethernet":    "Enabled: No\nServer: \nPort: 0\n",
	}}
	cli := CLI{Runner: runner, Root: root}
	if _, ok := cli.pointMacAtProxy(context.Background(), run, 8080); !ok {
		t.Fatal("deberia haber fijado el proxy")
	}
	runner.commands = nil
	cli.restoreMacProxy(context.Background(), run)
	if !strings.Contains(strings.Join(runner.commands, "|"), "-setwebproxystate Ethernet off") {
		t.Fatalf("%v", runner.commands)
	}
}

// scriptedRunner responde por subcadena y apunta lo ejecutado, que es lo que
// hace falta para probar una secuencia de comandos del sistema.
type scriptedRunner struct {
	outputs  map[string]string
	commands []string
}

func (r *scriptedRunner) Run(_ context.Context, name string, args ...string) CommandResult {
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	for needle, out := range r.outputs {
		if strings.Contains(command, needle) {
			return CommandResult{Stdout: out}
		}
	}
	return CommandResult{}
}

func (r *scriptedRunner) Start(_ context.Context, _ string, name string, args ...string) (int, error) {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	return 1, nil
}

func (r *scriptedRunner) LookPath(name string) (string, error) { return "/usr/sbin/" + name, nil }

// TestMacClockIsClosedOutsideAVM: en macOS mover el reloj mueve el de la
// maquina entera -- caduca certificados, rompe sesiones, desordena ficheros por
// fecha. Dentro de una VM dedicada eso es barato; en el Mac de alguien no. Por
// eso la puerta esta cerrada salvo que se diga explicitamente.
func TestMacClockIsClosedOutsideAVM(t *testing.T) {
	host := &scriptedRunner{outputs: map[string]string{"sysctl -n kern.hv_vmm_present": "0\n"}}
	cli := CLI{Runner: host, Root: t.TempDir()}
	if cli.macClockAllowed(context.Background(), []string{"travel"}) {
		t.Fatal("fuera de una VM no se toca el reloj sin decirlo")
	}
	if !cli.macClockAllowed(context.Background(), []string{"travel", "--system-clock"}) {
		t.Fatal("quien lo dice explicitamente sabe lo que hace")
	}

	vm := &scriptedRunner{outputs: map[string]string{"sysctl -n kern.hv_vmm_present": "1\n"}}
	inVM := CLI{Runner: vm, Root: t.TempDir()}
	if !inVM.macClockAllowed(context.Background(), []string{"travel"}) {
		t.Fatal("dentro de una VM es el caso para el que existe")
	}
}

// TestMacTimeTravelDisablesNetworkTimeFirst: si no, el sistema deshace el
// cambio en cuanto le apetece consultar la hora, y el sintoma seria una prueba
// que pasa o falla segun el momento en que se ejecute.
func TestMacTimeTravelDisablesNetworkTimeFirst(t *testing.T) {
	runner := &scriptedRunner{}
	cli := CLI{Runner: runner, Root: t.TempDir()}
	at, err := time.Parse(time.RFC3339, "2030-06-15T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.macTimeTravel(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) < 3 || !strings.Contains(runner.commands[0], "-setusingnetworktime off") {
		t.Fatalf("apagar la sincronizacion va PRIMERO: %v", runner.commands)
	}
	joined := strings.Join(runner.commands, "|")
	if !strings.Contains(joined, "-setdate") || !strings.Contains(joined, "-settime") {
		t.Fatalf("%v", runner.commands)
	}
}
