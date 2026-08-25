# Changelog

## v0.12.0

- **`ui erase` funciona en macOS** vaciando el campo con el valor vacío, que no depende de
  acertar cuántos borrados mandar ni de que el campo tenga el foco. Y `ui hideKeyboard` deja
  de fallar: en macOS no hay teclado en pantalla que esconder, así que no hace nada — romper
  ahí obligaría a bifurcar por plataforma un flow compartido, que es justo lo que los
  perfiles evitan.
- **baguette dejaba de declararse en iOS pero seguía compitiendo en macOS**, así que
  `ui erase` reportaba `driver=baguette` en un Mac: un éxito de una herramienta que ni
  siquiera puede tocar esa app.
- El README deja de vender MAV como sólo-iOS, con la sección de macOS entera: drivers y por
  qué hacen falta dos, puesta en marcha, captura de red, control de tiempo, y lo que no se
  puede hacer allí.

- **Captura de red en macOS, entera.** `mav network start` levanta mitmproxy y **apunta el
  sistema al proxy él solo** (`networksetup`, sin sudo) sobre el servicio por el que sale la
  ruta por defecto — no el primero de la lista, que suele ser un interfaz virtual de una VM
  y no da error, simplemente no captura nada. `network stop` y `mav stop` lo restauran, y el
  estado previo va al directorio del run porque start y stop son invocaciones distintas: un
  run que muere no puede dejar la máquina apuntando a un proxy muerto. Si el CA de mitmproxy
  no está confiado, el comando lo dice con el `security add-trusted-cert` exacto, porque sin
  eso el HTTPS sale como túneles CONNECT sin contenido: una captura que parece funcionar y
  no sirve.
- **Control de tiempo en macOS**, con lo que se puede y sin fingir lo que no. `mav time
  travel --to` y `mav time reset` mueven el reloj de la máquina; `freeze` y `scale` fallan
  diciendo que un reloj de sistema corre y no se puede parar ni acelerar. Cerrado por
  defecto fuera de una VM (`kern.hv_vmm_present`), forzable con `--system-clock`: en iOS
  simtime interpone el reloj que ve la app, y en macOS la única vía por proceso es
  libfaketime con `DYLD_INSERT_LIBRARIES`, que el hardened runtime bloquea en cualquier app
  firmada para distribuir.
- **`mav location` en macOS explica por qué no puede**, en vez de un "unsupported" pelado
  que invita a buscar durante una tarde: el "Simulate Location" de Xcode no es del
  depurador, viaja por el canal DVT —que es de dispositivos iOS— y contra una app de macOS
  no hace nada; lldb no tiene comando equivalente; las herramientas que existen falsean un
  iPhone conectado, no el Mac.

- **Un fallo ya sale con código 1.** Todos salían con 0: la línea `fail code=...` se escribía
  y el proceso decía que todo había ido bien, así que `mav ui tap ... && siguiente-paso`
  encadenaba después de un fallo y cada agente tenía que leer stdout para saber si su
  propio comando había funcionado. `main` ya sabía salir con 1; lo que faltaba era que el
  fallo llegara hasta él.
- Y la salida se escribe **siempre**, también al fallar: `mav ui` la bufferizaba y la
  descartaba si había error, dejando al usuario sin nada que leer justo cuando más falta
  hace.
- **`mav` arranca solo el demonio de CuaDriver** si no está en marcha, con `open -g` para
  no robar el foco. Antes cada comando moría con un mensaje que exigía saberse el conjuro
  — y un agente que se lo invente arrancando `cua-driver serve` suelto pierde la
  atribución de permisos, que es todo lo que aporta el broker. Un intento por proceso: si
  no levanta, insistir sólo convierte un fallo claro en una sucesión de esperas.
- **`report.json` ya no emite un veredicto sobre una captura que nunca existió.** Todo
  flow traía `"screenshot_evidence":{"ok":false}`, indistinguible de una captura rota para
  quien lee el JSON. Ahora el campo está ausente cuando no hay nada que validar.
- Tres comandos que fallaban en macOS por puertas con forma de iOS: **`clipboard`** daba
  `unsupported_on_device` cuando el driver ya lo servía, **`ui type`** forzaba `axe` como
  driver, y **`ui swipe`** mandaba a instalar `axe|idb`. Ahora los tres funcionan; el swipe
  se traduce a scroll con la dirección invertida, para que un flow escrito una vez
  signifique lo mismo en las dos plataformas.

- **`mav ui tap` no funcionaba en macOS aunque todo estuviera instalado.** La puerta
  preguntaba por `axe`, que en el Mac no se instala nunca, así que fallaba con
  `tool_missing` pidiendo una herramienta que además ya no es la única que sirve input
  allí.
- El input va ahora por **cua-driver** y axcli queda detrás, y no por entregar mejor:
  `cg-pid` sintetiza un evento de ratón y hay botones SwiftUI que lo aceptan **sin
  reaccionar**. Medido contra el onboarding de una app real: el tap reportaba éxito y la
  pantalla no avanzaba. El click de cua-driver va por AXPress cuando el elemento lo
  expone, y sobre esos mismos botones sí surte efecto.
- **Una clave desconocida dentro de un perfil ahora es un error** (`profile_unknown_key`)
  en vez de ignorarse. Escribir `fixture:` en un perfil no hacía nada, y desde fuera eso
  es indistinguible de que se aplicara sin efecto. Se acota a los perfiles a propósito:
  son nuevos, así que ninguna configuración existente puede romperse.
- Las ventanas se piden **por pid**: sin pid, cua-driver enumera sólo la capa 0 — para no
  inundar al llamante con tooltips, popovers, menús y el Dock — y una app cuya UI entera
  vive en una ventana accesoria parece cerrada. Requiere el arreglo propuesto aguas
  arriba en trycua/cua (issue #1451, regresión del PR #1452 al portar a Rust); sin él,
  una ventana flotante falla con un mensaje que lo dice.

- **El driver de macOS pasa a ser cua-driver** (trycua/cua, MIT), y salen
  peekaboo y la captura de axcli. El motivo es estructural: macOS concede
  Accessibility y Screen Recording **sólo a procesos GUI interactivos**, así que
  un CLI no puede tenerlos por mucho que se los des a la terminal. La única
  arquitectura que funciona es un broker —una app con los permisos y un
  socket—, y cua-driver la trae de serie: el binario que invocamos vive dentro
  de `/Applications/CuaDriver.app`. Además da árbol con geometría, captura de
  ventana e input en segundo plano **en una sola herramienta**, y su árbol y su
  captura salen de la misma llamada, así que describen el mismo instante.
- Medido dentro de una VM contra una ventana flotante, que es donde se cayó
  todo lo anterior: peekaboo la enumeraba y la descartaba sola por la capa;
  axcli leía el árbol pero su captura devolvía **el escritorio** recortado a las
  medidas de la ventana, sin error, y encima activaba la app. cua-driver
  devolvió el contenido real y el click llegó en segundo plano.
- axcli se queda **sólo como escotilla**: cua-driver resuelve la ventana por
  `list_windows`, que es *layer-0 only* por diseño declarado, así que una UI
  flotante —panel, HUD, popover, un onboarding— le resulta invisible. axcli
  apunta por `--app` y no necesita window id. Cuando no hay ventana que
  resolver, el error lo dice con esas palabras en vez de parecer "la app no
  está abierta".
- Dos cosas que se pierden y conviene saber: sus elementos **no exponen
  AXIdentifier** (sólo `element_token`, válido dentro del snapshot), y ya no hay
  menús ni gestión de ventanas al nivel que daba peekaboo.
- `mav doctor` reporta los permisos preguntando **al demonio**, que es quien los
  tiene, y el `next` pasa a ser `cua-driver permissions grant` — el único flujo
  de los probados que registra la app en los paneles solo, en vez de exigir
  añadirla a mano con el "+".

- **Captura por app en macOS: `peekaboo image` ya no existe.** El subcomando se eliminó
  en Peekaboo v4 y el driver estaba escrito contra la 3.0.0 de la máquina de desarrollo,
  así que cualquier instalación hecha hoy por `brew` fallaba con `INVALID_ARGUMENT`. Pasa
  a usar `peekaboo see --path`, que además devuelve árbol y captura en la misma llamada:
  las dos evidencias corresponden al mismo instante en vez de a dos momentos distintos.
- **`mav capture` ignoraba `--prefer-driver`.** El flag se aceptaba en la línea de
  comandos y se tiraba a la basura: la captura encaminaba siempre por coste, así que la
  única forma de esquivar un driver roto era desinstalarlo. Ahora se respeta, y el
  `idb` implícito de los dispositivos físicos sólo se aplica cuando no pediste nada.
- **macOS.** `mav` deja de ser sólo iOS. `target_kind: macos` es un tipo de destino de
  primera clase, con tres drivers nuevos: peekaboo (árbol de accesibilidad, menús,
  capturas de ventana), axcli (input que **no roba el foco**, vía `CGEventPostToPid`) y
  el `screencapture` del sistema (vídeo, y captura de pantalla entera como último
  recurso). Los logs y los crashes salieron casi gratis: `log stream` del host es la
  misma línea que en simulador quitando el `simctl spawn`, y los `.ips` del Mac son el
  mismo formato JSON que los de iOS, así que `ParseIPS` sirve sin tocar nada.
- Hacen falta **dos** drivers de input y no uno porque peekaboo no puede entregar
  eventos a un PID: o activa la app de destino —saltando de Space si hace falta— o
  dispara a lo que esté delante. `--no-auto-focus` no lo arregla, sólo empeora la
  puntería. El reparto entre los dos se expresa con `Cost(cap, target)`, que ya es por
  capacidad y por target, en vez de con un rasgo nuevo en la interfaz `Driver` que
  habría obligado al router a cablear qué capacidades son "input".
- **Perfiles de plataforma** en `.mav/config.yaml`: un bloque por plataforma que
  sobreescribe `app_target`, la receta de lanzamiento, `process_name`, `target_command`
  y los campos de log. Se selecciona con `--profile`, `MAV_PROFILE` o `default_profile`,
  en ese orden; un perfil pedido que no existe falla nombrando los válidos en vez de
  caer a la base en silencio. Son un overlay sobre los campos planos, no un sustituto:
  un repo de una sola plataforma no escribe ninguno y no cambia nada para él.
- **Fixtures**: estados con nombre —listas de comandos— que dejan la app en una
  situación conocida. Corren entre `install` y `launch`, que es la única ventana donde
  el contenedor ya existe y nada tiene su base de datos abierta; por eso mismo la app se
  cierra antes de sembrar. Componen con `--clear-state` (borra el contenedor, el fixture
  siembra encima), están disponibles como `fixture:` en el paso `open` de un flow, y el
  aplicado se registra en `report.json` — un run cuya evidencia no dice de qué estado
  partió no es reproducible.
- `--fixture` se rechaza junto a `--no-relaunch`, igual que `--clear-state` y por el
  mismo motivo: `--no-relaunch` se salta la receta entera, así que el fixture no
  llegaría a correr y el agente acabaría validando contra datos que nadie sembró.
- Un `target_kind` desconocido ya no falla abierto. `targetKind()` mandaba a `KindSim`
  todo lo que no fuera el literal `"device"`, así que un `target_kind: macos` escrito a
  mano antes de que existiera se comportaba como simulador de principio a fin: ese run
  resolvía un lease de simpool, arrancaba un iPhone simulado, y con `--clear-state`
  desinstalaba de él la app usando un `bundle_id` que en una app multiplataforma es el
  mismo en las dos. Ahora la validación corre al final de la carga, cuando ya se
  aplicaron el perfil y `MAV_TARGET_KIND`, porque las tres fuentes tienen que pasar por
  el mismo filtro.
- Un `clear_state` que no llega a desinstalar ya no se descarta en silencio. No aborta
  el `open` —el caso corriente es que la app aún no estuviera instalada— pero sale como
  `clear_state_warn`, siguiendo el mismo "avisa y sigue" de `target_command_warn`. Sin
  eso, `--clear-state` mentía: el usuario creía partir de cero y arrastraba el estado
  del run anterior.
- **Ciclo de vida de macOS**: lanzar, cerrar, `openURL`, portapapeles y el equivalente
  honesto de `--clear-state` (borra el contenedor y los preferences, no la app). El
  lanzamiento ejecuta `Contents/MacOS/<binario>` en vez de `open` porque `open` no propaga
  variables de entorno, y el entorno es como mav inyecta su configuración.
- `mav doctor` reporta el estado de Accessibility y Screen Recording en macOS, diciendo a
  quién hay que concederlos: al proceso que ejecuta `mav` —tu terminal o el harness del
  agente— no a `mav`.
- Los perfiles admiten `runner: local|crabbox`, que declara **dónde** corre ese perfil.
  mav no orquesta máquinas: la receta completa para una VM desechable está en
  `examples/macos-vm/`.
- `SaveConfig` pasa a usar `yaml.Marshal` en vez de un escritor a mano. El escritor
  omitía los valores vacíos, así que el fichero no podía expresar "presente y vale
  cadena vacía" — justo lo que un perfil necesita para **anular** un comando heredado.
  Y ahora rechaza escribir una config con perfil aplicado: guardarla aplanaría el perfil
  sobre la base, y un `mav sim select` en un repo con `default_profile` habría dejado el
  `app_target` de macOS como base, en silencio y sin vuelta atrás.

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
