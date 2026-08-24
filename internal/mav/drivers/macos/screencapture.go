// Package macos agrupa los drivers que operan sobre apps del propio Mac.
//
// A diferencia de iOS, aqui no hay un unico CLI que lo haga todo: el arbol de
// accesibilidad y los menus salen de una herramienta, el input de otra, y las
// capturas del propio sistema. El router ya sabe repartir por capacidad y
// coste, asi que cada driver declara lo que hace bien y deja lo demas.
package macos

import (
	"context"
	"errors"
	"os/exec"
	"strconv"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ScreencaptureID es la clave de registro del driver de capturas del sistema.
const ScreencaptureID = "screencapture"

// Screencapture envuelve el `screencapture` que trae macOS. Es el ultimo
// recurso para CapScreenshot: captura la pantalla entera, que como evidencia de
// una app concreta es peor que una captura acotada a su ventana. Un driver que
// sepa resolver el id de ventana debe declarar coste menor y ganarle.
type Screencapture struct {
	exec drivers.Executor
}

var _ drivers.ScreenshotDriver = (*Screencapture)(nil)

// NewScreencapture construye el driver.
func NewScreencapture(exec drivers.Executor) *Screencapture { return &Screencapture{exec: exec} }

func (d *Screencapture) ID() string { return ScreencaptureID }

// Provides solo declara capacidades en el Mac: en simulador y en dispositivo
// hay caminos mejores y ya cubiertos.
func (d *Screencapture) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.Kind != drivers.KindMac {
		return drivers.NewSet()
	}
	return drivers.NewSet(drivers.CapScreenshot, drivers.CapVideo)
}

// Cost coloca a screencapture como fallback aceptable, no como camino
// canonico: pantalla entera en vez de ventana.
func (d *Screencapture) Cost(c drivers.Capability, _ drivers.Target) int {
	switch c {
	case drivers.CapVideo:
		return 0 // nadie mas graba video en el Mac
	case drivers.CapScreenshot:
		return 50
	default:
		return 100
	}
}

// Probe comprueba que el binario del sistema esta donde deberia. No puede
// comprobar el permiso de Screen Recording: ese pertenece al proceso padre (la
// terminal o el harness del agente), no a mav, y no hay forma barata de
// preguntarlo sin intentar una captura de verdad.
func (d *Screencapture) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("screencapture")
	if err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthMissing,
			Detail: "screencapture not on PATH",
			Next:   "screencapture ships with macOS; check PATH",
		}
	}
	return drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"screencapture": path}}
}

func (d *Screencapture) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// Screenshot captura la pantalla. `-x` silencia el sonido del obturador, que en
// una sesion automatizada solo es ruido.
func (d *Screencapture) Screenshot(ctx context.Context, _ drivers.Target, spec drivers.ScreenshotSpec) error {
	if spec.OutPath == "" {
		return errors.New("screencapture: screenshot output path missing")
	}
	res := d.exec.Run(ctx, "screencapture", "-x", spec.OutPath)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

// VideoStart arranca una grabacion. screencapture -v graba hasta que se le
// manda SIGINT, que es como VideoStop lo para.
func (d *Screencapture) VideoStart(ctx context.Context, _ drivers.Target, spec drivers.VideoSpec) (drivers.VideoResult, error) {
	if spec.OutPath == "" {
		return drivers.VideoResult{}, errors.New("screencapture: video output path missing")
	}
	pid, err := d.exec.Start(ctx, "", "screencapture", "-v", "-x", spec.OutPath)
	if err != nil {
		return drivers.VideoResult{}, err
	}
	return drivers.VideoResult{PID: pid, OutPath: spec.OutPath}, nil
}

// VideoStop corta la grabacion con SIGINT. Matar con SIGKILL dejaria el .mov a
// medio escribir y sin indice, es decir ilegible.
func (d *Screencapture) VideoStop(_ context.Context, _ drivers.Target, pid int) error {
	if pid <= 0 {
		return errors.New("screencapture: video pid missing")
	}
	if err := exec.Command("kill", "-INT", strconv.Itoa(pid)).Run(); err != nil {
		return err
	}
	return nil
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	if s == "" {
		return "command failed"
	}
	return s
}
