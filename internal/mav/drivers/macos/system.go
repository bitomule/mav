package macos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// SystemID es la clave de registro del driver.
const SystemID = "macsystem"

// System cubre el ciclo de vida y las utilidades de una app de macOS con las
// herramientas que trae el propio sistema. No envuelve ningun CLI de terceros
// porque no hace falta: `open`, `pbcopy`, `pbpaste` y el filesystem bastan.
//
// Existe sobre todo por una razon concreta: sin un proveedor de CapTerminate en
// el Mac, el cierre de la app previo a sembrar un fixture era un no-op
// silencioso, y el fixture escribiria la base de datos con la instancia
// anterior teniendola abierta.
type System struct {
	exec drivers.Executor
}

var (
	_ drivers.LifecycleDriver     = (*System)(nil)
	_ drivers.DeviceUtilityDriver = (*System)(nil)
)

// NewSystem construye el driver.
func NewSystem(exec drivers.Executor) *System { return &System{exec: exec} }

func (d *System) ID() string { return SystemID }

func (d *System) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.Kind != drivers.KindMac {
		return drivers.NewSet()
	}
	return drivers.NewSet(
		drivers.CapInstall,
		drivers.CapLaunch,
		drivers.CapUninstall,
		drivers.CapTerminate,
		drivers.CapOpenURL,
		drivers.CapClipboard,
		drivers.CapAppList,
	)
}

// Cost: es el unico proveedor de todo esto en el Mac.
func (d *System) Cost(drivers.Capability, drivers.Target) int { return 0 }

// Probe no necesita comprobar nada instalable: son binarios del sistema. Lo que
// si comprueba es que estamos en macOS, porque un target mac en otro sistema no
// es un fallo recuperable sino una config imposible.
func (d *System) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	if _, err := p.LookPath("open"); err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthMissing,
			Detail: "`open` not on PATH; macOS targets need a Mac",
		}
	}
	return drivers.HealthReport{State: drivers.HealthOK}
}

func (d *System) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// Install en macOS no copia nada a /Applications: para validar se ejecuta la
// app donde la dejo el build. "Instalar" aqui es comprobar que el bundle esta
// donde decimos, que es la unica parte que puede fallar y la que da un error
// util cuando la receta de lanzamiento no produjo lo que creia.
func (d *System) Install(_ context.Context, _ drivers.Target, spec drivers.InstallSpec) error {
	if strings.TrimSpace(spec.Path) == "" {
		return errors.New("macsystem: no app path; the launch recipe must produce one via app_path")
	}
	info, err := os.Stat(spec.Path)
	if err != nil {
		return errors.New("macsystem: app bundle not found at " + spec.Path)
	}
	if !info.IsDir() || !strings.HasSuffix(spec.Path, ".app") {
		return errors.New("macsystem: not an app bundle: " + spec.Path)
	}
	return nil
}

// Launch ejecuta el binario de dentro del bundle en vez de usar `open`.
//
// No es un capricho: `open` no propaga variables de entorno al proceso que
// arranca, y el entorno es justo como mav inyecta su configuracion (el
// equivalente de los SIMCTL_CHILD_* del simulador). Ejecutar
// Contents/MacOS/<binario> directamente lo hereda todo.
func (d *System) Launch(ctx context.Context, target drivers.Target, spec drivers.LaunchSpec) (drivers.LaunchResult, error) {
	if target.AppPath == "" {
		return drivers.LaunchResult{}, errors.New("macsystem: launch needs the app bundle path")
	}
	binary, err := bundleExecutable(target.AppPath)
	if err != nil {
		return drivers.LaunchResult{}, err
	}
	pid, err := d.exec.Start(ctx, "", binary, spec.Args...)
	if err != nil {
		return drivers.LaunchResult{}, err
	}
	return drivers.LaunchResult{PID: pid, BundleID: spec.BundleID}, nil
}

// bundleExecutable resuelve Contents/MacOS/<binario>. Se elige el unico
// ejecutable que haya ahi en vez de asumir que se llama como el bundle, porque
// no siempre coincide -- el de Nokoru, sin ir mas lejos, no se llama Nokoru en
// todas sus variantes.
func bundleExecutable(appPath string) (string, error) {
	dir := filepath.Join(appPath, "Contents", "MacOS")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", errors.New("macsystem: no executable directory in " + appPath)
	}
	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		candidates = append(candidates, filepath.Join(dir, entry.Name()))
	}
	switch len(candidates) {
	case 0:
		return "", errors.New("macsystem: no executable in " + dir)
	case 1:
		return candidates[0], nil
	default:
		// Con varios, el que se llama como el bundle es la convencion.
		want := strings.TrimSuffix(filepath.Base(appPath), ".app")
		for _, candidate := range candidates {
			if filepath.Base(candidate) == want {
				return candidate, nil
			}
		}
		return "", errors.New("macsystem: ambiguous executable in " + dir)
	}
}

// Uninstall es el equivalente honesto de `simctl uninstall` en el Mac: no
// desinstala la app -- que se ejecuta desde donde este -- sino que borra su
// estado, que es lo que --clear-state quiere decir de verdad.
func (d *System) Uninstall(ctx context.Context, _ drivers.Target, bundleID string) error {
	if bundleID == "" {
		return errors.New("macsystem: uninstall needs a bundle id")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	// Sandboxed: todo cuelga del contenedor. Sin sandbox, los preferences
	// viven aparte, asi que hay que borrar los dos y no dar por hecho cual es.
	container := filepath.Join(home, "Library", "Containers", bundleID)
	if err := os.RemoveAll(container); err != nil {
		return err
	}
	d.exec.Run(ctx, "defaults", "delete", bundleID)
	return nil
}

// Boot no existe en el Mac: la maquina ya esta arrancada.
func (d *System) Boot(_ context.Context, _ drivers.Target) error { return nil }

// Terminate cierra la app. `osascript quit` pide un cierre limpio, que es lo
// que permite a la app cerrar su base de datos en vez de dejar el WAL a medias
// -- justo lo que el fixture necesita antes de sembrar.
func (d *System) Terminate(ctx context.Context, _ drivers.Target, bundleID string) error {
	if bundleID == "" {
		return errors.New("macsystem: terminate needs a bundle id")
	}
	script := `tell application id "` + bundleID + `" to quit`
	if res := d.exec.Run(ctx, "osascript", "-e", script); res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *System) OpenURL(ctx context.Context, _ drivers.Target, url string) error {
	if res := d.exec.Run(ctx, "open", url); res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *System) ClipboardWrite(ctx context.Context, _ drivers.Target, text string) error {
	input, ok := d.exec.(drivers.InputExecutor)
	if !ok {
		return errors.New("macsystem: input executor unavailable")
	}
	if res := input.RunInput(ctx, text, "pbcopy"); res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *System) ClipboardRead(ctx context.Context, _ drivers.Target) (string, error) {
	res := d.exec.Run(ctx, "pbpaste")
	if res.Err != nil {
		return "", errors.New(firstLine(res.Stderr))
	}
	return res.Stdout, nil
}

func (d *System) ListApps(ctx context.Context, _ drivers.Target) (string, error) {
	res := d.exec.Run(ctx, "ls", "/Applications")
	if res.Err != nil {
		return "", errors.New(firstLine(res.Stderr))
	}
	return res.Stdout, nil
}

// SetLocation y ResetLocation no tienen equivalente: macOS no permite
// sobreescribir la ubicacion de una app ya lanzada. El camino que si existe es
// el "Simulate Location" de un scheme de Xcode, que ocurre en el lanzamiento y
// no aqui.
func (d *System) SetLocation(context.Context, drivers.Target, float64, float64) error {
	return errors.New("location_unsupported_on_macos")
}

func (d *System) ResetLocation(context.Context, drivers.Target) error {
	return errors.New("location_unsupported_on_macos")
}
