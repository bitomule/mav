package macos

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// CuaID es la clave de registro del driver.
const CuaID = "cua"

// Cua envuelve cua-driver (trycua/cua, MIT), el driver canonico de mav en macOS.
//
// Entra por una razon estructural, no por preferencia: en macOS los permisos de
// Accessibility y Screen Recording se conceden SOLO a procesos GUI
// interactivos. Un CLI no puede tenerlos por mucho que se los concedas a la
// terminal. La unica arquitectura que funciona es un broker -- una app con los
// permisos, y un socket -- y cua-driver la trae de serie: el binario que
// invocamos vive dentro de /Applications/CuaDriver.app.
//
// Lo que aporta frente a lo que habia, medido dentro de una VM contra una
// ventana flotante (layer != 0), que es el caso que rompia a los demas:
//
//   - Peekaboo enumeraba esa ventana y la descartaba sola por la capa: ni
//     arbol ni captura.
//   - axcli leia el arbol, pero su captura devolvia el ESCRITORIO recortado a
//     las medidas de la ventana -- sin error -- y ademas activaba la app.
//   - cua-driver devuelve arbol CON geometria y la captura de la ventana con
//     contenido real en la MISMA llamada, y el click llega en segundo plano.
//
// Dos limites conocidos, ambos suyos:
//
//   - `list_windows` es layer-0 only por diseno declarado, asi que una ventana
//     flotante no aparece en el descubrimiento.
//   - sus elementos no exponen AXIdentifier: solo element_token (valido dentro
//     del snapshot), rol, etiqueta y frame.
type Cua struct {
	exec drivers.Executor
}

var (
	_ drivers.TreeDriver       = (*Cua)(nil)
	_ drivers.ScreenshotDriver = (*Cua)(nil)
	_ drivers.TapDriver        = (*Cua)(nil)
	_ drivers.TypeDriver       = (*Cua)(nil)
)

// NewCua construye el driver.
func NewCua(exec drivers.Executor) *Cua { return &Cua{exec: exec} }

func (d *Cua) ID() string { return CuaID }

func (d *Cua) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.Kind != drivers.KindMac {
		return drivers.NewSet()
	}
	return drivers.NewSet(
		drivers.CapTreeAX,
		drivers.CapScreenshot,
		drivers.CapCoordTap,
		drivers.CapSemanticTap,
		drivers.CapType,
	)
}

// Cost lo declara canonico en todo lo que provee: es el unico que cubre las
// cuatro capacidades con entrega en segundo plano verificada.
func (d *Cua) Cost(c drivers.Capability, _ drivers.Target) int {
	switch c {
	case drivers.CapTreeAX, drivers.CapScreenshot, drivers.CapCoordTap, drivers.CapSemanticTap, drivers.CapType:
		return 0
	default:
		return 100
	}
}

// Probe pregunta por los permisos al demonio, no al proceso que corre mav.
//
// Es la diferencia que importa: `permissions status` responde con la identidad
// de CuaDriver (com.trycua.driver) porque es su propio proceso responsable. Si
// no hay demonio contesta `unknown` en vez de mentir con los permisos de tu
// terminal, y por eso ese caso se reporta como degradado y no como sano.
func (d *Cua) Probe(ctx context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("cua-driver")
	if err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthMissing,
			Detail: "cua-driver not on PATH",
			Next:   "mav setup --install cua-driver",
		}
	}
	report := drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"cua-driver": path}}
	res := d.exec.Run(ctx, "cua-driver", "permissions", "status", "--json")
	var status struct {
		Accessibility   *bool `json:"accessibility"`
		ScreenRecording *bool `json:"screen_recording"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &status); err != nil {
		report.State = drivers.HealthDegraded
		report.Detail = "cua-driver daemon not answering"
		report.Next = "open -n -g -a CuaDriver --args serve"
		return report
	}
	var missing []string
	if status.Accessibility == nil || !*status.Accessibility {
		missing = append(missing, "Accessibility")
	}
	if status.ScreenRecording == nil || !*status.ScreenRecording {
		missing = append(missing, "Screen Recording")
	}
	if len(missing) > 0 {
		report.State = drivers.HealthDegraded
		report.Detail = "missing TCC permission: " + strings.Join(missing, ", ")
		// Su comando de concesion lanza la app por LaunchServices para que los
		// dialogos se atribuyan a ella y la registra en los paneles. Es la
		// unica herramienta de las probadas que automatiza esto; a las demas
		// hay que anadirlas a mano con el "+".
		report.Next = "cua-driver permissions grant"
	}
	return report
}

func (d *Cua) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// cuaCall invoca una herramienta del driver. La forma es siempre
// `cua-driver call <tool> '<json>'` y la respuesta es JSON por stdout.
func (d *Cua) cuaCall(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	payload, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	res := d.exec.Run(ctx, "cua-driver", "call", tool, string(payload))
	if strings.TrimSpace(res.Stdout) == "" {
		if res.Err != nil {
			return nil, fmt.Errorf("cua-driver %s: %s", tool, firstLine(res.Stderr))
		}
		return nil, fmt.Errorf("cua-driver %s: empty response", tool)
	}
	// Un rechazo llega con exit 0 y un objeto `refusal`, asi que mirar el
	// codigo de salida no basta.
	var refusal struct {
		Refusal *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"refusal"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &refusal); err == nil && refusal.Refusal != nil {
		return nil, fmt.Errorf("%s: %s", refusal.Refusal.Code, firstLine(refusal.Refusal.Message))
	}
	return []byte(res.Stdout), nil
}

// cuaWindow es una fila de list_windows.
type cuaWindow struct {
	WindowID int    `json:"window_id"`
	PID      int    `json:"pid"`
	AppName  string `json:"app_name"`
	Title    string `json:"title"`
	OnScreen bool   `json:"is_on_screen"`
	Bounds   struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"bounds"`
}

// resolvePID traduce bundle id a pid.
//
// mav identifica una app de macOS por su bundle, que es lo estable entre runs;
// cua-driver trabaja siempre por pid, que cambia en cada lanzamiento. La
// traduccion se hace aqui y no en la capa de target porque es una propiedad de
// esta herramienta, no del modelo de destino.
func (d *Cua) resolvePID(ctx context.Context, target drivers.Target) (int, error) {
	if target.PID > 0 {
		return target.PID, nil
	}
	if target.BundleID == "" {
		return 0, errors.New("cua: no app to target; set bundle_id")
	}
	raw, err := d.cuaCall(ctx, "list_apps", map[string]any{})
	if err != nil {
		return 0, err
	}
	apps, err := decodeCuaApps(raw)
	if err != nil {
		return 0, err
	}
	for _, a := range apps {
		if a.Running && a.PID > 0 && a.BundleID == target.BundleID {
			return a.PID, nil
		}
	}
	return 0, fmt.Errorf("cua: %s is not running", target.BundleID)
}

// cuaApp es una fila de list_apps. Incluye instaladas y no arrancadas, de ahi
// que haya que filtrar por Running antes de fiarse del pid.
type cuaApp struct {
	BundleID string `json:"bundle_id"`
	Name     string `json:"name"`
	PID      int    `json:"pid"`
	Running  bool   `json:"running"`
}

func decodeCuaApps(raw []byte) ([]cuaApp, error) {
	var flat struct {
		Apps       []cuaApp `json:"apps"`
		Structured *struct {
			Apps []cuaApp `json:"apps"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, fmt.Errorf("cua: unreadable app list: %w", err)
	}
	if len(flat.Apps) > 0 {
		return flat.Apps, nil
	}
	if flat.Structured != nil {
		return flat.Structured.Apps, nil
	}
	return nil, nil
}

// resolveWindow elige la ventana de la app.
//
// El criterio es el area visible mayor y no el z-order porque z_index puede
// venir nulo -- la propia herramienta avisa de que entonces no se puede
// inferir orden -- mientras que las medidas siempre estan.
func (d *Cua) resolveWindow(ctx context.Context, target drivers.Target) (cuaWindow, error) {
	pid, err := d.resolvePID(ctx, target)
	if err != nil {
		return cuaWindow{}, err
	}
	target.PID = pid
	// El pid va en la peticion y no se filtra despues, y no es una
	// optimizacion: sin pid la herramienta enumera solo la capa 0 -- para no
	// inundar al llamante con tooltips, popovers, menus y el Dock -- mientras
	// que nombrando el proceso admite todas las capas. Es la unica forma de
	// alcanzar una app cuya UI entera vive en una ventana accesoria.
	raw, err := d.cuaCall(ctx, "list_windows", map[string]any{"pid": pid})
	if err != nil {
		return cuaWindow{}, err
	}
	windows, err := decodeCuaWindows(raw)
	if err != nil {
		return cuaWindow{}, err
	}
	var best cuaWindow
	var bestArea float64
	for _, w := range windows {
		if w.PID != pid || !w.OnScreen {
			continue
		}
		area := w.Bounds.Width * w.Bounds.Height
		if area > bestArea {
			best, bestArea = w, area
		}
	}
	if bestArea == 0 {
		// Caso real y no hipotetico: una ventana flotante (panel, HUD,
		// popover, onboarding) no sale en list_windows porque la herramienta
		// solo enumera la capa 0. El mensaje lo dice para que nadie lo lea
		// como "la app no esta abierta", que es a lo que se parece.
		return cuaWindow{}, fmt.Errorf("cua: no on-screen window for pid %d", pid)
	}
	return best, nil
}

// decodeCuaWindows saca la lista venga plana o dentro de structuredContent,
// que depende del transporte.
func decodeCuaWindows(raw []byte) ([]cuaWindow, error) {
	var flat struct {
		Windows    []cuaWindow `json:"windows"`
		Structured *struct {
			Windows []cuaWindow `json:"windows"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, fmt.Errorf("cua: unreadable window list: %w", err)
	}
	if len(flat.Windows) > 0 {
		return flat.Windows, nil
	}
	if flat.Structured != nil {
		return flat.Structured.Windows, nil
	}
	return nil, nil
}

// cuaState es la respuesta de get_window_state: arbol Y captura a la vez.
type cuaState struct {
	PID            int           `json:"pid"`
	SnapshotID     string        `json:"snapshot_id"`
	Elements       []cuaElement  `json:"elements"`
	DegradedReason string        `json:"degraded_reason"`
	ScreenshotB64  string        `json:"screenshot_png_b64"`
	Structured     *cuaStateBody `json:"structuredContent"`
}

type cuaStateBody struct {
	PID            int          `json:"pid"`
	SnapshotID     string       `json:"snapshot_id"`
	Elements       []cuaElement `json:"elements"`
	DegradedReason string       `json:"degraded_reason"`
	ScreenshotB64  string       `json:"screenshot_png_b64"`
}

type cuaElement struct {
	Index int    `json:"element_index"`
	Token string `json:"element_token"`
	Role  string `json:"role"`
	Label string `json:"label"`
	Value string `json:"value"`
	Depth int    `json:"depth"`
	Frame *struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"w"`
		H float64 `json:"h"`
	} `json:"frame"`
	Enabled *bool `json:"enabled"`
}

func (s *cuaState) normalize() {
	if s.Structured == nil {
		return
	}
	if len(s.Elements) == 0 {
		s.Elements = s.Structured.Elements
	}
	if s.SnapshotID == "" {
		s.SnapshotID = s.Structured.SnapshotID
	}
	if s.PID == 0 {
		s.PID = s.Structured.PID
	}
	if s.ScreenshotB64 == "" {
		s.ScreenshotB64 = s.Structured.ScreenshotB64
	}
	if s.DegradedReason == "" {
		s.DegradedReason = s.Structured.DegradedReason
	}
}

// windowState pide arbol y captura de la ventana de la app.
func (d *Cua) windowState(ctx context.Context, target drivers.Target) (cuaState, error) {
	win, err := d.resolveWindow(ctx, target)
	if err != nil {
		return cuaState{}, err
	}
	raw, err := d.cuaCall(ctx, "get_window_state", map[string]any{
		"pid":       win.PID,
		"window_id": win.WindowID,
	})
	if err != nil {
		return cuaState{}, err
	}
	var state cuaState
	if err := json.Unmarshal(raw, &state); err != nil {
		return cuaState{}, fmt.Errorf("cua: unreadable window state: %w", err)
	}
	state.normalize()
	if state.PID == 0 {
		state.PID = win.PID
	}
	if state.DegradedReason != "" && len(state.Elements) == 0 {
		return state, fmt.Errorf("cua: %s", firstLine(state.DegradedReason))
	}
	return state, nil
}

// Tree devuelve el arbol de accesibilidad de la ventana.
func (d *Cua) Tree(ctx context.Context, target drivers.Target, _ drivers.TreeSpec) (drivers.TreeResult, error) {
	state, err := d.windowState(ctx, target)
	if err != nil {
		return drivers.TreeResult{}, err
	}
	encoded, err := json.Marshal(cuaElementsToNodes(state.Elements))
	if err != nil {
		return drivers.TreeResult{}, err
	}
	return drivers.TreeResult{JSON: encoded}, nil
}

// cuaElementsToNodes traduce al vocabulario de mav.
//
// `identifier` sale del element_token y NO del AXIdentifier, que cua-driver no
// expone. Se rellena igualmente porque `ui tap --id` tiene que poder apuntar a
// algo dentro del mismo run, pero ese valor NO sobrevive al siguiente
// snapshot: la propia herramienta rechaza indices caducados en vez de actuar
// sobre el elemento equivocado.
func cuaElementsToNodes(elements []cuaElement) []map[string]any {
	out := make([]map[string]any, 0, len(elements))
	for _, el := range elements {
		node := map[string]any{
			"identifier": el.Token,
			"label":      el.Label,
			"role":       el.Role,
			"value":      el.Value,
		}
		if el.Enabled != nil {
			node["enabled"] = *el.Enabled
		}
		if el.Frame != nil {
			node["frame"] = fmt.Sprintf("{{%g, %g}, {%g, %g}}", el.Frame.X, el.Frame.Y, el.Frame.W, el.Frame.H)
		}
		out = append(out, node)
	}
	return out
}

// Screenshot escribe la captura de la ventana.
//
// Sale del mismo get_window_state que el arbol, en base64, asi que imagen y
// arbol describen el MISMO instante -- que para una evidencia que acompana a un
// arbol es justo lo que se quiere -- y no hay una segunda invocacion que pueda
// pillar la pantalla ya cambiada.
func (d *Cua) Screenshot(ctx context.Context, target drivers.Target, spec drivers.ScreenshotSpec) error {
	if spec.OutPath == "" {
		return errors.New("cua: screenshot output path missing")
	}
	state, err := d.windowState(ctx, target)
	if err != nil {
		return err
	}
	if state.ScreenshotB64 == "" {
		// La herramienta omite la imagen a proposito cuando no puede probar
		// que corresponde a las medidas pedidas, en vez de entregar una
		// transformacion adivinada. Se propaga tal cual: una captura que no se
		// puede probar no es evidencia.
		return errors.New("cua: no provable screenshot for this window")
	}
	png, err := base64.StdEncoding.DecodeString(state.ScreenshotB64)
	if err != nil {
		return fmt.Errorf("cua: unreadable screenshot: %w", err)
	}
	return os.WriteFile(spec.OutPath, png, 0o644)
}

// findCuaElement localiza el elemento del selector dentro de un snapshot.
func findCuaElement(state cuaState, selector drivers.ElementSelector) (cuaElement, bool) {
	for _, el := range state.Elements {
		if selector.ID != "" && el.Token == selector.ID {
			return el, true
		}
		if selector.Text != "" && strings.Contains(el.Label, selector.Text) {
			return el, true
		}
	}
	return cuaElement{}, false
}

// Tap pulsa sin traer la app al frente.
//
// Son dos llamadas y no una a proposito: la herramienta exige un snapshot
// fresco antes de cada accion por elemento e invalida el mapa de indices en
// cuanto haces otro. Reusar un snapshot viejo es exactamente como se acaba
// pulsando lo que no era.
func (d *Cua) Tap(ctx context.Context, target drivers.Target, spec drivers.TapSpec) (drivers.TapResult, error) {
	state, err := d.windowState(ctx, target)
	if err != nil {
		return drivers.TapResult{}, err
	}
	args := map[string]any{"pid": state.PID}
	switch {
	case spec.Selector.ID != "" || spec.Selector.Text != "":
		el, ok := findCuaElement(state, spec.Selector)
		if !ok {
			return drivers.TapResult{}, errors.New("cua: no element matched the selector")
		}
		args["element_token"] = el.Token
	case spec.X != 0 || spec.Y != 0:
		args["x"], args["y"] = spec.X, spec.Y
	default:
		return drivers.TapResult{}, errors.New("cua: tap requires a selector or coordinates")
	}
	if _, err := d.cuaCall(ctx, "click", args); err != nil {
		return drivers.TapResult{}, err
	}
	return drivers.TapResult{MatchedID: spec.Selector.ID, MatchedText: spec.Selector.Text}, nil
}

// Type escribe en el elemento del selector.
func (d *Cua) Type(ctx context.Context, target drivers.Target, spec drivers.TextSpec) error {
	if spec.Text == "" {
		return errors.New("cua: type text missing")
	}
	state, err := d.windowState(ctx, target)
	if err != nil {
		return err
	}
	args := map[string]any{"pid": state.PID, "text": spec.Text}
	if spec.Selector.ID != "" || spec.Selector.Text != "" {
		el, ok := findCuaElement(state, spec.Selector)
		if !ok {
			return errors.New("cua: no element matched the selector")
		}
		args["element_token"] = el.Token
	}
	_, err = d.cuaCall(ctx, "type_text", args)
	return err
}
