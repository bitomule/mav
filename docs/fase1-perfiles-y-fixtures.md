# Fase 1 — Perfiles de plataforma y fixtures

Spec ejecutable. Deriva de [`macos-scope-evaluation.md`](./macos-scope-evaluation.md) §4, cuyas decisiones
ya están cerradas. Este documento las convierte en tareas con criterio de aceptación. Versión objetivo:
`v0.12.0`.

## Por qué esta fase va primera

Es la única con cambio de formato de config, y sin ella no hay macOS posible: **Nokoru necesita
`//App:NokoruiOS` y `//App:NokoruMac` desde el mismo repo, y ambos usan `app_info.bundle_id_debug`**
(`App/BUILD.bazel:118` y `:178`), así que el campo `bundle_id` no los distingue. Un `.mav/config.yaml` sólo
admite hoy un `app_target`, un `bundle_id` y una `launch.commands`.

Los fixtures entran en la misma fase porque comparten el mismo punto de la receta de lanzamiento y porque el
caso de uso que motivó todo esto —validar UI sobre estado conocido, sin grabar audio— los necesita.

## Decisiones ya cerradas

| Eje | Decisión |
|---|---|
| Multi-target | Perfiles por plataforma **dentro** de `.mav/config.yaml` |
| Fixtures | Estados **con nombre**, elegibles por run. Son comandos, no un formato de datos |
| Momento | Tras `install`, antes de `launch` |
| Relación con `launch.commands` | Lo **complementan**, no lo reemplazan |
| `--clear-state` | Borra contenedor → el fixture siembra encima |
| Plataformas | iOS y macOS a la vez |

## Diseño

### Perfiles: overlay, no sustitución

Los campos planos de hoy siguen siendo **la base**. Un perfil es una capa opcional encima. Un repo de una
sola plataforma nunca escribe un perfil y no cambia nada para él.

Esto **no es un shim de compatibilidad** —que la regla del proyecto prohíbe— sino el caso base del modelo:
la resolución es siempre "base, y encima el overlay si hay perfil seleccionado". No hay dos rutas de código
ni migración de ficheros existentes.

```yaml
# base: lo que ya existe, sin tocar
app_target: "//App:NokoruiOS"
bundle_id: com.davidcollado.nokoru.debug
target_command: simpool lease --device "iPhone 17 Pro" --os 26.3
launch:
  mode: custom
  commands:
    build: "bazelisk build '//App:NokoruiOS'"
    app_path: "./scripts/mav-app-path.sh"
    install: "xcrun simctl install \"$MAV_UDID\" \"$MAV_APP_PATH\""
    launch: "xcrun simctl launch \"$MAV_UDID\" \"$MAV_BUNDLE_ID\""

default_profile: ios          # opcional; sin él, la base se usa tal cual

profiles:
  macos:
    # OJO: `target_kind: macos` NO va en la Fase 1. Ver "Trampa de orden" abajo.
    app_target: "//App:NokoruMac"
    launch:
      commands:
        build: "bazelisk build '//App:NokoruMac'"
        app_path: "./scripts/mav-app-path-mac.sh"
        install: ""           # vacío explícito: en macOS no hay simctl install
        launch: "\"$MAV_APP_PATH/Contents/MacOS/Nokoru\""

fixtures:
  seeded-meetings:
    - "./scripts/mav-fixtures/seed-meetings.sh"
  empty:
    - "./scripts/mav-fixtures/wipe.sh"
```

**Precedencia de selección de perfil**, en el mismo estilo que la tabla de targets del README:

1. `--profile <nombre>` explícito
2. `MAV_PROFILE` en el entorno
3. `default_profile` en la config
4. Ninguno → los campos base tal cual

Un `--profile` que no existe **falla** (`profile_not_found`, listando los definidos). Silenciar eso repetiría
el fallo que `target_command_ignored` existe para evitar.

**Interacción con `MAV_TARGET_KIND`**: `LoadConfig` (`config.go:108-117`) deja que esa variable pise
`TargetKind` y los campos de simulador — es lo que usan los hijos de `run --matrix`. Hay que fijar el orden
respecto al overlay del perfil, porque la combinación existe: `default_profile: macos` más un hijo de matrix
con `MAV_TARGET_KIND=simulator` dejaría `app_target=//App:NokoruMac` con kind de simulador, es decir
construyendo la app de Mac para instalarla en un simulador. Propuesta: el env de target gana sobre el kind,
y `open` detecta la incoherencia kind/app_target y **falla** en vez de seguir.

**Regla de merge**: campo a campo, el perfil gana cuando está *presente*. Una cadena vacía explícita en el
perfil (como `install: ""` arriba) es presencia, y significa "sin comando" — no "hereda el de la base".

⚠️ **Pero "sin comando" no basta para saltarse el install**, y esto obliga a tocar más código del que parecía:
`runLaunchRecipe` entra al bloque de install si `Install != "" || appPath != ""` (`launch.go:69`), y el perfil
define `app_path`, así que entra igual. Con el comando vacío, `shouldUseDriverInstall` (`launch.go:112`)
devuelve `true` —`command == ""` es su primer caso— y encamina `CapInstall` por el router. O sea que un
`install: ""` acaba instalando por driver, que es justo lo contrario de lo que expresa.

**Semántica de presencia con los structs actuales**: `LaunchCommands` (`config.go:53-60`) son strings planos,
y `yaml.Unmarshal` no distingue "ausente" de `""`. Los perfiles necesitan un esquema paralelo con punteros o
`yaml.Node`. Eso es parte del coste real de T1, no un detalle.

**Campos sobreescribibles por un perfil** — hay que cerrar la lista, no dejarla abierta, o el primer campo no
contemplado se ignorará sin ruido:

| Campo | Por qué lo necesita Nokoru |
|---|---|
| `app_target` | `//App:NokoruiOS` vs `//App:NokoruMac` |
| `launch.commands.*` | Recetas distintas por plataforma |
| `process_name` | El binario de NokoruMac no se llama igual que el de iOS; hace falta para logs, crashes y para el terminate previo al fixture |
| `target_command` | Un `simpool lease` de simulador no pinta nada en un run de macOS |
| `log_subsystem` / `log_category` | Pueden divergir por plataforma |

Un test debe fallar cuando `configYAML` gane un campo que el merge de perfiles no contemple.

### Trampa de orden: `target_kind: macos` no puede entrar en esta fase

`LoadConfig` sólo rellena `target_kind` cuando viene vacío (`config.go`: `if cfg.TargetKind == "" { cfg.TargetKind = "simulator" }`), y `targetKind()` devuelve `KindDevice` únicamente cuando el valor es exactamente `"device"`; **cualquier otra cosa cae en `KindSim`**. Es decir: un `target_kind: macos` en un perfil de la Fase 1 no falla — se comporta como simulador, en silencio.

Escenario concreto: `mav open --profile macos` construye `//App:NokoruMac`, y acto seguido `resolveConfigTarget` corre el `target_command` de la base (`simpool lease --device "iPhone 17 Pro"`) — **alquila y arranca un iPhone simulado para un run de macOS**, toma el sim-lock, y `launchEnv` exporta ese `MAV_UDID`. Después `shouldUseDriverInstall` encamina a simctl y la app de Mac entra por la ruta de instalación del simulador.

Y el peor caso, que es de pérdida de datos: con `--clear-state`, `runLaunchRecipe` (`launch.go:35-37`) encamina `CapUninstall` contra ese simulador usando el `bundle_id` **compartido entre las dos plataformas** — es decir, **desinstala la app de iOS del simulador durante un run "de macOS"**. El resultado se descarta con `_ =` (`launch.go:36`), así que no aparece ni en el error ni en la evidencia.

Dos consecuencias para el alcance de esta fase:

1. **La Fase 1 no incluye `target_kind: macos`.** El eje de plataforma llega en la Fase 2 con `drivers.KindMac`.
2. **La validación end-to-end de esta fase es sólo con perfiles de iOS.** Lo que se demuestra aquí es que los perfiles y los fixtures funcionan sin romper el camino existente, no que macOS arranque.

Y una tarea que se gana: mientras `KindMac` no exista, un `target_kind` desconocido debe **fallar ruidosamente** en vez de degradar a simulador. Es un fallo latente que ya existe hoy, no lo introducen los perfiles — pero los perfiles lo vuelven fácil de pisar.

### Fixtures

Un fixture es una lista de comandos con nombre. Se ejecutan por el mismo camino que `launch.commands`
—es decir `runLaunchCommand`, que usa **`/bin/sh -lc`** (`launch.go:169`), no bash: `target_command` y los
pasos `exec` de los flows sí usan `/bin/bash -lc`, y esa asimetría ya existe hoy— con el entorno `MAV_*`
completo de `launchEnv`, en orden, parando al primer fallo. Un fixture con bashisms falla; documentarlo.

Selección: `mav open --fixture <nombre>`, y campo `fixture:` en el paso `open` de un flow. Sin `--fixture`,
no corre ninguno.

**Orden completo de la receta con todo activo:**

```
clear_state (si --clear-state)  →  healthcheck  →  build  →  app_path  →  install  →  FIXTURE  →  launch  →  cleanup
```

La ventana entre `install` y `launch` es la única correcta: el contenedor ya existe (lo crea el install) y la
app aún no ha arrancado, así que nada tiene el sqlite abierto.

**`--fixture` es incompatible con `--no-relaunch`.** `--no-relaunch` se salta la receta entera
(`cli.go:1069`, `if !noRelaunch { ... }`), así que un fixture nunca llegaría a correr. Aceptar el flag y no
ejecutarlo es configuración muerta. El repo ya tiene la respuesta correcta para exactamente esta forma:
`cli.go:1013` rechaza `--no-relaunch` junto a `--clear-state` con `open_flags_invalid`. Mismo trato.

## Puntos de código

| Fichero | Qué cambia |
|---|---|
| `internal/mav/config.go` — `configYAML` (~línea 121) | Campos `default_profile`, `profiles`, `fixtures` |
| `internal/mav/config.go` — `LoadConfig` | Resolver perfil y aplicar overlay tras cargar la base |
| `internal/mav/config.go` — `SaveConfig` (~línea 158) | ⚠️ **Escribe YAML a mano con un `strings.Builder`, no con `yaml.Marshal`.** Hay que extender el escritor o `mav setup` borrará perfiles y fixtures al reescribir la config |
| `internal/mav/launch.go` — `runLaunchRecipe` (~línea 88) | Insertar el paso de fixture entre el bloque `install` y el bloque `launch` |
| `internal/mav/launch.go` — `effectiveLaunchCommands` | Es el punto de merge que ya existe para device; el overlay de perfil se aplica antes |
| `internal/mav/cli.go` — parseo de flags globales (~línea 128) | `--profile`; `--fixture` en `open` |
| `internal/mav/flow.go` — paso `open` | Campo `fixture:` |

## Tareas

Cada una debe dejar `make check` en verde y ser un commit propio.

**T1 — Esquema de perfiles en config (lectura)**
Añadir `default_profile` y `profiles` a `configYAML` y a `Config`; implementar la resolución de precedencia y
el merge campo a campo.
*Aceptación*: test que carga una config con perfil `macos` y afirma que `app_target` y `launch.commands.build`
salen los del perfil y el resto de la base. Test de que `install: ""` en el perfil anula el de la base. Test
de que `--profile` inexistente devuelve `profile_not_found` listando los válidos. Test de que una config
**sin** `profiles` se comporta byte a byte como hoy.

**T2 — Sustituir el escritor artesanal de config por `yaml.Marshal`**
No extender el `strings.Builder` de `SaveConfig`: sustituirlo. Extender un serializador YAML hecho a mano a
mapas anidados de structs con semántica presente/ausente es reimplementar `yaml.Marshal` peor, y `yaml.v3` ya
es la única dependencia del proyecto.

Hay un motivo concreto además del estético: `writeCommandKV` (`config.go:226-229`) **omite los valores
vacíos**. Si el escritor de perfiles reutiliza ese patrón, el round-trip se come el `install: ""` del perfil y
tras un `mav setup` el perfil de macOS vuelve a heredar el `simctl install` de la base, en silencio. El
escritor destruiría exactamente la semántica de presencia que T1 necesita.

*Aceptación*: round-trip `LoadConfig(SaveConfig(cfg)) == cfg` que **incluya explícitamente un override de
cadena vacía** — sin ese caso el test pasa y el bug sobrevive. Test de que una config existente sin perfiles
se reescribe igual que hoy.

**T3 — Flag `--profile` y `MAV_PROFILE`**
Cablear la selección en el parseo de flags globales y en el entorno.
*Aceptación*: test de la precedencia completa (flag > env > `default_profile` > base). El perfil resuelto
aparece como campo `profile=` en la salida de los comandos, igual que `target_kind`.

**T4 — Esquema de fixtures y ejecución**
`fixtures` en config, `--fixture` en `open`, y el paso nuevo en `runLaunchRecipe`.
*Aceptación*: test de que los comandos corren **después** de install y **antes** de launch (verificable por
el orden en `commands.jsonl`). Test de que un fixture que falla aborta el `open` con el nombre del fixture y
el comando que petó. Test de que sin `--fixture` no corre nada. Test de que `--fixture` junto a
`--no-relaunch` devuelve `open_flags_invalid`. Y el nombre del fixture aplicado sale como campo en la línea
`ok` de `open`, no sólo en el trail.

**T5 — `fixture:` en el paso `open` de un flow**
Campo nuevo junto a `ClearState` en el struct de paso (`internal/mav/flow.go:138`). Una sola grafía,
`fixture:` — no repetir el doblete `clearState`/`clear-state` que ya arrastra ese struct.
*Aceptación*: flow de ejemplo en `testdata` que abre con fixture, y test que lo ejecuta.

**T6 — `--clear-state` compone con fixture**
Verificar que el orden es clear_state → … → install → fixture → launch.
*Aceptación*: test que activa ambos y afirma el orden en `commands.jsonl`.

**T7 — Terminar la app antes del fixture**
Nada en el camino de `open` mata la app: el `stop` del run anterior sólo mata procesos propios de mav (log
streams, vídeo, worker). Escenario: el run 1 lanzó la app; el run 2 hace `open --fixture seeded-meetings` y el
fixture escribe el GRDB **mientras la instancia vieja lo tiene abierto** — el WAL pisa la siembra o la
corrompe. Y como el launch de macOS es ejecución directa del binario, sin `open -n` ni singleton, acabas
además con una segunda instancia.
*Aceptación*: `CapTerminate` sobre `process_name` antes del paso de fixture; test del orden en
`commands.jsonl`. Depende de que `process_name` sea sobreescribible por perfil (ver tabla de campos).

**T8 — El fixture aparece en la evidencia del run**
El struct `Report` (`internal/mav/evidence.go:43`) no registra hoy nada de la receta de lanzamiento — ni
siquiera si hubo `--clear-state`. Un run cuyo estado lo sembró un fixture, y cuyo `report.json` no dice cuál,
no es reproducible desde su propia evidencia. Y la evidencia es la razón de existir de MAV.
*Aceptación*: `report.json` incluye el nombre del fixture aplicado (y si hubo `clear_state`); test que lo
afirma. Los comandos del fixture ya salen en `commands.jsonl` por usar el camino de `runLaunchCommand`,
pero el manifiesto verificado tiene que poder responder "¿de qué estado partió esto?" sin leer el trail.

**T9 — `target_kind` desconocido falla ruidosamente**
Mientras `KindMac` no exista (Fase 2), un `target_kind` que no sea `simulator` ni `device` debe ser un error
de config, no una degradación silenciosa a simulador. Ver "Trampa de orden" arriba.
*Aceptación*: test de que `target_kind: macos` devuelve un fallo de config nombrando los valores válidos.
Test de que `simulator`, `device` y vacío siguen comportándose igual que hoy.

**T10 — Documentación**
README (sección de config y de launch recipes) y `skills/mav/SKILL.md`. La skill es lo que leen los agentes:
si `--fixture` no está ahí, no existe.
*Aceptación*: `mav --help` y `mav open --help` mencionan ambos flags.

## Validación end-to-end

El unit-test no prueba que esto sirva. La prueba real es contra Nokoru, que ya tiene `.mav/config.yaml`:

1. Añadir un perfil `macos` y un fixture `seeded-meetings` a `~/Projects/Nokoru/.mav/config.yaml`.
2. El script del fixture siembra el GRDB y unos `.m4a` bajo
   `~/Library/Containers/com.davidcollado.nokoru.debug/Data/Library/Application Support/Nokoru/`
   (rutas verificadas en `App/Shared/NokoruPaths.swift`: `nokoruDirectory`, `recordingsDirectory`,
   `audioURL(for:)`).
3. `mav open --profile ios --fixture seeded-meetings` — debe seguir funcionando en simulador, con la app
   arrancando con datos ya presentes. Es la prueba de que perfiles y fixtures funcionan sin romper el camino
   existente.
4. `mav open` sin `--profile` ni `--fixture` — debe comportarse **exactamente** como antes de esta fase. Es
   la prueba de no-regresión que protege a Undolly, Boxy y HiddenFace, que no van a tener perfiles.
5. El perfil de macOS **no** se valida en esta fase: sin `drivers.KindMac` no hay a dónde despachar (ver
   "Trampa de orden"). Llega en la Fase 2.

## Fuera de alcance

- `drivers.KindMac` y el modelo de target macOS → Fase 2. Con él llega también `target_kind: macos` en los
  perfiles, que en esta fase queda deliberadamente fuera.
- Cualquier driver de macOS → Fase 3.
- Formato de fixture como datos (bundles, manifiestos). Un fixture es una lista de comandos; si tres apps
  acaban escribiendo el mismo script, entonces se promociona.
- Migrar los `.mav/config.yaml` existentes. No hace falta: sin `profiles`, el comportamiento es idéntico.
