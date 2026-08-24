package macos

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// TestAxcliIsTheOnlyInputPathOnMac: tras la leccion de que un click de
// peekaboo puede aterrizar en otra app, axcli es el unico que sirve input en el
// Mac. Si no esta instalado, `ui tap` falla -- que es lo que se quiere -- en vez
// de caer a un camino que pulsa a ciegas.
func TestAxcliIsTheOnlyInputPathOnMac(t *testing.T) {
	ax := NewAxcli(&fakeExec{})
	peek := NewPeekaboo(&fakeExec{})
	mac := macTarget()

	caps := ax.Provides(mac)
	if !caps.Has(drivers.CapSemanticTap) || !caps.Has(drivers.CapType) {
		t.Fatalf("axcli debe servir el input: %v", caps)
	}
	if peek.Provides(mac).Has(drivers.CapSemanticTap) {
		t.Fatal("peekaboo no debe competir por el input")
	}
	// Los taps van por CGEventPostToPid, que no roba el foco: camino canonico.
	if ax.Cost(drivers.CapSemanticTap, mac) != 0 {
		t.Fatal("el tap por PID es el camino bueno en el Mac")
	}
	// Escribir NO es background-safe ni siquiera aqui: `fill` activa la app
	// antes de teclear, en el codigo y sin flag para evitarlo. Se declara caro
	// para que quede dicho que no es gratis, aunque hoy no compita con nadie.
	if ax.Cost(drivers.CapType, mac) == 0 {
		t.Fatal("escribir activa la app: no puede anunciarse como camino canonico")
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

func TestAxcliCommandShapeMatchesTheReleasedVersion(t *testing.T) {
	f := &fakeExec{tools: map[string]bool{"axcli": true}}
	if _, err := NewAxcli(f).Tap(context.Background(), macTarget(), drivers.TapSpec{
		Selector: drivers.ElementSelector{ID: "saveButton"},
	}); err != nil {
		t.Fatal(err)
	}
	// --app va DESPUES del subcomando en 0.1.0, no antes: es flag del
	// subcomando, no global. Verificado contra el binario que instala la
	// formula.
	if len(f.commands) != 1 || !strings.HasPrefix(f.commands[0], "axcli click --app") {
		t.Fatalf("forma del comando incorrecta para 0.1.0: %v", f.commands)
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
