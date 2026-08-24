package macos

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// AxcliID es la clave de registro del driver.
const AxcliID = "axcli"

// Axcli envuelve axcli, que existe en la mezcla por una sola razon: entrega los
// eventos al proceso destino con CGEventPostToPid, sin activar la app, sin
// mover el cursor y sin saltar de Space. Si un agente valida mientras trabajas,
// eso no es un detalle de confort: es la diferencia entre poder usar el Mac y
// no poder.
//
// Queda como escotilla frente a cua-driver, que es el canonico. La razon es
// concreta: cua-driver resuelve la ventana por `list_windows`, que solo enumera
// la capa 0, asi que una UI flotante -- panel, HUD, popover, un onboarding --
// le resulta invisible. axcli apunta por `--app` y no necesita window id, asi
// que alcanza justo esas ventanas.
//
// Su captura NO se declara, y no es una omision: devolvia el escritorio
// recortado a las medidas de la ventana, sin error, cuando el proceso no tiene
// sesion grafica -- y ademas activaba la app. Un PNG plausible pero falso es
// peor que no tener captura.
type Axcli struct {
	exec drivers.Executor
}

var (
	_ drivers.TapDriver  = (*Axcli)(nil)
	_ drivers.TypeDriver = (*Axcli)(nil)
)

// NewAxcli construye el driver.
func NewAxcli(exec drivers.Executor) *Axcli { return &Axcli{exec: exec} }

func (d *Axcli) ID() string { return AxcliID }

func (d *Axcli) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.Kind != drivers.KindMac {
		return drivers.NewSet()
	}
	return drivers.NewSet(
		drivers.CapCoordTap,
		drivers.CapSemanticTap,
		drivers.CapType,
	)
}

// Cost es donde se expresa el reparto con Peekaboo, sin rasgos nuevos en la
// interfaz Driver.
//
// Los taps son coste 0: van por CGEventPostToPid, que es el default de axcli
// para click y scroll, y no roban el foco.
//
// Escribir NO es coste 0, y esto es facil de asumir mal: `input` y `fill`
// activan la app antes de teclear -- lo hacen en el codigo, sin flag para
// evitarlo -- asi que ahi axcli no es mejor que Peekaboo. Se declara igual de
// caro para que el router no lo prefiera creyendo que gana algo.
func (d *Axcli) Cost(c drivers.Capability, _ drivers.Target) int {
	switch c {
	case drivers.CapCoordTap, drivers.CapSemanticTap:
		return 0
	case drivers.CapType:
		return 60
	default:
		return 100
	}
}

// Probe comprueba el binario. axcli no tiene comando de diagnostico: verifica
// AXIsProcessTrusted al arrancar cualquier comando con destino y muere con
// `error: accessibility not granted`. No se invoca aqui a proposito -- pedirle
// permiso a una app real solo para sondear tendria efectos secundarios --, asi
// que el estado de TCC lo reporta Peekaboo, que si sabe preguntarlo sin tocar
// nada.
func (d *Axcli) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("axcli")
	if err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthMissing,
			Detail: "axcli not on PATH",
			Next:   "mav setup --install axcli",
		}
	}
	return drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"axcli": path}}
}

func (d *Axcli) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// axcliTargetArgs apunta a la app. axcli exige --app o --pid en todo comando
// con destino.
func axcliTargetArgs(target drivers.Target) ([]string, error) {
	if target.PID > 0 {
		return []string{"--pid", strconv.Itoa(target.PID)}, nil
	}
	if target.BundleID != "" {
		return []string{"--app", target.BundleID}, nil
	}
	return nil, errors.New("axcli: no app to target; set bundle_id or resolve the pid")
}

// axcliError traduce el fallo. axcli devuelve exit 1 para todo y escribe
// `error: <mensaje>` en stderr, asi que el motivo solo esta en el texto. No se
// intenta clasificarlo por substring mas alla de lo imprescindible: esos
// mensajes son suyos y pueden cambiarlos cuando quieran.
func axcliError(res drivers.ExecResult) error {
	detail := strings.TrimSpace(res.Stderr)
	for _, line := range strings.Split(detail, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "error:") {
			return errors.New(strings.TrimSpace(strings.TrimPrefix(line, "error:")))
		}
	}
	if detail == "" {
		return errors.New("axcli: command failed")
	}
	return errors.New(firstLine(detail))
}

// Tap pulsa sin robar el foco.
func (d *Axcli) Tap(ctx context.Context, target drivers.Target, spec drivers.TapSpec) (drivers.TapResult, error) {
	base, err := axcliTargetArgs(target)
	if err != nil {
		return drivers.TapResult{}, err
	}
	// El cursor software que axcli dibuja por defecto es util para un humano
	// mirando y ruido para un agente: contradice justo lo que se le pide, que
	// es no notarse.
	// --strategy cg-pid va EXPLICITO aunque sea el default. Dos motivos: deja
	// escrito en el comando cual es la propiedad de la que depende mav, y si
	// una version futura cambia el default, esto falla en vez de empezar a
	// robar el foco en silencio.
	//
	// Y no es teorico: la 0.1.0 que publica crates.io no tiene esta opcion
	// siquiera -- su click llama a activate() y clica por coordenadas moviendo
	// el cursor real. Por eso la formula apunta a un commit de main y no a la
	// version publicada.
	//
	// --app/--pid son flags del subcomando, no globales.
	args := []string{"click", "--strategy", "cg-pid"}
	args = append(args, base...)
	switch {
	case spec.Selector.ID != "":
		args = append(args, `[identifier="`+spec.Selector.ID+`"]`)
	case spec.Selector.Text != "":
		args = append(args, `text="`+spec.Selector.Text+`"`)
	case spec.X != 0 || spec.Y != 0:
		// `mouse click` es global e ignora --app: mueve el cursor real y
		// dispara sobre la ventana de encima. Deja de ser background-safe, asi
		// que se dice en vez de fingir que lo es.
		return drivers.TapResult{}, errors.New("axcli: coordinate taps are not background-safe; use a selector")
	default:
		return drivers.TapResult{}, errors.New("axcli: tap requires an id or text selector")
	}
	if res := d.exec.Run(ctx, "axcli", args...); res.Err != nil {
		return drivers.TapResult{}, axcliError(res)
	}
	return drivers.TapResult{MatchedID: spec.Selector.ID, MatchedText: spec.Selector.Text}, nil
}

// Type escribe en un elemento concreto. Ojo: esto SI activa la app -- `fill`
// llama a activate() antes de teclear y no hay forma de evitarlo -- de ahi que
// Cost lo declare caro.
func (d *Axcli) Type(ctx context.Context, target drivers.Target, spec drivers.TextSpec) error {
	if spec.Text == "" {
		return errors.New("axcli: type text missing")
	}
	base, err := axcliTargetArgs(target)
	if err != nil {
		return err
	}
	args := []string{"fill"}
	args = append(args, base...)
	switch {
	case spec.Selector.ID != "":
		args = append(args, `[identifier="`+spec.Selector.ID+`"]`, spec.Text)
	case spec.Selector.Text != "":
		args = append(args, `text="`+spec.Selector.Text+`"`, spec.Text)
	default:
		return errors.New("axcli: typing requires a selector; use cua-driver to type into the focused element")
	}
	if res := d.exec.Run(ctx, "axcli", args...); res.Err != nil {
		return axcliError(res)
	}
	return nil
}
