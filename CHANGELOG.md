# Changelog

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
