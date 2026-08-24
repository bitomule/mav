package macos

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

const permissionsGranted = `{"success":true,"data":{"permissions":[{"name":"Screen Recording","isGranted":true},{"name":"Accessibility","isGranted":true}]}}`

func macTarget() drivers.Target {
	return drivers.Target{Kind: drivers.KindMac, BundleID: "com.example.app"}
}

// TestPeekabooErrorsComeFromStdoutNotExitCode fija la trampa principal de esta
// herramienta: los errores salen por stdout como JSON y el exit code es 1 para
// todos por igual, asi que el codigo real solo esta dentro del JSON. Un wrapper
// que mire stderr o el exit code se queda sin saber que paso.
func TestPeekabooErrorsComeFromStdoutNotExitCode(t *testing.T) {
	f := &fakeExec{
		tools: map[string]bool{"peekaboo": true},
		results: map[string]drivers.ExecResult{
			"peekaboo see": {
				Stdout: `{"success":false,"error":{"code":"APP_NOT_FOUND","message":"Application 'x' not found"}}`,
				Code:   1,
			},
		},
	}
	d := NewPeekaboo(f)
	_, err := d.Tree(context.Background(), macTarget(), drivers.TreeSpec{})
	if err == nil {
		t.Fatal("un fallo debe propagarse")
	}
	if !strings.Contains(err.Error(), "APP_NOT_FOUND") {
		t.Fatalf("el codigo del JSON debe llegar al error: %v", err)
	}
}

// TestPeekabooTreeAcceptsBothOutputShapes: en 3.0.0-beta3 el elemento de stdout
// no trae bounds ni is_enabled -- viven en el fichero de snapshot -- mientras
// que en main ya salen inline. Aceptar las dos formas evita que actualizar
// Peekaboo rompa el driver.
func TestPeekabooTreeAcceptsBothOutputShapes(t *testing.T) {
	beta := `{"success":true,"data":{"ui_elements":[{"id":"elem_1","identifier":"saveButton","role":"button","label":"Guardar","is_actionable":true}]}}`
	main := `{"success":true,"data":{"ui_elements":[{"id":"elem_1","identifier":"saveButton","role":"button","label":"Guardar","is_enabled":true,"bounds":{"x":10,"y":20,"width":80,"height":24}}]}}`

	for name, stdout := range map[string]string{"beta3": beta, "upstream-main": main} {
		f := &fakeExec{tools: map[string]bool{"peekaboo": true}, results: map[string]drivers.ExecResult{"peekaboo see": {Stdout: stdout}}}
		res, err := NewPeekaboo(f).Tree(context.Background(), macTarget(), drivers.TreeSpec{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var nodes []map[string]any
		if err := json.Unmarshal(res.JSON, &nodes); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(nodes) != 1 {
			t.Fatalf("%s: nodes=%v", name, nodes)
		}
		// El identificador AX gana a elem_N: es el estable entre runs, que es
		// lo que un agente espera de --id.
		if nodes[0]["identifier"] != "saveButton" {
			t.Fatalf("%s: identifier=%v", name, nodes[0]["identifier"])
		}
		if nodes[0]["label"] != "Guardar" {
			t.Fatalf("%s: label=%v", name, nodes[0]["label"])
		}
	}
}

func TestPeekabooFallsBackToElemIDWhenThereIsNoAXIdentifier(t *testing.T) {
	stdout := `{"success":true,"data":{"ui_elements":[{"id":"elem_7","role":"button","label":"Sin id"}]}}`
	f := &fakeExec{tools: map[string]bool{"peekaboo": true}, results: map[string]drivers.ExecResult{"peekaboo see": {Stdout: stdout}}}
	res, err := NewPeekaboo(f).Tree(context.Background(), macTarget(), drivers.TreeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	var nodes []map[string]any
	if err := json.Unmarshal(res.JSON, &nodes); err != nil {
		t.Fatal(err)
	}
	if nodes[0]["identifier"] != "elem_7" {
		t.Fatalf("sin identificador AX debe usarse elem_N para poder pulsarlo: %v", nodes[0])
	}
}

// TestPeekabooIsExpensiveForInput es la decision de diseno que sustituye a un
// rasgo BackgroundSafe en la interfaz: el reparto con axcli se expresa con
// Cost, que ya es por capacidad y por target.
func TestPeekabooIsExpensiveForInput(t *testing.T) {
	d := NewPeekaboo(&fakeExec{})
	mac := macTarget()
	if d.Cost(drivers.CapTreeAX, mac) != 0 {
		t.Fatal("el arbol es lo que solo peekaboo sabe hacer")
	}
	if d.Cost(drivers.CapScreenshot, mac) != 0 {
		t.Fatal("captura acotada a la ventana debe ganar a la pantalla entera de screencapture")
	}
	if d.Cost(drivers.CapType, mac) <= d.Cost(drivers.CapTreeAX, mac) {
		t.Fatal("su input activa la app, asi que debe ser caro para que le gane uno que entregue por PID")
	}
}

func TestPeekabooProbeReportsMissingPermissionAgainstTheParentProcess(t *testing.T) {
	denied := `{"success":true,"data":{"permissions":[{"name":"Accessibility","isGranted":false}]}}`
	f := &fakeExec{tools: map[string]bool{"peekaboo": true}, results: map[string]drivers.ExecResult{"peekaboo permissions": {Stdout: denied}}}
	report := NewPeekaboo(f).Probe(context.Background(), f)
	if report.State != drivers.HealthDegraded {
		t.Fatalf("un permiso que falta es degradado, no roto: se puede conceder sin reinstalar nada. got %v", report.State)
	}
	if !strings.Contains(report.Next, "terminal or agent harness") {
		t.Fatalf("el mensaje debe decir a quien hay que dar el permiso, que no es mav: %q", report.Next)
	}
}

func TestPeekabooProbeHealthyWhenPermissionsGranted(t *testing.T) {
	f := &fakeExec{tools: map[string]bool{"peekaboo": true}, results: map[string]drivers.ExecResult{"peekaboo permissions": {Stdout: permissionsGranted}}}
	if report := NewPeekaboo(f).Probe(context.Background(), f); report.State != drivers.HealthOK {
		t.Fatalf("state=%v detail=%q", report.State, report.Detail)
	}
}

func TestPeekabooRefusesToPromiseTypingIntoAnElement(t *testing.T) {
	f := &fakeExec{tools: map[string]bool{"peekaboo": true}}
	err := NewPeekaboo(f).Type(context.Background(), macTarget(), drivers.TextSpec{
		Text:     "hola",
		Selector: drivers.ElementSelector{ID: "campo"},
	})
	// Su propio help dice que hay que enfocar antes con click. Aceptar el
	// selector y escribir en otro sitio seria peor que rechazarlo.
	if err == nil {
		t.Fatal("escribir en un elemento concreto no esta soportado y debe decirse")
	}
}

func TestPeekabooOnlyProvidesOnMac(t *testing.T) {
	d := NewPeekaboo(&fakeExec{})
	if len(d.Provides(drivers.Target{Kind: drivers.KindSim})) != 0 {
		t.Fatal("en simulador manda AXe")
	}
}
