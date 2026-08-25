# Changelog

## v0.13.0

### macOS in a disposable VM

- **`vm: true` next to `target_kind: macos` runs the app under test in a throwaway
  machine, and nothing about driving mav changes.** `open`, `ui tree`, `ui tap`,
  `capture`, `run`, `logs`, `crashes`, `evidence` and `network` take the same arguments
  and answer the same way; the only new field is `vm=true`, so an agent chaining loose
  commands can tell whether what it just drove was the VM's app or this machine's.
- **That one key is the whole config surface.** No host, no IP, no job name, no tool
  name, no key path. Which hypervisor provides the machine is mav's business, so the day
  it changes no `config.yaml` on anybody's disk has to. It replaces `runner:
  local|crabbox`, which shipped in v0.12.0, only declared an intent and did nothing.
- **Evidence lands in the local `.mav/runs/<id>/`**, which is the part that decides
  whether the feature is real. Captures, trees, logs, HAR and `report.json` are rsynced
  out of the guest after every command, not once at the end: an agent driving mav command
  by command never reaches anything that would be "the end", and evidence it cannot read
  until some later command happens to sync is evidence it reasons about stale. The
  upstream artifact mechanism was not an option: `crabbox run --artifact-glob` rejects
  native macOS targets ([crabbox#1393](https://github.com/openclaw/crabbox/issues/1393)).
- **The machine is handed back on `mav stop`, at the end of a flow, and on an idle
  timeout.** Not tidiness: Virtualization.framework *and* the macOS EULA cap you at two
  concurrent macOS VMs, so a leaked lease blocks the next run. The idle path rides the
  run worker's existing lease expiry, which is the one place mav already knows the run's
  owner is gone, so an agent that crashes releases the machine without anybody's help.
  Evidence always comes home before the release: the order has one safe direction and no
  second chance.
- **The remote project root is the same absolute path as the local one.** mav computes
  artifact paths everywhere, and any other choice would mean translating each one at each
  call site with a silent wrong-file bug waiting on every one it missed.
- **The launch recipe splits across the two machines.** `healthcheck`, `build` and
  `app_path` stay here, because a VM image carrying every project's build dependencies is
  not an image anybody can share; `install`, the fixture, `launch` and `cleanup` run
  there, because that is where the app runs. The checkout and the built bundle are shipped
  across in between.
- **Guest processes are stopped on the guest.** The PIDs a run records in VM mode belong
  to the other machine, and signalling them here does not fail loudly, it kills whatever
  local process happens to hold that number. `Runner` grew an optional `Stop`, which is
  the seam that makes the difference visible instead of catastrophic.
- **`mav vm install` installs the VM tooling**, and every VM failure ends naming it.
  Nobody writing `vm: true` is told which hypervisor to go and install, because that is
  exactly the detail the config surface exists to hide. `mav doctor` reports
  `vm_tooling`, `vm_image` and the current lease without ever leasing a machine to do
  it -- with a budget of two, a diagnostic that takes a slot is not a diagnostic. Nothing
  in either command's output names the underlying tool, including on success.
- **The guest's copy of the bundle is re-signed ad-hoc when it would otherwise not
  launch**, and `open` says so with `resigned=adhoc`. A development-signed app carries
  entitlements tied to a team and a device list, and in a clean VM the kernel kills it on
  launch with no message; the symptom three commands later is "the app is not running",
  which points at everything except the signature. It is a real trade, iCloud and push go
  with it, so it is reported rather than done quietly, and only the guest's copy is
  touched.
- **An outdated hypervisor is caught before anything is leased.** Below 2.29 it dies while
  injecting the SSH key with a message about terminal sizes, and nothing in that message
  points at the version. `mav doctor` reports `vm_tooling=outdated` and `mav vm install`
  upgrades it.
- `vm: true` is rejected on a simulator or device target instead of ignored. A simulator
  is reached from this machine and a phone is plugged into it; accepting the flag there
  would leave somebody believing they were isolated when nothing had changed.

## v0.12.0

### macOS

- **`mav` stops being iOS-only.** `target_kind: macos` is a first-class target kind. A macOS
  app has no UDID: its identity is its bundle id plus the `.app` path the launch recipe
  resolves at runtime. `ui tree`, `ui tap`, `ui type`, `ui erase`, `ui swipe`, `ui wait`,
  `capture`, `open`, `app list`, `openURL`, `clipboard`, `logs`, `crashes`, `evidence`,
  `run`, `network` and `time` all work there. Logs and crashes came almost for free: `log
  stream` on the host is the same line as on the simulator minus the `simctl spawn`, and the
  Mac's `.ips` files are the same JSON as iOS's, so `ParseIPS` works untouched.
- **The driver is [cua-driver](https://github.com/trycua/cua) (MIT).** The reason is
  structural rather than a preference: macOS grants Accessibility and Screen Recording only
  to interactive GUI processes, so a CLI cannot hold them however many times you grant them
  to your terminal. The only architecture that works is a broker, an app that owns the
  permissions plus a socket, and cua-driver ships one: the binary mav invokes lives inside
  `/Applications/CuaDriver.app`. It returns the tree with geometry, the window capture and
  background input from a single tool, and the tree and the capture come out of the same
  call, so both describe the same instant.
- Two alternatives were dropped on measurements, not taste. Peekaboo discards windows with
  `layer != 0`, so an app whose UI is a floating panel gets neither tree nor capture.
  axcli's capture returned **the desktop** cropped to the window's bounds, with no error, and
  activated the app on the way. A plausible PNG instead of an error is worse than failing.
- **[axcli](https://github.com/andelf/axcli) stays as an escape hatch**, input only.
  cua-driver resolves the window through `list_windows`, and an app whose entire UI lives in
  an accessory window has to be addressed by pid instead. When there is no window to resolve,
  the error says so in those words rather than looking like "the app isn't open".
- Input goes through cua-driver and axcli sits behind it, and not because it delivers worse:
  `cg-pid` synthesizes a mouse event and there are SwiftUI buttons that accept it **without
  reacting**. Measured against a real app's onboarding, the tap reported success and the
  screen did not advance. cua-driver's click goes through AXPress when the element exposes
  it, and on those same buttons it takes effect.
- **Network capture works end to end.** `mav network start` brings up mitmproxy and points
  the system at the proxy itself with `networksetup`, no sudo, on the service the default
  route leaves through, not the first in the list, which is usually a VM's virtual interface
  and gives no error, it just captures nothing. `network stop` and `mav stop` restore it, and
  the previous state goes in the run directory: start and stop are separate invocations, and
  a run that dies must not leave the machine pointing at a dead proxy. If the mitmproxy CA is
  not trusted, the command says so with the exact `security add-trusted-cert`; without it
  HTTPS comes out as CONNECT tunnels with no content.
- **Time control**, with what is possible and without faking what isn't. `mav time travel
  --to` and `mav time reset` move the machine's clock; `freeze` and `scale` fail saying a
  system clock runs and cannot be stopped or accelerated. Closed by default outside a VM
  (`kern.hv_vmm_present`), forced with `--system-clock`. On iOS simtime interposes the clock
  the app sees; on macOS the only per-process route is libfaketime through
  `DYLD_INSERT_LIBRARIES`, which the hardened runtime blocks in any app signed for
  distribution.
- **`mav location` explains why it can't**, instead of a bare "unsupported" that invites an
  afternoon of searching: Xcode's "Simulate Location" is not a debugger feature, it travels
  over the DVT channel, which serves iOS devices, and does nothing against a macOS app; lldb
  has no equivalent command; the tools that exist fake a connected iPhone, not the Mac.
- `mav doctor` reports permissions by asking **the daemon**, which is the one that holds
  them, and starts it if it isn't running. `mav` does that itself, with `open -g` so it
  doesn't steal focus: an agent that invents the incantation starts a bare `cua-driver
  serve`, which loses the permission attribution that is the whole point of the broker.
- Two things macOS does not get: elements do not expose AXIdentifier, only `element_token`,
  which is valid inside one snapshot, so `ui tap --id` has no cross-run stability there. And
  there is no menu-bar interaction or window management.

### Profiles and fixtures

- **Platform profiles** in `.mav/config.yaml`: one block per platform overriding
  `app_target`, the launch recipe, `process_name`, `target_command` and the log fields.
  Selected with `--profile`, `MAV_PROFILE` or `default_profile`, in that order; a profile
  that doesn't exist fails naming the valid ones instead of quietly falling back. They are an
  overlay over the flat fields: a single-platform repo writes none and nothing changes for
  it.
- **Fixtures**: named states, lists of commands, that leave the app in a known situation.
  They run between `install` and `launch`, the only window where the container already exists
  and nothing holds its database open, which is also why the app is closed before seeding.
  They compose with `--clear-state` (wipe the container, the fixture seeds on top), they are
  available as `fixture:` in a flow's `open` step, and the one applied is recorded in
  `report.json`: a run whose evidence doesn't say which state it started from is not
  reproducible.
- `--fixture` is rejected alongside `--no-relaunch`, like `--clear-state` and for the same
  reason: `--no-relaunch` skips the whole recipe, so the fixture would never run and the
  agent would validate against data nobody seeded.
- **An unknown key inside a profile is an error** (`profile_unknown_key`) instead of being
  ignored. Writing `fixture:` in a profile did nothing, and from outside that is
  indistinguishable from it applying with no effect. Scoped to profiles on purpose: they are
  new, so no existing configuration can break.

### Output contract

- **A failure now exits 1.** They all exited 0: the `fail code=...` line was written and the
  process said everything went fine, so `mav ui tap ... && next-step` kept going after a
  failure and every agent had to read stdout to find out whether its own command had worked.
  If you script against mav, this is the change to look at.
- And output is **always** written, on failure too: `mav ui` buffered it and discarded it
  when the command errored, leaving nothing to read exactly when it matters most.
- **`report.json` no longer passes a verdict on a screenshot that never existed.** Every flow
  carried `"screenshot_evidence":{"ok":false}`, indistinguishable from a broken capture for
  whoever reads the JSON. The field is absent when there is nothing to validate.

### Fixes

- An unknown `target_kind` no longer fails open. `targetKind()` sent everything that wasn't
  the literal `"device"` to `KindSim`, so a `target_kind: macos` written by hand behaved as a
  simulator from end to end: it resolved a simpool lease, booted a simulated iPhone, and with
  `--clear-state` uninstalled the app from it using a `bundle_id` that in a cross-platform
  app is the same on both. Validation now runs at the end of loading, once the profile and
  `MAV_TARGET_KIND` have been applied, because all three sources have to go through the same
  filter.
- A `clear_state` that doesn't manage to uninstall is no longer dropped in silence. It
  doesn't abort the `open`, the ordinary case is that the app wasn't installed yet, but it
  comes out as `clear_state_warn`. Without that, `--clear-state` lied: you believed you were
  starting from scratch and carried the previous run's state along.
- **`mav capture` ignored `--prefer-driver`.** The flag was accepted and thrown away: capture
  always routed by cost, so the only way to dodge a broken driver was to uninstall it.
- **`ui tree` on a physical device serves through idb again.** Replacing the per-tool gate
  with the router left the idb path behind a block that always returns, so it became dead
  code and the tree started failing on a device instead of falling back. idb declares
  `CapTreeAX` now and serves it, behind AXe on a simulator and alone on a device.
- `SaveConfig` moves to `yaml.Marshal` instead of a hand-rolled writer. The writer omitted
  empty values, so the file could not express "present and equal to the empty string", which
  is exactly what a profile needs to **annul** an inherited command. And it refuses to write
  a config with a profile applied: saving it would flatten the profile onto the base, and a
  `mav sim select` in a repo with `default_profile` would have left the macOS `app_target` as
  the base, silently and with no way back.

### Tooling

- `scripts/build-mav-vm-image.sh` builds a reproducible tart image with mav, cua-driver,
  axcli and mitmproxy. It builds cua-driver from the fork carrying
  [trycua/cua#3375](https://github.com/trycua/cua/pull/3375), since the released one cannot
  see an accessory window, and it refuses to call the image ready while the driver's
  permissions are off. Those cannot be granted from a script on macOS 26: seeding TCC.db does
  not work, PPPC profiles are only honored from an MDM, and AppleScript cannot reach System
  Settings over SSH. They are flipped once while the image is built, and every VM cloned from
  it starts with them on.

## v0.11.0

- El router de capacidades vuelve a decidir de verdad. La mayoria de las llamadas a
  `Route()` pedian una capacidad **y ademas** clavaban el driver (`prefer: "axe"`,
  `"idb"`, `"baguette"`, `"simctl"`), y otras trece se saltaban el router entero
  llamando a `axe`/`xcrun` a pelo. Eso convertia la tabla de `Cost` en decoracion:
  daba igual que un driver declarase la capacidad, nunca iba a ganar. Ahora los
  `prefer` redundantes desaparecen -- se han quitado solo donde la capacidad tiene
  un unico proveedor, comprobado capacidad por capacidad, asi que el driver que
  sirve cada comando no cambia -- y los que de verdad desempatan (`tap.coord`,
  `type`, `swipe`, `screenshot`) siguen ahi, con test de regresion que lo fija.
- `--prefer-driver` acepta cualquier driver registrado en vez de solo `auto|axe`,
  y el `usage` del error los lista de verdad en vez de repetir una cadena fija.
  Abrir el flag destapo un fallo que su propia validacion tapaba: `ui swipe`
  fijaba `axe` en cuanto estaba instalado y solo miraba `--prefer-driver` para el
  caso `axe`, asi que cualquier otro valor se habria aceptado y luego ignorado en
  silencio. Un prefer explicito manda ahora, y si el driver pedido no puede servir
  la capacidad se falla nombrandolo (`prefer_driver_unusable driver=<id>`) en vez
  de correr otro sin decirlo -- la misma correccion de rumbo que `target_command_ignored`
  en la v0.9.1.
- `isPhysicalDevice` (44 usos) y `normalizedTargetKind` desaparecen a favor de
  `targetKind()`, que devuelve el enum del router en vez de un booleano. Era un
  `if` binario que asumia "si no es device, es simulador" repartido por todo el
  CLI; ahora es un `switch`, y las guardas sim-only se escriben `!= KindSim` para
  que un tercer tipo de target falle cerrado en vez de colarse por la rama del
  simulador. La grafia publica no se mueve: `target_kind` sigue diciendo
  `simulator`/`device` en la salida, en `MAV_TARGET_KIND` y en los `config.yaml`
  ya escritos en disco.
- El rastro de evidencia del ciclo de vida mejora de paso: `commands.jsonl`
  registraba `driver=<el que se pidio>` y ahora registra `driver=<el que sirvio>`.
  Cuando el enrutado falla del todo no se inventa un driver: anota la capacidad.

## v0.10.1

- Un comando ya no falla cuando el simulador que `target_command` había
  resuelto se apaga por debajo dentro de la ventana del caché. Antes mav
  despachaba contra el dispositivo muerto y fallaba en 0 segundos, sin
  intentar nada, pese a que volver a preguntar habría bastado. Ahora, cuando
  un comando falla **y** una consulta de estado confirma que el simulador no
  está arrancado, mav invalida lo cacheado, reejecuta `target_command` y
  reintenta el comando **una vez** — nunca en bucle —, anunciándolo con
  `target_command_restale` para que un reintento no sea silencioso.
- El camino feliz no paga nada: la comprobación de estado sólo ocurre tras un
  fallo, y se decide por consulta real, no por adivinar el texto del error.
- Sin `target_command` configurado no hay a quién repreguntar, así que el
  error dice `reason=simulator_not_booted` en vez de dejar pasar el stderr
  crudo del driver.

## v0.10.0

- `mav run` ahora reinvoca `target_command` cada ~60s mientras el run está en marcha, como
  señal de vida pura -- no como una nueva resolución. El caché por run de `target_command`
  (2 min de TTL, pensado para una navegación en caliente de comandos sueltos) deja un hueco real
  en `mav run`: un solo paso largo -- un `open` con build, un `exec` que envuelve uno -- puede
  pasar minutos sin que mav despache ningún otro comando, así que nada volvía a tocar
  `target_command` en ese tiempo. Un gestor de pool que reserva su slot por TTL de reloj de pared
  (`simpool lease` es exactamente esto, y a partir de su propia v0.5.0 baja ese TTL por defecto a
  3 minutos) no tenía forma de saber que el run seguía vivo durante ese silencio, y reclamaba el
  slot -- justo la colisión que `target_command` existe para evitar.
- El run nunca cambia de simulador a media ejecución por culpa de una reinvocación: el UDID que
  usa para despachar quedó fijado al principio del run (la resolución que `bindFlowTarget` capturó
  antes del primer paso) y sigue siendo ese durante toda su vida. Si una reinvocación periódica
  resuelve a un UDID distinto, eso significa que algo ya se ha llevado el slot; el run se queda con
  el UDID original y añade un aviso accionable a `logs.txt` en vez de perseguir el nuevo -- cambiar
  de simulador a mitad de run reubicaría la colisión, no la evitaría. Un fallo a mitad de run se
  trata igual: avisado, nunca fatal, la misma forma de "avisa y sigue" que ya tenía la caída de
  emergencia de `target_command` para un solo comando.

## v0.9.2

- La caída de emergencia al simulador arrancado cuando `target_command` falla no llegaba a todos
  los comandos: `resolveConfigTarget` dejaba `simulator_udid` vacío en el `Config` que cada
  comando usa para despachar (axe, idb, xcrun...), y solo `withResolvedTarget` —el código que
  construye los campos de la salida de éxito, no el que decide a qué simulador hablar— aplicaba
  la caída real. `mav doctor` "funcionaba" porque solo necesitaba ese campo para reportar; `mav ui
  tree` y el resto de comandos que arrancan un driver directamente con el UDID de `cfg` recibían
  uno vacío y `axe` rechazaba la llamada (`Missing expected argument '--udid <udid>'`), pese a que
  el propio `target_command_warn` de esa misma respuesta afirmaba "falling back to the booted
  simulator". `resolveConfigTarget` ahora aplica esa caída sobre el propio `Config`, así que todo
  punto de entrada que pasa por ahí (o por `mustLoadConfig`) queda resuelto de verdad, no solo
  reportado. `mav time freeze|travel|scale|status|reset` no llamaba a la resolución en absoluto
  (bug aparte, mismo síntoma) y ahora sí.

## v0.9.1

- `target_command` dejaba de tener efecto en silencio cuando el repo también tenía un
  `simulator_udid` fijado en `.mav/config.yaml` (el pin gana, como debe ser), sin que nada lo
  dijera. Varios repos reales ya tenían un pin de una `mav sim select` anterior, así que añadir
  `target_command` ahí sería configuración muerta y nadie se enteraría. La precedencia no cambia
  —el pin sigue ganando— pero ahora ese conflicto es visible: `target_command_warn` avisa en cada
  comando afectado de que el pin está ganando y qué hacer (quitar el pin o quitar el comando).
  Nunca falla el comando ni lo cuelga: una config ambigua sigue siendo una config que funciona,
  solo que avisada.

## v0.9.0

- Nuevo campo `target_command` en `.mav/config.yaml`: un comando que mav ejecuta para obtener el
  UDID del simulador a usar. Resuelve el uso en caliente con varios simuladores arrancados a la vez
  —decenas de invocaciones sueltas (`mav tap`, `mav swipe`, `mav screenshot`...) sin un punto único
  donde envolver con un pool manager externo. mav no importa ni conoce simpool ni ningún otro pool
  manager: `target_command` solo ejecuta el comando configurado y lee un UDID de su stdout.
- Precedencia: un `--target` explícito en `mav run` (y los `MAV_TARGET_*` que fija en sus hijos de
  matrix) y los `MAV_TARGET_*` puestos directamente en el entorno ganan siempre; un `simulator_udid`
  fijado en config (`mav sim select`) también gana; `target_command` solo entra en juego donde antes
  se caía en silencio al simulador arrancado, y sigue cayendo ahí si falla.
- Se cachea por run igual que la resolución del simulador arrancado
  (`.mav/runs/<run-id>/target-command.json`, mismo TTL de 2 min), así que una navegación en caliente
  lo ejecuta una vez, no una vez por comando.
- Si `target_command` falla o no imprime nada, mav nunca cuelga ni hace panic: cae al comportamiento
  anterior (el simulador arrancado) y añade `target_command_warn=<motivo y siguiente paso>` a la
  salida del comando en vez de fallarlo.

## v0.8.0

- Toda respuesta de exito reporta ahora el objetivo sobre el que se actuo: `udid`, `target_kind`
  y `target_name`. En uso en caliente —un agente llamando al CLI comando a comando— eso permite
  fijar las llamadas siguientes al mismo simulador en vez de adivinarlo. Con varios agentes en la
  misma maquina, adivinar significa conducir la app de otro, y el fallo es silencioso porque los
  taps funcionan y las aserciones pasan.
- Cuando no hay objetivo fijado, el UDID reportado revela a que simulador se cayo por defecto:
  convierte un comportamiento implicito en algo observable.
- La resolucion del simulador arrancado se cachea por run (`booted-simulator.json`, TTL de 2 min).
  Sin ella cada comando pagaba ~0,78 s: 30 comandos pasaban de 23,2 s a 0,9 s.
- Los procesos auxiliares de grabacion de video ya no quedan huerfanos cuando `mav` muere de forma
  abrupta. El reaper de expiracion de lease los recoge, y `evidence start`, `network start` y la
  captura de logs garantizan que exista un vigilante aunque se invoquen sueltos, sin `mav open`.

## v0.7.0

- `mav run` ya no comparte el puntero global `.mav/current-run`: cada invocación crea y posee su propio run.
  Dos agentes trabajando en el mismo repo dejaban de pisarse los procesos y de escribir su evidencia en el
  directorio del otro, un fallo que pasaba en silencio porque las aserciones seguían en verde.
- El paso `open` recibe el run por parámetro en lugar de releer el puntero, así que ya no puede detener
  procesos pertenecientes a otro run.
- `current-run` queda como comodidad para los comandos manuales sueltos (`mav open`, `mav ui tap`, `mav logs`).
- Un flow mantiene un solo run de principio a fin. Antes cada paso `open` abría uno nuevo a mitad de flow,
  lo que dispersaba los artefactos: la evidencia aterriza ahora en un único sitio.
- `--run` con un identificador inexistente falla con `run_not_found` en vez de ignorarse en silencio.

## v0.6.0

- Transparent per-run worker over a private Unix socket, with direct fallback and persistent Baguette/DAP sessions.
- Renewable 15-minute inactivity lease with automatic worker, log, LLDB, time-control and simulator-lock cleanup.
- Single-call action, wait and observation fast path for tap, type, swipe, double tap, drag, drag path and toggle.
- Strict typed flow selectors, spatial/hierarchy predicates, boolean conditions, count assertions, stable waits and tree deltas.
- Flow parameters, target bindings, extraction outputs and configurable retry policies.
- New iOS primitives for gestures, app lifecycle, URLs, location and clipboard.
- Optional simulator wall-clock control through `simtime`.
- Idempotent setup verification for the simtime dylib and Xcode `lldb-dap`.
- Parallel multi-target flow runs with isolated artifacts and aggregate reports.
- Simulator LLDB debugging through `lldb-dap`.

Unknown YAML fields now fail linting. Existing flat `id`, `text`, `value` selectors and `optional: true` remain supported.
