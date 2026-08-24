# ¿Ampliar MAV a apps de macOS? ¿Y renombrarlo?

Evaluación técnica y plan. Fecha: 2026-08-19. Repo en `v0.11.0`.

> **Revisión 4** — la v1 daba el backend macOS por local-only (TCC) y 8 comandos por "sin equivalente"; ambas
> cosas eran pesimistas (§2.4, §2.5). La v2 proponía construir la capa de VM; ya existe y es `crabbox` (§2.6).
> La v3 ordenaba las fases con la VM primero. Esta v4 recoge las decisiones de la sesión de grilling: el
> objetivo es **probar que MAV funciona en macOS**, con `NokoruMac` de banco de pruebas, y el driver local va
> antes que la VM. La recomendación de no renombrar no ha cambiado en ninguna revisión.

---

## Recomendación

**Hacerlo, en este orden: no renombrar, higiene primero (hecha), driver macOS local, y la VM después.**

| Decisión | Veredicto | Por qué en una línea |
|---|---|---|
| Renombrar el binario / repo / módulo | **No** | El coste no está en el binario (1 día) sino en `.mav/`, las 22 vars `MAV_*` que viven en los `launch.commands` de cada repo, y la skill ya instalada — y la regla del proyecto prohíbe shims, así que sería un corte seco por un problema que nadie tiene. |
| Reinterpretar el acrónimo + retocar el tagline | **Sí** | 3 líneas. "MAV" pasa a ser un nombre (como `git` o `curl`), no unas siglas que defender. Cuando se decida el go de macOS, no antes. |
| Backend macOS | **Sí** | ~75% del command surface mapea sin VM, ~85% con VM. Sólo 4 comandos son mobile-only de verdad. Y macOS **añade** 5 capacidades que iOS no tiene. |
| **App de referencia** | **NokoruMac, como banco de pruebas** | El entregable es "MAV funciona en macOS", no "Nokoru está cubierto". Nokoru es la app macOS real más a mano (bazel, sandboxed, ya tiene `.mav/config.yaml` para iOS). Cubrir su funcionalidad está **fuera de alcance**: es lo que impide que esto se deslice a un proyecto de QA de Nokoru. |
| **Criterio de aceptación** | **Bucket A completo** | Los ~14 comandos que mapean 1:1 (§2.4), funcionando contra NokoruMac. Lo descartado es el bucket C: 4 comandos mobile-only (home, hideKeyboard, rotate/twoFingerPan sin API pública, device iOS). |
| VM (Tart) como target | **Sí, pero después del driver** | Sigue siendo lo que devuelve `erase`, `matrix` y `time travel`, porque el papel real del simulador en MAV no es "iOS" sino "dispositivo desechable de propiedad exclusiva". Pero no bloquea el primer entregable: se entra cuando un prompt de TCC rompa un run local. |
| Construir esa capa de VM dentro de MAV | **No — usar `crabbox`** | Lease, sync del checkout sucio, ejecución remota, evidencia de run y release ya existen, en Go, MIT, con un **provider `tart` de primera clase para VMs macOS**. Construirlo rompería el rol de MAV (wrapper sobre drivers + evidencia). Ver §2.6. |
| CI con targets macOS | **Sí, dentro de VM** | Las imágenes base de Tart traen **SIP desactivado y auto-login en una sesión Aqua completa**, así que el provisioning escribe los permisos directamente en `TCC.db`. El bloqueo de TCC es del Mac del desarrollador, no de una VM que tú controlas. |

**Qué hacer primero:**

1. ~~Fase 0 de higiene~~ — **hecha** (PR #53, `v0.11.0`). El router ya decide de verdad.
2. **Perfiles y fixtures** (Fase 1). Es lo único con cambio de formato de config, y sin ello Nokoru no puede
   tener iOS y macOS en el mismo repo.
3. **Modelo de target macOS** (Fase 2). `KindMac`, `AppPath`/`PID`, y `doctor` reportando TCC. Barata y sin
   cambio de comportamiento, pero el driver no puede aterrizar sin ella.
4. **Los dos drivers macOS** (Fase 3). El entregable que prueba la tesis.
5. **VM** (Fase 4), cuando duela.

---

## 1. Cómo está abstraído MAV hoy

### Lo que ya está bien

`internal/mav/drivers/` es un router de capacidades real:

- `Capability` — 35 capacidades declaradas, en `drivers/capability.go`.
- `Driver` + 16 interfaces funcionales (`TapDriver`, `TreeDriver`, `GestureDriver`, `LifecycleDriver`…), en
  `drivers/driver.go`.
- `Registry` con auto-registro y `Router` que decide por `Provides(target)` → filtro de salud (`Probe`) →
  orden por `Cost(cap, target)`, con `ErrNoDriver` estructurado que lista por qué perdió cada candidato.
- 6 drivers conviviendo: `axe`, `simctl`, `idb`, `baguette`, `network` (mitmproxy), `simtime`.
- `Target` (`drivers/target.go`) ya es un struct neutro: `{Kind, UDID, Name, Runtime, BundleID, Locale, Language}`.

Añadir un séptimo driver es, por diseño, un paquete nuevo y una línea en `RegisterDefaultDrivers`.

### Lo que la Fase 0 arregló

El acoplamiento no estaba en `drivers/` sino en la capa de encima, y la mayor parte ya está resuelta en
`v0.11.0`:

| Problema | Estado |
|---|---|
| `isPhysicalDevice(cfg)` booleano, 44 call sites | ✅ Sustituido por `targetKind(cfg) drivers.TargetKind` + `switch`, con las guardas sim-only escritas `!= KindSim`. ⚠️ **Pero eso no basta para que un tercer kind falle cerrado**: `targetKind()` (`target.go:19-24`) devuelve `KindSim` para *cualquier* valor que no sea `"device"`, así que un label desconocido colapsa a simulador **antes** de llegar a ninguna guarda. Y `targetKindLabel()` lo normaliza de vuelta a `"simulator"` al escribir. Lo cierra la T8 de la Fase 1 |
| `Route()` con `prefer` hardcodeado | ✅ Eliminados los redundantes (capacidades con un único proveedor, comprobado uno a uno); los que desempatan de verdad siguen con test de regresión |
| 13 `Runner.Run("axe"/"idb"/"xcrun")` saltándose el router | ✅ Encaminadas |
| `--prefer-driver auto\|axe` | ✅ Acepta cualquier driver registrado |
| `cfg.SimulatorUDID`, 70 referencias | ⚠️ Sigue. La identidad del target *es* un UDID de CoreSimulator; una app macOS se identifica por `(bundle id, pid)` o ruta del `.app`. Lo resuelve la Fase 2 |

### Lo que se reutiliza tal cual

- `crashparse.go` / `ParseIPS` — los `.ips` de macOS son **el mismo formato JSON** de iOS 15 / macOS 12.
- `uiobservation.go` / `Element` — ya es "framework-neutral", y sus campos (`Role`, `Subrole`, `Title`,
  `Value`, `Focused`) son literalmente vocabulario AX de macOS. iOS es el que tomó prestado.
- `agent_tree.go`, `treediff.go`, `selector.go`, `evidence.go`, `runstate.go`, `flow.go`, `worker.go`.
- Logs: hoy `simctl spawn <udid> log stream --style compact --level debug --predicate …` (`cli.go:1273`). En
  macOS es **la misma línea sin `simctl spawn <udid>`** (verificado en local).
- `launch.go:197` ya exporta `MAV_PLATFORM: "ios"`. El gancho existe.

---

## 2. Viabilidad técnica

### 2.1 Estado real de las APIs (verificado, no de memoria)

Comprobado contra el SDK instalado (`MacOSX26.2.sdk`, Xcode 26.3), en
`ApplicationServices.framework/Frameworks/HIServices.framework/Headers/AXUIElement.h`:

- **No deprecados**: `AXUIElementCreateApplication`, `AXUIElementCreateSystemWide`,
  `AXUIElementCopyAttributeValue`, `AXUIElementCopyAttributeValues`,
  `AXUIElementCopyMultipleAttributeValues`, `AXUIElementSetAttributeValue`, `AXUIElementPerformAction`,
  `AXUIElementCopyElementAtPosition`, `AXUIElementSetMessagingTimeout`, `AXIsProcessTrustedWithOptions`.
- Deprecados desde 10.9, y son los tres que no interesan: `AXAPIEnabled`, `AXMakeProcessTrusted`,
  `AXUIElementPostKeyboardEvent`.

Dos detalles obligatorios que se descubren tarde:

- `AXUIElementCopyMultipleAttributeValues` — N atributos en **una** llamada IPC. Sin esto, recorrer un árbol
  es un RPC por atributo por nodo y `ui tree` se va a segundos.
- `AXUIElementSetMessagingTimeout` — sin timeout explícito, una app colgada cuelga a quien la inspecciona,
  es decir a `mav ui tree`. Obligatorio, no opcional.

**Límite conocido:** hay apps que no exponen su árbol AX salvo activación manual — Electron/Chromium
requieren `AXManualAccessibility`, algunas apps Java/Chromium `AXEnhancedUserInterface`, y hay casos donde ni
eso ([Electron #37465](https://github.com/electron/electron/issues/37465),
[foro Apple](https://developer.apple.com/forums/thread/756895)). Para apps SwiftUI/AppKit propias no aplica,
pero define el borde: **MAV-macOS funciona bien sobre apps nativas y mal sobre todo lo demás.**

### 2.2 Permisos TCC — el factor decisivo *en el Mac del desarrollador*

| Servicio TCC | Hace falta para | ¿Sin interacción? |
|---|---|---|
| `kTCCServiceAccessibility` | Leer el árbol AX de otra app, `AXUIElementPerformAction`, inyectar `CGEvent` | Sólo con MDM (perfil PPPC) o escribiendo en `/Library/Application Support/com.apple.TCC/TCC.db` **con SIP desactivado**. `tccutil` de Apple sólo *resetea* |
| `kTCCServiceScreenCapture` | `screencapture` de ventanas de otras apps | **No, de ninguna forma vía MDM.** Apple lo hizo *deny-only* en PPPC: ni un admin corporativo puede pre-autorizarlo. Además re-pregunta periódicamente (semanal en Sequoia 15.0, mensual desde 15.1; el panel se llama "Screen & System Audio Recording" desde macOS 26) |
| `kTCCServiceAppleEvents` | `osascript` → System Events | Prompt por par (origen, destino). PPPC sí puede pre-autorizarlo |
| `kTCCServiceMicrophone` | Grabar audio (Nokoru) | **Deny-only en PPPC**, igual que ScreenCapture |

El detalle más subestimado: **el permiso se atribuye al *responsible process*, no al binario.** Para un CLI,
quien queda autorizado es la terminal o el harness del agente (Terminal, iTerm, Claude Code), no `mav`.
Ventaja: un click autoriza todo lo que lances desde ahí. Desventaja: `mav doctor` no puede reportar de forma
fiable "tengo permiso", y lanzar el mismo `mav` desde otra terminal cambia el resultado sin que nada del repo
cambie.

**En GitHub-hosted runners es imposible** y lo es desde 2020 sin solución oficial:
[runner-images #1567](https://github.com/actions/runner-images/issues/1567),
[#1441](https://github.com/actions/virtual-environments/issues/1441),
[#3286](https://github.com/actions/runner-images/issues/3286),
[virtual-environments #553](https://github.com/actions/virtual-environments/issues/553),
[community #39846](https://github.com/orgs/community/discussions/39846). Vale igual para XCUITest que para
AXUIElement: la barrera es TCC, no la API.

> Pero esto es una propiedad del Mac que no controlas, no de macOS. En una máquina cuya imagen tú fabricas,
> TCC deja de ser un muro y pasa a ser una fila de una tabla SQLite. Ver §2.5.

### 2.3 Cómo implementarlo: cuatro opciones

| Opción | Pros | Contras |
|---|---|---|
| **`osascript` + System Events** | Cero dependencias | Lo más lento (un Apple Event por propiedad); sin ids estables — los selectores son `UI element 3 of group 2 of window 1`; necesita **dos** permisos; `osascript` no es bundle. **Descartada como capa principal:** no sostiene `mav ui tap --id`. Pero ver §2.4 bucket D para dónde sí aporta |
| **AXUIElement nativo vía cgo** | API completa, batching, timeouts | Mete cgo en un proyecto con **una** dependencia (`yaml.v3`) que compila cruzado en 2 líneas de `release.yml`. Con `CGO_ENABLED=1` eso se acaba. Y mantienes tu propio walker AX para siempre. **Choca con la regla "no self-maintained low-level tooling"** |
| **Helper en Swift** | Nativo, sin cgo, encaja con el patrón "shell out a un CLI" | Dos binarios; fórmula no trivial; toolchain Swift en el pipeline. Misma regla incumplida, versión suave |
| **Envolver CLIs existentes** ⭐ | Exactamente el patrón AXe. Cero mantenimiento de bajo nivel | Ningún candidato tiene la solidez que AXe tiene en iOS. Riesgo de bus factor |

**Herramientas ya hechas equivalentes a AXe para macOS:**

| Herramienta | Qué cubre | Madurez | Instalación |
|---|---|---|---|
| [**Peekaboo**](https://github.com/openclaw/Peekaboo) ([docs](https://peekaboo.sh/)) | `see` (árbol AX), `click`, `type`, `press`, `scroll`, `drag`, **`menu`**, `dialog`, **`window`**, `space`, `app`, `capture` | La más establecida. v3.0.0 (nov 2025). Brew + npm + MCP | `brew install steipete/tap/peekaboo` |
| [**axcli**](https://github.com/andelf/axcli) | snapshot AX, click, type, keys, scroll, screenshot; **input background-safe** vía `CGEventPostToPid`; selectores tipo CSS; screenshots vía ScreenCaptureKit | 27★, Rust, MIT/Apache | `cargo install axcli` (sin brew) |
| [**cliclick**](https://formulae.brew.sh/formula/cliclick) | Clicks y teclado sintéticos | **En homebrew-core** | `brew install cliclick` |
| [**ax-cli**](https://github.com/watzon/ax-cli) | `tree`, `find`, `wait`, `snapshot`, `diff`, `click`, `type`; JSON | 1★, 17 commits — inmaduro | `brew tap watzon/ax` |
| [**AXON / AXUI**](https://github.com/1amageek/AXUI) | Extracción AX plana con filtros | Pequeño | SPM |
| [**macos-use**](https://macos-use.dev/) | Árbol AX nativo + CGEvent, diff-only | Producción, MIT | **Sólo MCP, sin CLI** → no sirve como driver |
| [**appium-mac2-driver**](https://github.com/appium/appium-mac2-driver) | XCUITest de macOS vía WebDriverAgentMac | Mantenido por Appium | Pesado: build de WDA-mac, firma, y **Xcode Helper necesita Accessibility igual** |

**Decisión: los dos, Peekaboo y axcli.** No es cinturón y tirantes — ver §4.5, el reparto es forzado por una
limitación real de Peekaboo.

### 2.4 Mapeo comando a comando

Sólo **4 comandos son mobile-only de verdad**. La primera versión de este documento daba 8; al mirarlos uno a
uno, la mitad tenía camino.

**Bucket A — 1:1, sólo cambia el backend (~14). Éste es el criterio de aceptación.**

| Comando MAV | Backend macOS |
|---|---|
| `ui tree` | AXUIElement / `peekaboo see` |
| `ui tap --id/--text` | `AXUIElementPerformAction(kAXPressAction)` — **mejor que iOS**: acción real del elemento, no un tap por coordenadas |
| `ui tap --x --y` | `CGEventPostToPid` (axcli) |
| `ui type` | `CGEvent` de teclado, o `kAXValueAttribute` |
| `ui erase --focused` | `AXUIElementSetAttributeValue(element, kAXValueAttribute, "")`, o Cmd+A + Delete |
| `ui wait`, `ui scrollUntil` | Se construyen sobre `ui tree` |
| `capture` | `screencapture -x -l <windowid>` o `-R x,y,w,h` |
| `capture` (vídeo) | `screencapture -v` / `-V <segundos>` (verificado en el `man` local) |
| `logs` | `log stream --style compact --level debug --predicate …` |
| `crashes` | `~/Library/Logs/DiagnosticReports` + `/Library/Logs/DiagnosticReports`; `ParseIPS` sin tocar |
| `evidence *`, `run`, `flow lint` | Agnósticos, cero cambios |
| `openURL` | `open <url>` |
| `clipboard copy/read` | `pbcopy`/`pbpaste` (más simple que en sim) |
| `app kill` | `kill` / `osascript quit` |
| `debug attach/break/eval` | `lldb-dap` contra pid local — **más simple que iOS** |

**Bucket B — el comando existe pero significa otra cosa (~9)**

| Comando | Qué cambia |
|---|---|
| `open` (build/install/launch) | No hay contenedor sandbox creado por `simctl install`. La inyección de entorno (equivalente de `SIMCTL_CHILD_*`) obliga a ejecutar `App.app/Contents/MacOS/<binario>` directamente: `open -n` no propaga variables |
| `open --clear-state` | `rm -rf ~/Library/Containers/<bundle>` + `defaults delete`. Ver Fase 1: compone con fixtures |
| Selección de target | "Qué UDID" → "qué app / qué ventana" en host; "qué VM" con VM |
| `ui tree --include-system` | En iOS es SpringBoard vía baguette. En macOS la barra de menús, el Dock y las otras apps son **simplemente más apps AX**: sale gratis, pero el flag deja de significar lo mismo |
| `ui pinch` | Los gestos multitouch sintéticos sólo existen por **API privada** (`kCGEventGesture`); el camino público, `NSEvent`, no se puede sintetizar entre procesos. **Pero** muchas vistas macOS implementan zoom como Cmd+scroll: `pinch` mapea a scroll-con-modificador |
| `ui press volume_up/down` | `osascript -e 'set volume output volume N'` |
| `ui press lock` | `pmset displaysleepnow` |
| `time freeze/travel/scale` | En el Mac del dev: `simtime` inyecta `libsimtime.dylib` vía `DYLD_INSERT_LIBRARIES`, y el hardened runtime **no lo honra** en ejecutables protegidos (haría falta `com.apple.security.cs.allow-dyld-environment-variables` **y** `com.apple.security.cs.disable-library-validation`) → sólo builds debug propias. **Con VM: cambiar el reloj del sistema es aceptable porque la máquina es desechable** |
| `location set/reset` | No hay override para una app ya lanzada, **pero** Xcode expone "Simulate Location" a nivel de scheme y `Simulated Location` en test plans, y MAV ya lanza la app con su launch recipe. Condicional, no imposible |
| `network start/stop` | **Mejor en macOS.** `mitmproxy --mode local:<app>` o `local:<pid>` intercepta por proceso sin tocar el proxy del sistema |
| `run --matrix` | Sobre runtimes de simulador → **sobre imágenes de VM** (macOS 15 / macOS 26) |
| `sim list/select/boot` | → `vm list/select/boot`: `tart clone` + `tart run` es literalmente `simctl create` + `simctl boot` |

**Bucket C — genuinamente mobile-only, se documenta y se cierra (4). Fuera de alcance.**

| Comando | Por qué |
|---|---|
| `ui press home` | No existe el concepto |
| `ui hideKeyboard` | No hay teclado software |
| `ui rotate` / `ui twoFingerPan` | Sólo por API privada de CGEvent. Perseguirla viola la regla de no mantener tooling de bajo nivel, y se rompería sin aviso |
| `device list/select` | Otro eje entero (dispositivo iOS físico) |

**Bucket D — lo que macOS AÑADE y iOS no tiene**

| Capacidad nueva | Cómo |
|---|---|
| **Barra de menús automatizable** | AX expone `AXMenuBar` de cada app; Peekaboo tiene `menu`. En iOS no existe nada equivalente. Vía de control semántica, estable y localizable |
| **Gestión de ventanas** | Múltiples ventanas por app, mover/redimensionar, Spaces. `peekaboo window`/`space` |
| **Diccionarios AppleScript** | Una app macOS puede exponer su propio `.sdef` y ser conducida semánticamente — un canal que iOS no tiene **en absoluto**. Aquí es donde AppleScript sí aporta: no como sustituto del árbol AX, sino como capacidad *adicional* |
| **Captura de red por proceso** | `mitmproxy --mode local:<pid>` |
| **Debug directo** | `lldb-dap` attach a un pid local, sin `simctl spawn` |

### 2.5 VMs: el modelo de target correcto, no un parche de CI

#### 2.5.1 Lo que Tart da, verificado

[**Tart**](https://github.com/openai/tart) — VMs macOS/Linux sobre Apple Silicon usando
Virtualization.framework. 6545★, release 2.35.0 (2026-08-04), Swift, activo. **Nota:** el repo era
`cirruslabs/tart` y ahora redirige a `openai/tart`; licencia **FSL-1.1-ALv2** (convierte a Apache 2.0 a los
2 años) — libre para este uso, no es MIT.

Las imágenes base de cirruslabs traen de fábrica:

- **SIP desactivado.** La llave de todo: con SIP off, el provisioning **escribe los grants directamente en
  `TCC.db`**. Accessibility, Screen Recording y micrófono dejan de ser un click humano.
- **Auto-login del usuario `admin` en una sesión Aqua completa con WindowServer.** El segundo requisito no
  obvio: el Accessibility API necesita una sesión GUI real, y una VM la tiene aunque arranque headless.
- **Arranque headless** (`--no-graphics`), con VNC opcional (`--vnc-experimental`).
- SSH con credenciales conocidas, clonado de imágenes, e imágenes para macOS 15 y macOS 26, con
  [plantillas Packer públicas](https://netjibbing.com/post/packer-macos-26/).

No es teórico: [**jonnyzzz/tart-skills**](https://github.com/jonnyzzz/tart-skills) es este patrón ya
construido. Y está productizado en otro sitio: **CircleCI desactiva SIP en todas sus imágenes desde Xcode
11.7 y publica un orb para insertar permisos en `TCC.db`.**

**Audio:** tart soporta micrófono — `VZHostAudioInputStreamSource` en `Sources/tart/VM.swift`, bajo el flag
`--audio` y sólo cuando la VM **no** es suspendable. Relevante si algún día se valida el flujo de grabación
de Nokoru; **crabbox no expone ese flag** en su provider de tart.

Restricciones: Apple Silicon obligatorio; **máximo 2 VMs macOS concurrentes** (límite de
Virtualization.framework *y* del EULA); en hosts headless con macOS 15+ hay que desbloquear `login.keychain`.

#### 2.5.2 Por qué esto reordena el resto

El insight que lo une: **el papel real del simulador en MAV no es "iOS". Es "un dispositivo desechable, de
propiedad exclusiva, que puedo borrar y del que puedo tener varios".** Todo lo que quedaba fuera en macOS
quedaba fuera por ser destructivo o global. En una máquina desechable eso deja de ser un problema.

De ahí que la maquinaria "sim-only" **no sea código muerto para macOS**:

| Maquinaria existente | Su análogo con VM |
|---|---|
| `simlock.go` — un run, un simulador, nunca compartido | Un run, una VM. Idéntico, y más necesario: **sólo caben 2** |
| `target_command` + keepalive + stale-retry | Preguntar al pool manager qué VM usar. El diseño ya es agnóstico |
| `matrix.go` | Matriz sobre imágenes de VM (macOS 15 / 26) |
| `resolveBootedSimulator` | `tart list` de VMs corriendo |
| `simctl erase` / `--clear-state` | Re-clonar desde la imagen base (checkpoint/fork aún no está en el provider `tart`; sí en `parallels`) |

El eje real no es "sim vs device", son dos: **plataforma** (iOS / macOS) × **naturaleza**
(desechable-y-exclusivo / real-y-compartido). `mac-vm` se comporta como `sim`; `mac-host` como `device`.

### 2.6 El stack de openclaw ya cubre la infraestructura — MAV no debe construirla

Recordando el rol de MAV — **wrapper sobre drivers con una capa de evidencia** — MAV no debe poseer
máquinas, ni transporte, ni orquestación. Debe poseer *el control de la app y la evidencia sobre la app*.

| Lo que MAV necesita | Lo que ya existe | Estado |
|---|---|---|
| Máquina macOS desechable, lease, sync, ejecución remota, evidencia de run, release | [**crabbox**](https://github.com/openclaw/crabbox) — Go, **MIT**, 1320★ | **Provider `tart` de primera clase** (alias `local-tart`, `macos-vm`) |
| Driver AX de macOS | [**Peekaboo**](https://github.com/openclaw/Peekaboo) sobre [**AXorcist**](https://github.com/openclaw/AXorcist) | Listo, brew |
| Mission control de runs de agente | [**crabfleet**](https://github.com/openclaw/crabfleet) — trata crabbox como *runtime type* | Existe |
| AppleScript / JXA | [**macos-automator-mcp**](https://github.com/steipete/macos-automator-mcp) | **MCP, no CLI** → no sirve como driver |
| Bindings Go de Virtualization.framework | [**steipete/vz**](https://github.com/steipete/vz) | No hace falta: crabbox ya envuelve `tart` |

#### ¿Es crabbox para Mac o sólo para Linux?

El README engaña: de sus ~50 providers casi todos dicen "Linux". **El núcleo soporta macOS de primera; los
extras son Linux-first.** Verificado leyendo los 124 docs de `docs/`:

| Funcionalidad | ¿macOS (provider `tart`)? |
|---|---|
| Lease con TTL, idle-timeout, renovación, release | ✅ |
| Sync del checkout sucio por rsync | ✅ |
| Ejecutar comando, stream de salida, exit code | ✅ |
| Warm reuse (`warmup` / `prewarm` / `--id`) | ✅ |
| `--desktop` (Screen Sharing nativo del guest) | ✅ |
| `crabbox webvnc` + `crabbox screenshot` sobre el guest macOS | ✅ |
| `--artifact-glob` / `--require-artifact` | ❌ [#1393 abierta](https://github.com/openclaw/crabbox/issues/1393) |
| Actions hydration | ❌ *"still requires Linux"* |
| Grabación MP4 de `desktop` | ❌ *"macOS is not supported for recording"* |
| `--code`, `--tailscale` | ❌ |
| Checkpoint / fork / restore | ❌ en `tart`; ✅ en `parallels` |
| Flag de audio (`tart run --audio`) | ❌ no expuesto |

Detalle a favor: `crabbox screenshot --provider tart` captura el guest **por ARD/VNC, no por
`screencapture`**, así que no depende del Screen Recording TCC dentro del guest.

#### ¿Y qué aporta sobre usar `tart` a pelo?

Su doc describe el provider paso a paso: `tart clone` → `tart set` → `tart run --no-graphics` → sondear
`tart ip` → `tart exec` para inyectar la clave SSH → esperar SSH → sync → run → `stop` + `delete`. **Eso es
exactamente el script que escribirías tú.** Cuatro cosas encima:

1. **Leases con TTL y release garantizado.** Con 2 VMs máximo, una VM huérfana de un script que petó te come
   media capacidad. Esta lección MAV ya la pagó: el saga de `target_command` + keepalive de la v0.10.0.
2. **Warm reuse.** Su propio smoke usa `--ttl 30m` porque *"the first run may need to pull and boot a macOS
   base image"*. Sin `warmup` + `--id`, **cada** `mav run` paga arranque de VM.
3. **Sync correcto**: tracked + nonignored, fingerprint skip, guardas contra borrados masivos.
4. Distribución firmada/notarizada por brew, historial, `--timing-json`.

**El contraargumento honesto:** si sólo corres una VM, un repo, un comando cada vez, `tart clone && run &&
ssh && rsync` son ~40 líneas y cero dependencias. Compensa porque el caso de MAV es el otro: bucle en
caliente, varios worktrees, jobs en paralelo peleándose por 2 slots.

Lo que quita el miedo a la dependencia: el provider `tart` es **local puro** — *"never uses the coordinator
or cloud credentials"*.

#### La costura real, identificada aguas arriba

Sacar `.mav/runs/<id>/` de la VM. `--artifact-glob` es el mecanismo exacto, pero *"Native Windows and macOS
targets reject this collector"*. Está trackeado y abierto:
[**crabbox#1393**](https://github.com/openclaw/crabbox/issues/1393). Salidas, en orden: contribuirlo aguas
arriba; `rsync` sobre `crabbox ssh --id` mientras tanto; o conformarse con la línea `ok cmd=… k=v` de stdout.

#### Dónde NO se solapan

crabbox tiene `desktop click/paste/type/key` y `desktop proof`. Suena a que ya hace lo de MAV, y no: eso es
**input por coordenadas sobre un escritorio Linux gestionado**. MAV es **control semántico del árbol de
accesibilidad de una app Apple**. Capas distintas:

| Capa | Responsable | Evidencia que produce |
|---|---|---|
| Máquina, lease, sync, transporte | **crabbox** | "este comando corrió aquí, con este diff, y salió esto" |
| App: árbol, taps semánticos, esperas, capturas, crashes | **MAV** | "la app hizo esto: este árbol, esta captura, este crash" |

---

## 3. El rename

### 3.1 Qué hay en juego

`bitomule/mav`: **3 estrellas, 0 forks, creado 2026-05-02, 31 releases, 81 clones / 62 únicos en 14 días.**
Uso real pero pequeño. Dos puertas públicas: `brew install bitomule/tap/mav` y
`npx skills add bitomule/mav --skill mav --global` (hardcodeado en `cli.go:634`).

### 3.2 Coste real

| Superficie | Coste | Comentario |
|---|---|---|
| Module path (23 ficheros) | Trivial | `sed` + `go.mod` |
| Nombre del binario | Bajo | `cmd/<nombre>/`, `Makefile`, 4 assets en `release.yml`, 2 `awk`, clase de la fórmula, `bin.install`, y los dos `assert_match` del `test do` |
| Docs | Medio-mecánico | ~1000 ocurrencias de "mav" |
| Rename del repo | Gratis | GitHub redirige web y remotes |
| Fórmula de Homebrew | **Trampa** | `Formula/mav.rb` debe quedarse o `brew install bitomule/tap/mav` da 404. Si la borras, las instalaciones existentes dejan de actualizarse **en silencio** |
| **`.mav/` + `config.yaml` + `current-run` + `/tmp/mav/sim-locks/`** | **Alto** | Estado en disco en *cada repo consumidor* |
| **Las 22 variables `MAV_*`** | **Alto** | Escritas a mano en los `launch.commands` de cada `.mav/config.yaml`. Renombrarlas rompe todos los proyectos a la vez |
| **La skill `mav`** | **Alto** | Los agentes que la instalaron en global se quedan con la copia vieja; convivirían las dos |

Las tres filas caras son exactamente donde un shim sería la respuesta obvia — y la regla del proyecto lo
prohíbe. Un rename sería un **corte seco**.

### 3.3 Candidatos, con colisiones comprobadas

Verificado contra la API de Homebrew, búsqueda de repos en GitHub por estrellas, `proxy.golang.org` y el PATH.

| Candidato | Lectura | brew core | brew cask | Colisión GitHub | Veredicto |
|---|---|---|---|---|---|
| **`mav` (mantener)** | "Mac & Mobile Agent Verifier", o simplemente un nombre | 404 libre | 404 libre | Nada exacto; ruido con maven/mavericks. Ojo: "MAV" = *Micro Air Vehicle* en robótica | ✅ **La opción** |
| `aav` | Apple Agent Verifier | 404 | 404 | Aave (DeFi, 1437★+) domina el token | ❌ |
| `pav` | Platform Agent Verifier | 404 | 404 | DeepPavlov 6990★, pavex 2069★ | ⚠️ no significa nada |
| `verity` | Enfoque evidencia | 404 | 404 | *dm-verity* (kernel) es el significado dominante | ❌ |
| `pippin` | Variedad de manzana | 404 | 404 | `pippinlovesyou/pippin` 550★ — **framework de agentes autónomos**, mismo espacio | ❌ |
| `orchard` | Manzanal | 404 | **200 — cask existente** + `cirruslabs/orchard` (orquestación de VMs macOS, dominio idéntico) | OrchardCMS 8157★ | ❌ triple |
| `gala` | Variedad de manzana | 404 | 404 | Difuso y genérico | ⚠️ |

### 3.4 La alternativa barata

Dejar de tratar "MAV" como siglas. `git` no significa nada. Cuando se confirme el go de macOS:

- README: *"Mobile Agent Verifier (`mav`) is the interface between an agent and iOS"* → *"`mav` is the
  interface between an agent and Apple app UIs"*.
- `desc` de la fórmula → `"Agent control plane for Apple app UIs"`. **Ojo:** el `test do` hace
  `assert_match "Mobile Agent Verifier"` — hay que cambiar los dos a la vez o el release falla.
- `description` de la skill: quitar "iOS", dejar "Apple apps".

---

## 4. Plan por fases

### Fase 0 — Higiene del router — **HECHA** (PR #53, `v0.11.0`)

`targetKind()` n-ario sustituyendo a `isPhysicalDevice`, `--prefer-driver` abierto a cualquier driver
registrado, `prefer` redundantes eliminados, y las 13 llamadas directas encaminadas por el router. Sin esto,
un target nuevo no era "un driver más" sino tocar `cli.go` entero.

*(El cambio de tagline se dejó fuera a propósito: depende del go de macOS y toca los `assert_match` de la
fórmula.)*

### Fase 1 — Perfiles y fixtures (`v0.12.0`)

Desbloquea todo lo demás, y es la única fase con cambio de formato de config.

**Perfiles por plataforma** en `.mav/config.yaml`: un bloque por plataforma que sobreescribe `app_target`,
`launch.commands` y el kind de target. El motivo es concreto y verificado: Nokoru necesita
`//App:NokoruiOS` y `//App:NokoruMac` desde el mismo repo, **y ambos usan `app_info.bundle_id_debug`**
(`App/BUILD.bazel:118` y `:178`), así que el campo `bundle_id` no los distingue.

**Fixtures**: `fixtures: { <nombre>: [comandos] }`, seleccionables por run (`--fixture <nombre>`, y campo
equivalente en el flow). Son comandos que dejan la app en un estado conocido. **Complementan
`launch.commands`, no lo reemplazan.**

- **Punto de inserción**: en `runLaunchRecipe` (`internal/mav/launch.go`), **entre el paso `install` y el
  paso `launch`**. Es la única ventana donde el contenedor ya existe y nada tiene el sqlite abierto.
- **Estados con nombre, no un bloque único**: un flow de búsqueda quiere datos, uno de onboarding quiere
  vacío. Sin nombres acabas con un solo estado que no sirve a ninguno de los dos.
- **`--clear-state` compone**: borra el contenedor → el fixture siembra encima. Limpio + estado conocido =
  run determinista. Es el equivalente honesto de `simctl uninstall` + siembra.
- **iOS y macOS a la vez**: en iOS el fixture opera sobre el contenedor del simulador; en macOS sobre
  `~/Library/Containers/<bundle-id>/Data/`.

Ejemplo real para la documentación, con rutas verificadas en `App/Shared/NokoruPaths.swift`: sembrar el
GRDB de Nokoru y unos `.m4a` bajo `Application Support/Nokoru/` (`recordings/<uuid>.m4a`).

### Fase 2 — Modelo de target macOS (`v0.13.0`)

- `drivers.KindMac` en el enum; `Target.AppPath` y `Target.PID` junto a `UDID` (no en lugar de).
- `target_kind: macos` en el perfil; `MAV_PLATFORM=macos` (el campo ya existe en `launch.go:197`, hoy
  siempre `"ios"`).
- `mav doctor` reporta estado TCC, diciendo **qué proceso es el titular del permiso** — es el padre, no
  `mav` (§2.2).
- Sin cambio de comportamiento para usuarios actuales.

### Fase 3 — Los dos drivers macOS (`v0.14.0`) — el entregable que prueba la tesis

`internal/mav/drivers/macos/`, con **dos** drivers:

| Driver | Sirve |
|---|---|
| **Peekaboo** | Árbol AX, `menu`, `window`, capturas |
| **axcli** | Todo el input: tap, type, scroll, drag — vía `CGEventPostToPid` |

- **El reparto sale de `Cost(cap, target)`, sin interfaz nueva.** axcli declara coste 0 para las capacidades
  de input en `KindMac`; Peekaboo declara coste alto para esas mismas y coste 0 para árbol, `menu`, `window`
  y capturas. El router ya ordena por ahí, así que background-safe queda **activo por defecto** sin añadir
  un segundo eje de decisión: si el agente valida mientras trabajas, robarte el foco a mitad de un flow no es
  un detalle, es que no puedes usar el Mac. Se renuncia puntualmente con `--prefer-driver`. Ver §4.5.
- Lectura directa sin herramienta: `screencapture`, `log stream`, `~/Library/Logs/DiagnosticReports`.
- **Distribución**: fórmula espejo de axcli en `bitomule/tap` **apuntando al release upstream de
  `andelf/axcli`** — un puntero y un sha, no un fork. Mismo tratamiento para el resto de herramientas, de
  modo que el usuario haga un solo `brew tap` sin que nadie pase a mantener código ajeno.
  `mav setup --install axcli` por el mismo camino que axe/baguette.
- **Criterio de aceptación: bucket A completo (§2.4) contra NokoruMac.**

### Fase 4 — VM configurable (`v0.15.0`)

- crabbox con `provider: tart`, seleccionable **desde el perfil** (no un flag suelto).
- Warmup que instala `mav` + drivers y siembra los grants de TCC en `TCC.db` — posible porque la imagen base
  trae SIP off.
- Sacar `.mav/runs/<id>/`: `rsync` sobre `crabbox ssh --id`, o contribuir
  [crabbox#1393](https://github.com/openclaw/crabbox/issues/1393).
- **Disparador para hacerla**: el primer prompt de TCC que bloquee un run local. Nokoru pide cinco permisos
  (micrófono, reconocimiento de voz, captura de audio, calendario, Apple Events), así que probablemente
  llegue pronto.

### Explícitamente fuera

Rename. Backend XCUITest/appium-mac2. Gestos multitouch por API privada. **Bucket C** (4 comandos
mobile-only). Escribir orquestación de VMs, leasing, sync o transporte dentro de MAV. Y **cubrir la
funcionalidad de Nokoru**: es banco de pruebas para MAV, no el objeto de la validación.

### 4.5 Riesgos de diseño anotados

Dos cosas que salieron del propio diseño y que conviene fijar **antes** de escribir código.

**1. El reparto background-safe se expresa con `Cost`, no con un rasgo nuevo en la interfaz `Driver`.**

*(Decisión revisada y cerrada. La primera versión de este plan proponía un `BackgroundSafe() bool` en la
interfaz; se descartó por lo que sigue.)*

El problema de partida es real: axcli también provee árbol de accesibilidad, así que un desempate global por
"es background-safe" le haría ganar `tree` a Peekaboo por un motivo que no tiene nada que ver con el foco, y
con ello se perderían `menu` y `window`, que es justamente por lo que Peekaboo entra.

La corrección aparente —acotar el desempate a capacidades de input— tiene un coste escondido: `Route()`
(`drivers/router.go:79-131`) necesitaría una lista cableada de qué capacidades cuentan como "input". Eso es
conocimiento por-capacidad viviendo fuera de la declaración de capacidades: exactamente la clase de
special-case que el PR #53 acaba de eliminar. Y obligaría a los 6 drivers de iOS a contestar una pregunta que
no les aplica.

**`Cost(cap, target)` (`drivers/driver.go:37`) ya es *por capacidad* y *por target*, que son justo los dos
ejes que hacen falta.** Un driver que roba el foco declara coste alto para las capacidades de input en
`KindMac`; el reparto Peekaboo/axcli sale solo. Un único criterio de ordenación, cero cambios de interfaz,
ninguna lista cableada, y ningún driver de iOS tocado.

El corolario para la Fase 3: los dos drivers de macOS se distinguen **por su tabla de costes**, no por un
flag. Y si mañana aparece un tercer driver background-safe, entra sin tocar el router.

**2. Background-safe por defecto puede fallar en silencio al escribir.** `CGEventPostToPid` entrega el evento
al proceso, pero hay controles de AppKit que sólo aceptan teclado con foco real. No es resoluble leyendo
código: la primera vez que un `ui type` no escriba nada en un campo, es esto. Mitigación: que el driver
verifique el valor tras escribir (releer `kAXValueAttribute`) en vez de reportar éxito a ciegas.

**Por qué axcli no es opcional, verificado en el código de Peekaboo:** Peekaboo **enfoca por defecto** —
`ensureFocused` corre justo antes de cada click (`Core/PeekabooAutomationKit/.../FocusUtilities.swift`,
`docs/commands/click.md`) y llega a saltar de Space si hace falta. Existe `--no-auto-focus`, pero eso sólo
evita robar el foco: el evento sigue yendo a lo que esté delante, así que aciertas menos, no más. La entrega
por PID es otra cosa, y es lo que hace falta para un requisito de background-safe.

---

## Fuentes

**Verificado en local (no web):** `MacOSX26.2.sdk` (Xcode 26.3) — `AXUIElement.h`, headers de
`Virtualization.framework`; `man screencapture`; `log stream --help`; API de Homebrew; `gh api
search/repositories`; `gh api repos/openai/tart`; `proxy.golang.org`. De Nokoru:
`App/BUILD.bazel`, `App/Shared/NokoruPaths.swift`, `App/MacOSInfo.plist`, `MUSTS.yml`, `.mav/config.yaml`.
De Peekaboo (checkout local): `docs/focus.md`, `docs/commands/click.md`,
`Core/PeekabooAutomationKit/Sources/PeekabooAutomationKit/Utilities/FocusUtilities.swift`.

**Accessibility API y sus límites**
- [Electron #37465 — `AXManualAccessibility` no soportado](https://github.com/electron/electron/issues/37465)
- [Apple Developer Forums — elementos AX sólo expuestos con VoiceOver / Accessibility Inspector](https://developer.apple.com/forums/thread/756895)
- [MacPaw Research — Parsing macOS application UI](https://research.macpaw.com/publications/how-to-parse-macos-app-ui)

**Permisos TCC**
- [jano.dev — Accessibility Permission in macOS (2025)](https://jano.dev/apple/macos/swift/2025/01/08/Accessibility-Permission.html)
- [Entonos — How to modify TCC on macOS via command line](https://entonos.com/2023/06/23/how-to-modify-tcc-on-macos/)
- [Rainforest QA — A deep dive into macOS TCC.db](https://www.rainforestqa.com/blog/macos-tcc-db-deep-dive)
- [Addigy — PPPC para usuarios estándar](https://support.addigy.com/hc/en-us/articles/4403549601043-Privacy-Preferences-Policy-Control-PPPC-for-Standard-Users) — ScreenCapture y micrófono son deny-only
- [9to5Mac — Sequoia pasa el prompt de Screen Recording a mensual](https://9to5mac.com/2024/08/14/macos-sequoia-screen-recording-prompt-monthly/) · [iDownloadBlog — 15.1 reduce la frecuencia](https://www.idownloadblog.com/2024/10/09/macos-sequoia-15-1-macos-screen-recording-prompts-frequency-reduced/)

**CI y VMs**
- [runner-images #1567](https://github.com/actions/runner-images/issues/1567) · [#1441](https://github.com/actions/virtual-environments/issues/1441) · [#3286](https://github.com/actions/runner-images/issues/3286) · [virtual-environments #553](https://github.com/actions/virtual-environments/issues/553) · [community #39846](https://github.com/orgs/community/discussions/39846)
- [Tart (openai/tart)](https://github.com/openai/tart) · [Quick Start](https://tart.run/quick-start/) · [FAQ](https://tart.run/faq/) · `Sources/tart/VM.swift` (soporte de audio)
- [jonnyzzz/tart-skills](https://github.com/jonnyzzz/tart-skills) · [Packer templates para macOS 15 y 26](https://netjibbing.com/post/packer-macos-26/)
- [CircleCI — Testing macOS apps](https://circleci.com/docs/guides/test/testing-macos/) — SIP off desde Xcode 11.7 y orb para TCC.db
- [tart discussion #507 — límite de 2 VMs](https://github.com/cirruslabs/tart/discussions/507) · [Eclectic Light — cómo Apple limita las VMs](https://eclecticlight.co/2022/08/04/virtualisation-on-apple-silicon-macs-8-how-apple-limits-vms/)

**Stack de openclaw / steipete**
- [crabbox](https://github.com/openclaw/crabbox) (Go, MIT) · [provider tart](https://github.com/openclaw/crabbox/blob/main/docs/providers/tart.md) · [artifacts](https://github.com/openclaw/crabbox/blob/main/docs/features/artifacts.md)
- [crabbox#1393 — artifact globs en targets macOS (abierta)](https://github.com/openclaw/crabbox/issues/1393)
- [crabfleet](https://github.com/openclaw/crabfleet) · [AXorcist](https://github.com/openclaw/AXorcist) · [macos-automator-mcp](https://github.com/steipete/macos-automator-mcp) · [steipete/vz](https://github.com/steipete/vz)

**Herramientas**
- [Peekaboo](https://github.com/openclaw/Peekaboo) · [docs](https://peekaboo.sh/) · [axcli](https://github.com/andelf/axcli) · [cliclick](https://formulae.brew.sh/formula/cliclick) · [ax-cli](https://github.com/watzon/ax-cli) · [AXUI](https://github.com/1amageek/AXUI) · [macos-use](https://macos-use.dev/) · [appium-mac2-driver](https://github.com/appium/appium-mac2-driver)

**Otros**
- [mitmproxy — Intercepting macOS Applications (local redirect mode)](https://www.mitmproxy.org/posts/local-capture/macos/)
- [Apple — Interpreting the JSON format of a crash report](https://developer.apple.com/documentation/xcode/interpreting-the-json-format-of-a-crash-report) · [Acquiring crash reports and diagnostic logs](https://developer.apple.com/documentation/xcode/acquiring-crash-reports-and-diagnostic-logs)
- [Apple — Simulating location in tests](https://developer.apple.com/documentation/xcode/simulating-location-in-tests)
- [HackTricks — DYLD_INSERT_LIBRARIES y hardened runtime](https://hacktricks.wiki/en/macos-hardening/macos-security-and-privilege-escalation/macos-proces-abuse/macos-library-injection/macos-dyld-hijacking-and-dyld_insert_libraries.html) · [Qt/Squish — automatizar apps con hardened runtime](https://qatools.knowledgebase.qt.io/squish/mac/troubleshoot/hardened-runtime/)
