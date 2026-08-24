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

**Regla de merge**: campo a campo, el perfil gana cuando está *presente*. Una cadena vacía explícita en el
perfil (como `install: ""` arriba) es presencia, y significa "sin comando" — no "hereda el de la base". Es la
diferencia que permite a macOS anular el `simctl install` heredado.

### Trampa de orden: `target_kind: macos` no puede entrar en esta fase

`LoadConfig` sólo rellena `target_kind` cuando viene vacío (`config.go`: `if cfg.TargetKind == "" { cfg.TargetKind = "simulator" }`), y `targetKind()` devuelve `KindDevice` únicamente cuando el valor es exactamente `"device"`; **cualquier otra cosa cae en `KindSim`**. Es decir: un `target_kind: macos` en un perfil de la Fase 1 no falla — se comporta como simulador, en silencio.

Escenario concreto: `mav open --profile macos` construye `//App:NokoruMac`, y acto seguido `launchEnv` exporta un `MAV_UDID` de simulador y `shouldUseDriverInstall` encamina a simctl. La app de Mac acaba pasando por la ruta de instalación del simulador y el fallo aparece dos capas más abajo, sin relación aparente con la causa.

Dos consecuencias para el alcance de esta fase:

1. **La Fase 1 no incluye `target_kind: macos`.** El eje de plataforma llega en la Fase 2 con `drivers.KindMac`.
2. **La validación end-to-end de esta fase es sólo con perfiles de iOS.** Lo que se demuestra aquí es que los perfiles y los fixtures funcionan sin romper el camino existente, no que macOS arranque.

Y una tarea que se gana: mientras `KindMac` no exista, un `target_kind` desconocido debe **fallar ruidosamente** en vez de degradar a simulador. Es un fallo latente que ya existe hoy, no lo introducen los perfiles — pero los perfiles lo vuelven fácil de pisar.

### Fixtures

Un fixture es una lista de comandos con nombre. Se ejecutan por el mismo camino que `launch.commands`
(`/bin/bash -lc`, con el entorno `MAV_*` completo de `launchEnv`), en orden, parando al primer fallo.

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

**T2 — Perfiles en el escritor de config**
Extender `SaveConfig` para que serialice `default_profile`, `profiles` y `fixtures`.
*Aceptación*: test round-trip — `LoadConfig(SaveConfig(cfg)) == cfg` con perfiles y fixtures poblados. Sin
esto, `mav setup` sobre un repo con perfiles los destruye en silencio, que es el fallo más caro de esta fase.

**T3 — Flag `--profile` y `MAV_PROFILE`**
Cablear la selección en el parseo de flags globales y en el entorno.
*Aceptación*: test de la precedencia completa (flag > env > `default_profile` > base). El perfil resuelto
aparece como campo `profile=` en la salida de los comandos, igual que `target_kind`.

**T4 — Esquema de fixtures y ejecución**
`fixtures` en config, `--fixture` en `open`, y el paso nuevo en `runLaunchRecipe`.
*Aceptación*: test de que los comandos corren **después** de install y **antes** de launch (verificable por
el orden en `commands.jsonl`). Test de que un fixture que falla aborta el `open` con el nombre del fixture y
el comando que petó. Test de que sin `--fixture` no corre nada.

**T5 — `fixture:` en el paso `open` de un flow**
Campo nuevo junto a `ClearState` en el struct de paso (`internal/mav/flow.go:138`). Una sola grafía,
`fixture:` — no repetir el doblete `clearState`/`clear-state` que ya arrastra ese struct.
*Aceptación*: flow de ejemplo en `testdata` que abre con fixture, y test que lo ejecuta.

**T6 — `--clear-state` compone con fixture**
Verificar que el orden es clear_state → … → install → fixture → launch.
*Aceptación*: test que activa ambos y afirma el orden en `commands.jsonl`.

**T7 — El fixture aparece en la evidencia del run**
El struct `Report` (`internal/mav/evidence.go:43`) no registra hoy nada de la receta de lanzamiento — ni
siquiera si hubo `--clear-state`. Un run cuyo estado lo sembró un fixture, y cuyo `report.json` no dice cuál,
no es reproducible desde su propia evidencia. Y la evidencia es la razón de existir de MAV.
*Aceptación*: `report.json` incluye el nombre del fixture aplicado (y si hubo `clear_state`); test que lo
afirma. Los comandos del fixture ya salen en `commands.jsonl` por usar el camino de `runLaunchCommand`,
pero el manifiesto verificado tiene que poder responder "¿de qué estado partió esto?" sin leer el trail.

**T8 — `target_kind` desconocido falla ruidosamente**
Mientras `KindMac` no exista (Fase 2), un `target_kind` que no sea `simulator` ni `device` debe ser un error
de config, no una degradación silenciosa a simulador. Ver "Trampa de orden" arriba.
*Aceptación*: test de que `target_kind: macos` devuelve un fallo de config nombrando los valores válidos.
Test de que `simulator`, `device` y vacío siguen comportándose igual que hoy.

**T9 — Documentación**
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
