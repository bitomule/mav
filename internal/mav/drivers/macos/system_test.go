package macos

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func makeBundle(t *testing.T, name string, executables ...string) string {
	t.Helper()
	root := t.TempDir()
	app := filepath.Join(root, name+".app")
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, exe := range executables {
		if err := os.WriteFile(filepath.Join(macos, exe), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return app
}

// TestSystemLaunchesTheBinaryNotOpen: `open` no propaga variables de entorno al
// proceso que arranca, y el entorno es justo como mav inyecta su configuracion
// -- el equivalente de los SIMCTL_CHILD_* del simulador.
func TestSystemLaunchesTheBinaryNotOpen(t *testing.T) {
	app := makeBundle(t, "Nokoru", "Nokoru")
	f := &fakeExec{tools: map[string]bool{"open": true}}
	res, err := NewSystem(f).Launch(context.Background(), drivers.Target{Kind: drivers.KindMac, AppPath: app}, drivers.LaunchSpec{BundleID: "com.example.app"})
	if err != nil {
		t.Fatal(err)
	}
	if res.PID == 0 {
		t.Fatal("debe devolver el pid del proceso lanzado")
	}
	if len(f.commands) != 1 || !strings.Contains(f.commands[0], "Contents/MacOS/Nokoru") {
		t.Fatalf("debe ejecutarse el binario del bundle, no `open`: %v", f.commands)
	}
	if strings.HasPrefix(f.commands[0], "open ") {
		t.Fatalf("`open` perderia el entorno: %v", f.commands)
	}
}

// El binario no siempre se llama como el bundle, asi que se elige el unico que
// haya antes de asumir el nombre.
func TestSystemResolvesASingleExecutableWhateverItsName(t *testing.T) {
	app := makeBundle(t, "Nokoru", "nNokoru")
	f := &fakeExec{}
	if _, err := NewSystem(f).Launch(context.Background(), drivers.Target{Kind: drivers.KindMac, AppPath: app}, drivers.LaunchSpec{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.commands[0], "nNokoru") {
		t.Fatalf("%v", f.commands)
	}
}

func TestSystemPrefersTheBundleNameWhenAmbiguous(t *testing.T) {
	app := makeBundle(t, "Nokoru", "helper", "Nokoru")
	f := &fakeExec{}
	if _, err := NewSystem(f).Launch(context.Background(), drivers.Target{Kind: drivers.KindMac, AppPath: app}, drivers.LaunchSpec{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(f.commands[0]), "/Nokoru") {
		t.Fatalf("con varios candidatos manda la convencion del nombre: %v", f.commands)
	}
}

// Install en macOS no copia nada: comprueba que el bundle esta donde la receta
// dijo. Es la unica parte que puede fallar, y da un error util cuando el build
// no produjo lo que creia.
func TestSystemInstallVerifiesTheBundleExists(t *testing.T) {
	d := NewSystem(&fakeExec{})
	if err := d.Install(context.Background(), drivers.Target{Kind: drivers.KindMac}, drivers.InstallSpec{Path: "/nope/Missing.app"}); err == nil {
		t.Fatal("un bundle que no existe debe fallar aqui, no dos capas mas abajo")
	}
	app := makeBundle(t, "Nokoru", "Nokoru")
	if err := d.Install(context.Background(), drivers.Target{Kind: drivers.KindMac}, drivers.InstallSpec{Path: app}); err != nil {
		t.Fatalf("un bundle valido no debe fallar: %v", err)
	}
}

// TestSystemTerminateAsksForACleanQuit: el fixture necesita que la app haya
// cerrado su base de datos, no que la hayan matado con el WAL a medias.
func TestSystemTerminateAsksForACleanQuit(t *testing.T) {
	f := &fakeExec{}
	if err := NewSystem(f).Terminate(context.Background(), drivers.Target{Kind: drivers.KindMac}, "com.example.app"); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || !strings.Contains(f.commands[0], "to quit") {
		t.Fatalf("debe pedirse un cierre limpio: %v", f.commands)
	}
	if strings.Contains(f.commands[0], "kill") {
		t.Fatalf("matarla dejaria el WAL a medias: %v", f.commands)
	}
}

func TestSystemProvidesTerminateOnMac(t *testing.T) {
	// Sin esto, el cierre previo a sembrar un fixture era un no-op silencioso
	// y el fixture escribia con la instancia anterior teniendo el sqlite
	// abierto.
	caps := NewSystem(&fakeExec{}).Provides(drivers.Target{Kind: drivers.KindMac})
	if !caps.Has(drivers.CapTerminate) {
		t.Fatal("alguien tiene que proveer CapTerminate en el Mac")
	}
	if len(NewSystem(&fakeExec{}).Provides(drivers.Target{Kind: drivers.KindSim})) != 0 {
		t.Fatal("en simulador manda simctl")
	}
}

func TestSystemLocationIsHonestlyUnsupported(t *testing.T) {
	d := NewSystem(&fakeExec{})
	if err := d.SetLocation(context.Background(), drivers.Target{Kind: drivers.KindMac}, 1, 2); err == nil {
		t.Fatal("macOS no permite sobreescribir la ubicacion de una app ya lanzada; hay que decirlo")
	}
}
