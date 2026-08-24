package macos

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// TestAxcliWinsTapsAndNotTyping es el nucleo del reparto con Peekaboo, y la
// razon por la que no hizo falta un rasgo BackgroundSafe en la interfaz Driver:
// Cost ya es por capacidad y por target.
func TestAxcliWinsTapsAndNotTyping(t *testing.T) {
	ax := NewAxcli(&fakeExec{})
	peek := NewPeekaboo(&fakeExec{})
	mac := macTarget()

	// Los taps van por CGEventPostToPid, que no roba el foco: axcli gana.
	if ax.Cost(drivers.CapCoordTap, mac) >= peek.Cost(drivers.CapCoordTap, mac) {
		t.Fatal("axcli debe ganar los taps: entrega por PID sin activar la app")
	}
	// Escribir NO: `fill` activa la app igualmente, asi que no aporta nada
	// sobre peekaboo y no debe preferirse creyendo que si.
	if ax.Cost(drivers.CapType, mac) < peek.Cost(drivers.CapType, mac) {
		t.Fatal("axcli no es mejor escribiendo: fill activa la app igual que peekaboo")
	}
}

// TestAxcliRefusesCoordinateTaps: `mouse click` es global, ignora --app, mueve
// el cursor real y dispara sobre la ventana de encima. Aceptarlo aqui seria
// vender como background-safe algo que no lo es.
func TestAxcliRefusesCoordinateTaps(t *testing.T) {
	f := &fakeExec{tools: map[string]bool{"axcli": true}}
	_, err := NewAxcli(f).Tap(context.Background(), macTarget(), drivers.TapSpec{X: 10, Y: 20})
	if err == nil || !strings.Contains(err.Error(), "background-safe") {
		t.Fatalf("un tap por coordenadas no es background-safe y debe rechazarse: %v", err)
	}
	if len(f.commands) != 0 {
		t.Fatalf("no debe ejecutarse nada: %v", f.commands)
	}
}

func TestAxcliDisablesTheVisualCursor(t *testing.T) {
	f := &fakeExec{tools: map[string]bool{"axcli": true}}
	if _, err := NewAxcli(f).Tap(context.Background(), macTarget(), drivers.TapSpec{
		Selector: drivers.ElementSelector{ID: "saveButton"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || !strings.Contains(f.commands[0], "--no-visual-cursor") {
		t.Fatalf("el cursor software contradice lo que se le pide a un agente: %v", f.commands)
	}
	if !strings.Contains(f.commands[0], `identifier="saveButton"`) {
		t.Fatalf("el selector debe ir por identificador: %v", f.commands)
	}
}

// TestAxcliErrorKeepsTheToolMessage: axcli devuelve exit 1 para todo y el
// motivo solo esta en el texto de stderr.
func TestAxcliErrorKeepsTheToolMessage(t *testing.T) {
	f := &fakeExec{
		tools: map[string]bool{"axcli": true},
		results: map[string]drivers.ExecResult{
			"axcli": {Stderr: "Found app: X (pid=1)\nerror: locator not found: [identifier=\"nope\"]\n", Code: 1, Err: errors.New("exit status 1")},
		},
	}
	_, err := NewAxcli(f).Tap(context.Background(), macTarget(), drivers.TapSpec{
		Selector: drivers.ElementSelector{ID: "nope"},
	})
	if err == nil {
		t.Fatal("debe fallar")
	}
	// stderr trae tambien lineas de estado ("Found app: ..."), asi que hay que
	// quedarse con la linea de error y no con la primera.
	if !strings.Contains(err.Error(), "locator not found") {
		t.Fatalf("el motivo real debe sobrevivir: %v", err)
	}
	if strings.Contains(err.Error(), "Found app") {
		t.Fatalf("las lineas de estado no son el error: %v", err)
	}
}

func TestAxcliNeedsAnAppToTarget(t *testing.T) {
	f := &fakeExec{tools: map[string]bool{"axcli": true}}
	_, err := NewAxcli(f).Tap(context.Background(), drivers.Target{Kind: drivers.KindMac}, drivers.TapSpec{
		Selector: drivers.ElementSelector{ID: "x"},
	})
	if err == nil {
		t.Fatal("axcli exige --app o --pid en todo comando con destino")
	}
}

func TestAxcliDoesNotProvideTheTree(t *testing.T) {
	// Su arbol es texto indentado, truncado y sin geometria ni estado.
	// Declararlo obligaria al router a elegir entre dos formatos distintos por
	// un motivo que no tiene que ver con la calidad del dato.
	caps := NewAxcli(&fakeExec{}).Provides(macTarget())
	if caps.Has(drivers.CapTreeAX) {
		t.Fatal("el arbol lo da peekaboo, que emite JSON")
	}
}
