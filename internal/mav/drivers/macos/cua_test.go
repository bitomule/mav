package macos

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func macTarget() drivers.Target {
	return drivers.Target{Kind: drivers.KindMac, BundleID: "com.example.app", PID: 4242}
}

const cuaWindowList = `{"windows":[
 {"window_id":10,"pid":4242,"app_name":"App","title":"","is_on_screen":true,"bounds":{"width":1024,"height":30}},
 {"window_id":11,"pid":4242,"app_name":"App","title":"Main","is_on_screen":true,"bounds":{"width":800,"height":600}},
 {"window_id":12,"pid":999,"app_name":"Otra","title":"","is_on_screen":true,"bounds":{"width":1600,"height":1200}}
]}`

func cuaState1x1() string {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgo=")
	_ = png
	return `{"snapshot_id":"s1","screenshot_png_b64":"aGkK","elements":[
 {"element_index":0,"element_token":"s1:0","role":"AXWindow","frame":{"x":0,"y":0,"w":800,"h":600}},
 {"element_index":1,"element_token":"s1:1","role":"AXButton","label":"Get started","enabled":true,"frame":{"x":10,"y":20,"w":100,"h":30}}
]}`
}

func cuaExec() *fakeExec {
	return &fakeExec{
		tools: map[string]bool{"cua-driver": true},
		results: map[string]drivers.ExecResult{
			"cua-driver call list_windows":     {Stdout: cuaWindowList},
			"cua-driver call get_window_state": {Stdout: cuaState1x1()},
			"cua-driver call click":            {Stdout: `{"effect":"unverifiable","delivery":{"mode":"background"}}`},
			"cua-driver call type_text":        {Stdout: `{"ok":true}`},
			"cua-driver permissions status":    {Stdout: `{"accessibility":true,"screen_recording":true}`},
		},
	}
}

// TestCuaPicksTheLargestOnScreenWindowOfThePid: list_windows devuelve tambien
// la tira de barra de menu que toda app publica, y ventanas de otros procesos.
// Elegir por area visible del pid correcto es lo que evita capturar la barra
// de menu creyendo que es la app.
func TestCuaPicksTheLargestOnScreenWindowOfThePid(t *testing.T) {
	f := cuaExec()
	if _, err := NewCua(f).Tree(context.Background(), macTarget(), drivers.TreeSpec{}); err != nil {
		t.Fatal(err)
	}
	var got string
	for _, c := range f.commands {
		if strings.Contains(c, "get_window_state") {
			got = c
		}
	}
	if !strings.Contains(got, `"window_id":11`) {
		t.Fatalf("debe elegir la ventana de 800x600 del pid 4242: %q", got)
	}
}

// TestCuaAsksForWindowsByPID: sin pid en la peticion, list_windows enumera
// solo la capa 0 -- para no inundar al llamante con tooltips, popovers y el
// Dock -- y una app cuya UI entera es una ventana flotante parece cerrada.
// Nombrando el proceso, admite todas las capas. Filtrar despues en Go no
// sirve: lo que no viene, no se puede filtrar.
func TestCuaAsksForWindowsByPID(t *testing.T) {
	f := cuaExec()
	if _, err := NewCua(f).Tree(context.Background(), macTarget(), drivers.TreeSpec{}); err != nil {
		t.Fatal(err)
	}
	var listing string
	for _, c := range f.commands {
		if strings.Contains(c, "list_windows") {
			listing = c
		}
	}
	if !strings.Contains(listing, `"pid":4242`) {
		t.Fatalf("el pid tiene que ir en la peticion, no aplicarse despues: %q", listing)
	}
}

func TestCuaNoWindowFailsLoudly(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver call list_windows"] = drivers.ExecResult{Stdout: `{"windows":[]}`}
	_, err := NewCua(f).Tree(context.Background(), macTarget(), drivers.TreeSpec{})
	if err == nil {
		t.Fatal("sin ventana no puede haber arbol")
	}
	if !strings.Contains(err.Error(), "no on-screen window") {
		t.Fatalf("el error debe nombrar la causa real: %v", err)
	}
}

// TestCuaRefusalIsAnErrorDespiteExitZero: un rechazo llega con exit 0 y un
// objeto `refusal` en stdout, asi que mirar el codigo de salida no basta.
func TestCuaRefusalIsAnErrorDespiteExitZero(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver call click"] = drivers.ExecResult{
		Stdout: `{"status":"refused","refusal":{"code":"snapshot_id_required","message":"bare element_index is not accepted"}}`,
	}
	_, err := NewCua(f).Tap(context.Background(), macTarget(), drivers.TapSpec{Selector: drivers.ElementSelector{Text: "Get started"}})
	if err == nil {
		t.Fatal("un rechazo es un fallo aunque el exit code sea 0")
	}
	if !strings.Contains(err.Error(), "snapshot_id_required") {
		t.Fatalf("el codigo del rechazo debe llegar al error: %v", err)
	}
}

// TestCuaTapUsesAFreshSnapshotToken: la herramienta invalida el mapa de
// indices en cuanto tomas otro snapshot, y rechaza indices sueltos. Apuntar
// por element_token del snapshot recien tomado es lo que impide pulsar lo que
// ya no esta ahi.
func TestCuaTapUsesAFreshSnapshotToken(t *testing.T) {
	f := cuaExec()
	if _, err := NewCua(f).Tap(context.Background(), macTarget(), drivers.TapSpec{Selector: drivers.ElementSelector{Text: "Get started"}}); err != nil {
		t.Fatal(err)
	}
	var click string
	for _, c := range f.commands {
		if strings.Contains(c, "call click") {
			click = c
		}
	}
	if !strings.Contains(click, `"element_token":"s1:1"`) {
		t.Fatalf("el tap debe ir por el token del snapshot: %q", click)
	}
	if strings.Contains(click, "element_index") {
		t.Fatalf("los indices sueltos los rechaza la herramienta: %q", click)
	}
}

// TestCuaTreeCarriesGeometry: el arbol tiene que traer frame, que es lo que
// axcli no da y lo que el diff entre snapshots necesita para emparejar
// elementos sin identificador.
func TestCuaTreeCarriesGeometry(t *testing.T) {
	res, err := NewCua(cuaExec()).Tree(context.Background(), macTarget(), drivers.TreeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	var nodes []map[string]any
	if err := json.Unmarshal(res.JSON, &nodes); err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes=%v", nodes)
	}
	if nodes[1]["frame"] != "{{10, 20}, {100, 30}}" {
		t.Fatalf("falta la geometria: %v", nodes[1])
	}
	if nodes[1]["role"] != "AXButton" || nodes[1]["label"] != "Get started" {
		t.Fatalf("rol y etiqueta deben pasar tal cual: %v", nodes[1])
	}
}

// TestCuaScreenshotComesFromTheSameCallAsTheTree: imagen y arbol salen del
// mismo get_window_state, asi que describen el mismo instante. Si la captura
// necesitara su propia invocacion, la evidencia visual podria ser de una
// pantalla que ya cambio.
func TestCuaScreenshotComesFromTheSameCallAsTheTree(t *testing.T) {
	f := cuaExec()
	out := filepath.Join(t.TempDir(), "s.png")
	if err := NewCua(f).Screenshot(context.Background(), macTarget(), drivers.ScreenshotSpec{OutPath: out}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hi\n" {
		t.Fatalf("la captura debe salir del base64 de la respuesta: %q", data)
	}
	for _, c := range f.commands {
		if strings.Contains(c, "screenshot") {
			t.Fatalf("no debe haber una segunda llamada de captura: %v", f.commands)
		}
	}
}

// TestCuaUnprovableScreenshotIsAnError: la herramienta omite la imagen a
// proposito cuando no puede probar que corresponde a las medidas pedidas. Eso
// se propaga: una captura que no se puede probar no es evidencia.
func TestCuaUnprovableScreenshotIsAnError(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver call get_window_state"] = drivers.ExecResult{Stdout: `{"snapshot_id":"s1","elements":[{"element_index":0,"element_token":"s1:0","role":"AXWindow"}]}`}
	err := NewCua(f).Screenshot(context.Background(), macTarget(), drivers.ScreenshotSpec{OutPath: filepath.Join(t.TempDir(), "s.png")})
	if err == nil || !strings.Contains(err.Error(), "provable") {
		t.Fatalf("sin imagen probable hay que fallar: %v", err)
	}
}

// TestCuaProbeAsksTheDaemonNotTheCaller: `permissions status` responde con la
// identidad de CuaDriver porque es su propio proceso responsable. Sin demonio
// contesta algo que no es JSON de permisos, y eso es degradado -- no sano --
// porque los permisos del que llama no dicen nada del que captura.
func TestCuaProbeAsksTheDaemonNotTheCaller(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver permissions status"] = drivers.ExecResult{Stdout: `{"accessibility":true,"screen_recording":false}`}
	report := NewCua(f).Probe(context.Background(), f)
	if report.State != drivers.HealthDegraded {
		t.Fatalf("un permiso que falta es degradado: %v", report.State)
	}
	if !strings.Contains(report.Detail, "Screen Recording") {
		t.Fatalf("hay que decir cual falta: %q", report.Detail)
	}
	if report.Next != "cua-driver permissions grant" {
		t.Fatalf("el siguiente paso es su flujo de concesion, que si lo automatiza: %q", report.Next)
	}
}

// TestCuaIsSilentOnNonMacTargets fija que no compite en iOS.
func TestCuaIsSilentOnNonMacTargets(t *testing.T) {
	d := NewCua(cuaExec())
	for _, kind := range []drivers.TargetKind{drivers.KindSim, drivers.KindDevice} {
		if d.Provides(drivers.Target{Kind: kind}).Has(drivers.CapTreeAX) {
			t.Fatalf("kind=%v", kind)
		}
	}
}

// TestCuaResolvesPIDFromBundleID: mav identifica una app por bundle, que es lo
// estable entre runs; cua-driver trabaja por pid, que cambia en cada
// lanzamiento. Si esa traduccion no ocurriera, ningun comando de macOS
// encontraria destino.
func TestCuaResolvesPIDFromBundleID(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver call list_apps"] = drivers.ExecResult{Stdout: `{"apps":[
	 {"bundle_id":"com.otra","name":"Otra","pid":7,"running":true},
	 {"bundle_id":"com.example.app","name":"App","pid":4242,"running":true},
	 {"bundle_id":"com.example.app.old","name":"Vieja","pid":9,"running":false}
	]}`}
	target := macTarget()
	target.PID = 0
	if _, err := NewCua(f).Tree(context.Background(), target, drivers.TreeSpec{}); err != nil {
		t.Fatal(err)
	}
	var state string
	for _, c := range f.commands {
		if strings.Contains(c, "get_window_state") {
			state = c
		}
	}
	if !strings.Contains(state, `"pid":4242`) {
		t.Fatalf("debe traducir bundle -> pid: %q", state)
	}
}

// TestCuaNotRunningSaysSo: sin la app arrancada no hay pid, y el error tiene
// que decir eso y no un fallo de ventana, que lleva a mirar donde no es.
func TestCuaNotRunningSaysSo(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver call list_apps"] = drivers.ExecResult{Stdout: `{"apps":[{"bundle_id":"com.example.app","name":"App","pid":0,"running":false}]}`}
	target := macTarget()
	target.PID = 0
	_, err := NewCua(f).Tree(context.Background(), target, drivers.TreeSpec{})
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("got %v", err)
	}
}

// TestCuaStartsTheDaemonItselfWhenItIsDown: el demonio caido es el unico fallo
// que mav puede resolver solo, y hacerlo evita que cada agente aprenda -- o se
// invente -- el conjuro. Lo importante del comando es `open -g`: arrancar el
// driver no puede robarle el foco a nadie.
func TestCuaStartsTheDaemonItselfWhenItIsDown(t *testing.T) {
	f := cuaExec()
	down := drivers.ExecResult{Stdout: "Cua Driver daemon is not running on /tmp/x.sock.\nStart it first with: cua-driver serve"}
	f.results["cua-driver call list_windows"] = down
	f.onCommand = func(cmd string) {
		if strings.HasPrefix(cmd, "open ") {
			f.results["cua-driver call list_windows"] = drivers.ExecResult{Stdout: cuaWindowList}
		}
	}
	if _, err := NewCua(f).Tree(context.Background(), macTarget(), drivers.TreeSpec{}); err != nil {
		t.Fatal(err)
	}
	var launch string
	for _, c := range f.commands {
		if strings.HasPrefix(c, "open ") {
			launch = c
		}
	}
	if launch == "" {
		t.Fatalf("mav debe levantarlo, no rendirse: %v", f.commands)
	}
	if !strings.Contains(launch, "-g") {
		t.Fatalf("sin -g el arranque roba el foco, que es lo unico que este driver promete no hacer: %q", launch)
	}
	if !strings.Contains(launch, "-a CuaDriver") {
		t.Fatalf("tiene que ir por la app, que es quien tiene los permisos: %q", launch)
	}
}

// TestCuaGivesUpAfterOneStartAttempt: si el demonio no levanta, reintentar en
// cada comando convierte un fallo claro en una sucesion de esperas.
func TestCuaGivesUpAfterOneStartAttempt(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver call list_windows"] = drivers.ExecResult{Stdout: "Cua Driver daemon is not running on /tmp/x.sock."}
	f.results["cua-driver permissions status"] = drivers.ExecResult{Stdout: "{}"}
	d := NewCua(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 3; i++ {
		if _, err := d.Tree(ctx, macTarget(), drivers.TreeSpec{}); err == nil {
			t.Fatal("sin demonio no hay arbol")
		}
	}
	var starts int
	for _, c := range f.commands {
		if strings.HasPrefix(c, "open ") {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("un intento por proceso, no uno por comando: %d", starts)
	}
}
