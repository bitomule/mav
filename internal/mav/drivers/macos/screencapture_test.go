package macos

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type fakeExec struct {
	commands []string
	results  map[string]drivers.ExecResult
	tools    map[string]bool
	startPID int

	// onCommand deja que un test cambie el mundo al ejecutarse un comando, que
	// es lo que hace falta para probar un arranque de demonio: el mismo
	// comando falla antes y funciona despues.
	onCommand func(string)
}

func (f *fakeExec) LookPath(name string) (string, error) {
	if f.tools[name] {
		return "/usr/sbin/" + name, nil
	}
	return "", os.ErrNotExist
}

func (f *fakeExec) Run(_ context.Context, name string, args ...string) drivers.ExecResult {
	command := name + " " + strings.Join(args, " ")
	f.commands = append(f.commands, command)
	if f.onCommand != nil {
		f.onCommand(command)
	}
	for needle, res := range f.results {
		if strings.Contains(command, needle) {
			return res
		}
	}
	return drivers.ExecResult{}
}

func (f *fakeExec) Start(_ context.Context, _ string, name string, args ...string) (int, error) {
	f.commands = append(f.commands, name+" "+strings.Join(args, " "))
	if f.startPID == 0 {
		f.startPID = 4242
	}
	return f.startPID, nil
}

func TestScreencaptureOnlyProvidesOnMac(t *testing.T) {
	d := NewScreencapture(&fakeExec{})
	for _, kind := range []drivers.TargetKind{drivers.KindSim, drivers.KindDevice} {
		if len(d.Provides(drivers.Target{Kind: kind})) != 0 {
			t.Fatalf("screencapture no debe declarar nada en %s: en simulador y dispositivo hay caminos mejores", kind)
		}
	}
	caps := d.Provides(drivers.Target{Kind: drivers.KindMac})
	if !caps.Has(drivers.CapScreenshot) || !caps.Has(drivers.CapVideo) {
		t.Fatalf("en mac debe declarar screenshot y video: %v", caps)
	}
}

func TestScreencaptureIsAFallbackForScreenshots(t *testing.T) {
	d := NewScreencapture(&fakeExec{})
	mac := drivers.Target{Kind: drivers.KindMac}
	// Captura la pantalla entera, que como evidencia de una app concreta es
	// peor que acotarla a su ventana. Un driver que resuelva el id de ventana
	// tiene que poder ganarle por coste.
	if got := d.Cost(drivers.CapScreenshot, mac); got == 0 {
		t.Fatalf("screenshot no deberia ser coste canonico, got %d", got)
	}
	if got := d.Cost(drivers.CapVideo, mac); got != 0 {
		t.Fatalf("video si es canonico: nadie mas graba en el Mac, got %d", got)
	}
}

func TestScreencaptureSilencesTheShutter(t *testing.T) {
	f := &fakeExec{}
	d := NewScreencapture(f)
	if err := d.Screenshot(context.Background(), drivers.Target{Kind: drivers.KindMac}, drivers.ScreenshotSpec{OutPath: "/tmp/x.png"}); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || !strings.Contains(f.commands[0], "-x") {
		t.Fatalf("una sesion automatizada no debe hacer ruido: %v", f.commands)
	}
}

func TestScreencaptureScreenshotRequiresPath(t *testing.T) {
	d := NewScreencapture(&fakeExec{})
	if err := d.Screenshot(context.Background(), drivers.Target{Kind: drivers.KindMac}, drivers.ScreenshotSpec{}); err == nil {
		t.Fatal("sin ruta de salida debe fallar")
	}
}
