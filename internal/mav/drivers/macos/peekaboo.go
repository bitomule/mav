package macos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// PeekabooID es la clave de registro del driver.
const PeekabooID = "peekaboo"

// Peekaboo envuelve el CLI de Peekaboo, que es al Mac lo que AXe es al
// simulador: arbol de accesibilidad, taps semanticos, y ademas menus y
// ventanas, que en iOS no existen.
//
// Lo que NO hace bien, y por eso no viene solo: su input activa la app de
// destino (ensureFocused antes de cada click, saltando de Space si hace falta).
// Existe --no-auto-focus, pero eso no lo arregla -- solo evita robar el foco,
// y entonces el evento va a lo que este delante. Para input de verdad en
// segundo plano hace falta entrega por PID, que es lo que aporta axcli.
type Peekaboo struct {
	exec drivers.Executor
}

var (
	_ drivers.TreeDriver       = (*Peekaboo)(nil)
	_ drivers.TapDriver        = (*Peekaboo)(nil)
	_ drivers.TypeDriver       = (*Peekaboo)(nil)
	_ drivers.ScreenshotDriver = (*Peekaboo)(nil)
)

// NewPeekaboo construye el driver.
func NewPeekaboo(exec drivers.Executor) *Peekaboo { return &Peekaboo{exec: exec} }

func (d *Peekaboo) ID() string { return PeekabooID }

func (d *Peekaboo) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.Kind != drivers.KindMac {
		return drivers.NewSet()
	}
	// Peekaboo NO declara capacidades de input, y esto no es una omision.
	//
	// Su click activa la app de destino y dispara por coordenadas resueltas de
	// un snapshot. Si ese snapshot ya no describe la pantalla -- porque la
	// ventana se movio, porque otra app se puso delante, o porque el snapshot
	// vigente es otro -- el click aterriza en lo que haya ahi. En una prueba
	// real eso abrio el correo del usuario en vez de pulsar un boton de la app
	// bajo prueba.
	//
	// Un tap que puede acertarle a otra aplicacion es peor que no tener tap:
	// el segundo falla y se arregla, el primero hace algo que nadie pidio y
	// puede no notarse hasta mucho despues. Asi que si no hay un driver que
	// entregue por PID -- axcli -- el router devuelve ErrNoDriver y `ui tap`
	// falla diciendo que falta, en vez de caer a un camino que puede pulsar
	// cualquier cosa.
	return drivers.NewSet(
		drivers.CapTreeAX,
		drivers.CapScreenshot,
	)
}

// Cost reparte el trabajo con axcli sin necesidad de un rasgo nuevo en la
// interfaz: Cost ya es por capacidad Y por target, que son los dos ejes que
// hacen falta.
//
// Peekaboo es canonico en lo que solo el sabe hacer (arbol, y capturas
// acotadas a una ventana) y deliberadamente caro en input, porque el suyo
// activa la app. Un driver que entregue por PID sin robar el foco le gana ahi
// por coste, y el reparto sale solo.
func (d *Peekaboo) Cost(c drivers.Capability, _ drivers.Target) int {
	switch c {
	case drivers.CapTreeAX:
		return 0
	case drivers.CapScreenshot:
		// Acotada a la ventana de la app: mejor evidencia que la pantalla
		// entera de screencapture, que declara 50.
		return 0
	case drivers.CapSemanticTap, drivers.CapCoordTap, drivers.CapType:
		// No se declaran en Provides (ver arriba); el coste queda por si
		// alguien las reintroduce, para que nunca sean el camino barato.
		return 100
	default:
		return 100
	}
}

// Probe comprueba el binario Y los permisos, que en macOS son la mitad del
// problema. `peekaboo permissions --json` los reporta sin efectos secundarios.
func (d *Peekaboo) Probe(ctx context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("peekaboo")
	if err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthMissing,
			Detail: "peekaboo not on PATH",
			Next:   "mav setup --install peekaboo",
		}
	}
	report := drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"peekaboo": path}}
	res := d.exec.Run(ctx, "peekaboo", "permissions", "--json")
	missing := missingPermissions(res.Stdout)
	if len(missing) > 0 {
		// Degradado y no roto a proposito: el permiso pertenece al proceso
		// padre (la terminal o el harness del agente), asi que puede
		// concederse sin reinstalar nada, y el mensaje tiene que decir a quien
		// hay que darselo.
		report.State = drivers.HealthDegraded
		report.Detail = "missing TCC permission: " + strings.Join(missing, ", ")
		report.Next = "grant it to the terminal or agent harness that runs mav, not to mav itself: System Settings > Privacy & Security"
	}
	return report
}

func (d *Peekaboo) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// peekabooEnvelope es la forma comun de toda salida --json de Peekaboo.
type peekabooEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// decodeEnvelope interpreta la salida de Peekaboo.
//
// Ojo con dos cosas que no son evidentes: los errores salen por STDOUT, no por
// stderr, y el exit code es 1 para todos por igual, asi que el codigo real
// solo esta dentro del JSON. Un wrapper que mire el exit code o stderr se
// queda sin saber que paso.
func decodeEnvelope(stdout string) (*peekabooEnvelope, error) {
	var env peekabooEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		return nil, fmt.Errorf("peekaboo: unreadable output: %s", firstLine(stdout))
	}
	if !env.Success {
		if env.Error != nil {
			return nil, fmt.Errorf("%s: %s", env.Error.Code, firstLine(env.Error.Message))
		}
		return nil, errors.New("peekaboo: command failed without an error code")
	}
	return &env, nil
}

func missingPermissions(stdout string) []string {
	env, err := decodeEnvelope(stdout)
	if err != nil {
		return nil
	}
	var payload struct {
		Permissions []struct {
			Name      string `json:"name"`
			IsGranted bool   `json:"isGranted"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(env.Data, &payload); err != nil {
		return nil
	}
	var missing []string
	for _, perm := range payload.Permissions {
		if !perm.IsGranted {
			missing = append(missing, perm.Name)
		}
	}
	return missing
}

// appArgs apunta a la app bajo prueba. Peekaboo acepta nombre, bundle id o
// PID:<n>; el bundle id es lo unico que mav tiene siempre.
func appArgs(target drivers.Target) []string {
	if target.PID > 0 {
		return []string{"--app", "PID:" + strconv.Itoa(target.PID)}
	}
	if target.BundleID != "" {
		return []string{"--app", target.BundleID}
	}
	if target.Name != "" && target.Name != "localhost" {
		return []string{"--app", target.Name}
	}
	return nil
}

// seeElement es un elemento tal y como lo emite `see --json`.
//
// Los campos de geometria y estado son punteros/omitibles a proposito: en
// 3.0.0-beta3 el elemento de stdout NO trae bounds, is_enabled ni focus -- eso
// vive en el fichero de snapshot -- mientras que en main ya salen inline.
// Aceptar las dos formas evita que actualizar Peekaboo rompa el driver.
type seeElement struct {
	ID              string `json:"id"`
	Identifier      string `json:"identifier"`
	Role            string `json:"role"`
	RoleDescription string `json:"role_description"`
	Label           string `json:"label"`
	Description     string `json:"description"`
	Title           string `json:"title"`
	Value           string `json:"value"`
	IsActionable    bool   `json:"is_actionable"`
	IsEnabled       *bool  `json:"is_enabled"`
	Bounds          *struct {
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"bounds"`
}

// Tree pide el arbol y lo traduce al vocabulario que ExtractElements entiende.
//
// Limitacion heredada que conviene conocer: los identificadores de elemento de
// Peekaboo (elem_N) pertenecen a un snapshot, y el snapshot "actual" es estado
// compartido en ~/.peekaboo. Dos runs de mav conduciendo macOS a la vez en la
// misma maquina se pisan. Es una limitacion de la herramienta, no de mav, y se
// acepta antes que reimplementar su gestion de snapshots.
func (d *Peekaboo) Tree(ctx context.Context, target drivers.Target, _ drivers.TreeSpec) (drivers.TreeResult, error) {
	args := append([]string{"see"}, appArgs(target)...)
	args = append(args, "--json")
	res := d.exec.Run(ctx, "peekaboo", args...)
	env, err := decodeEnvelope(res.Stdout)
	if err != nil {
		return drivers.TreeResult{}, err
	}
	var payload struct {
		Elements []seeElement `json:"ui_elements"`
	}
	if err := json.Unmarshal(env.Data, &payload); err != nil {
		return drivers.TreeResult{}, fmt.Errorf("peekaboo: unreadable tree: %w", err)
	}
	out := make([]map[string]any, 0, len(payload.Elements))
	for _, el := range payload.Elements {
		node := map[string]any{
			// El identificador AX va primero porque es el estable entre runs,
			// que es lo que un agente espera de `--id`. elem_N solo vale
			// dentro del snapshot que lo genero.
			"identifier": firstNonEmptyString(el.Identifier, el.ID),
			// El texto util esta en `description`, no en `label`: peekaboo
			// rellena `label` con el rol localizado cuando el elemento no
			// tiene titulo propio, asi que un arbol entero sale diciendo
			// "boton" en el idioma del sistema. `description` es donde vive lo
			// que un agente necesita leer ("Soporte", "Ajustes", el texto de
			// la fila...).
			"label": firstNonEmptyString(el.Description, el.Title, usefulLabel(el)),
			// El rol estable en ingles, NO role_description, que esta
			// localizado: un selector escrito contra "botón" deja de funcionar
			// en cuanto cambia el idioma del sistema.
			"role":  el.Role,
			"title": el.Title,
			"value": el.Value,
		}
		if el.IsEnabled != nil {
			node["enabled"] = *el.IsEnabled
		}
		if el.Bounds != nil {
			node["frame"] = fmt.Sprintf("{{%g, %g}, {%g, %g}}", el.Bounds.X, el.Bounds.Y, el.Bounds.Width, el.Bounds.Height)
		}
		out = append(out, node)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return drivers.TreeResult{}, err
	}
	return drivers.TreeResult{JSON: encoded}, nil
}

// Tap pulsa por identificador, por texto, o por coordenadas.
func (d *Peekaboo) Tap(ctx context.Context, target drivers.Target, spec drivers.TapSpec) (drivers.TapResult, error) {
	args := append([]string{"click"}, appArgs(target)...)
	switch {
	case spec.Selector.ID != "":
		args = append(args, "--on", spec.Selector.ID)
	case spec.Selector.Text != "":
		args = append(args, spec.Selector.Text)
	case spec.X != 0 || spec.Y != 0:
		args = append(args, "--coords", strconv.Itoa(spec.X)+","+strconv.Itoa(spec.Y))
	default:
		return drivers.TapResult{}, errors.New("peekaboo: tap requires an id, text or coordinates")
	}
	args = append(args, "--json")
	res := d.exec.Run(ctx, "peekaboo", args...)
	if _, err := decodeEnvelope(res.Stdout); err != nil {
		return drivers.TapResult{}, err
	}
	return drivers.TapResult{
		MatchedID:   spec.Selector.ID,
		MatchedText: spec.Selector.Text,
		X:           spec.X,
		Y:           spec.Y,
	}, nil
}

// Type escribe en el elemento con foco. Peekaboo no sabe escribir en uno
// concreto -- su propio help dice que hay que enfocarlo antes con click -- asi
// que un selector aqui seria una promesa que no puede cumplir.
func (d *Peekaboo) Type(ctx context.Context, target drivers.Target, spec drivers.TextSpec) error {
	if spec.Text == "" {
		return errors.New("peekaboo: type text missing")
	}
	if !spec.Selector.IsZero() {
		return errors.New("peekaboo: typing into a specific element is not supported; tap it first")
	}
	args := append([]string{"type", spec.Text}, appArgs(target)...)
	args = append(args, "--json")
	res := d.exec.Run(ctx, "peekaboo", args...)
	_, err := decodeEnvelope(res.Stdout)
	return err
}

// Screenshot captura la ventana de la app, no la pantalla entera.
//
// Usa `see --path` y no `image`: ese subcomando se elimino en Peekaboo v4, y
// una version instalada por brew hoy es v4. Ademas `see` devuelve el arbol y la
// captura en la misma llamada, asi que cuando alguien encadena tree+capture
// -- que es el caso normal en un flow -- se ahorra una invocacion entera, y las
// dos evidencias corresponden al MISMO instante en vez de a dos momentos
// distintos, que para una captura que acompana a un arbol importa.
func (d *Peekaboo) Screenshot(ctx context.Context, target drivers.Target, spec drivers.ScreenshotSpec) error {
	if spec.OutPath == "" {
		return errors.New("peekaboo: screenshot output path missing")
	}
	args := append([]string{"see"}, appArgs(target)...)
	args = append(args, "--mode", "window", "--path", spec.OutPath, "--json")
	res := d.exec.Run(ctx, "peekaboo", args...)
	_, err := decodeEnvelope(res.Stdout)
	return err
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// usefulLabel devuelve el `label` de peekaboo solo cuando aporta algo. Cuando
// coincide con role_description es que el elemento no tenia titulo y peekaboo
// puso ahi el rol localizado, que como etiqueta es ruido.
func usefulLabel(el seeElement) string {
	if el.Label == el.RoleDescription {
		return ""
	}
	return el.Label
}
