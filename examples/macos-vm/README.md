# Correr `mav` contra una app de macOS dentro de una VM desechable

Esta es la Fase 4 del [análisis de scope](../../docs/macos-scope-evaluation.md), y lo
importante es lo que **no** hay aquí: código de MAV. MAV no orquesta máquinas. Envuelve
drivers y produce evidencia; la máquina la pone [crabbox](https://github.com/openclaw/crabbox),
que ya sabe alquilar una VM de macOS con `tart`, sincronizar el checkout sucio, ejecutar y
devolverla al acabar.

## Por qué molestarse

En tu propio Mac, validar una app de macOS tiene dos problemas que no se arreglan con
código:

1. **TCC.** Una app real pide varios permisos (micrófono, calendario, Apple Events…). O ya
   se los concediste —y entonces tu test no parte de limpio— o te comes un prompt a mitad
   del run. Y Screen Recording y micrófono son *deny-only* en PPPC: ni un admin de MDM
   puede pre-autorizarlos.
2. **Estado.** `--clear-state` borra el contenedor de la app, pero no el resto de rastro
   que deja en el sistema.

En una VM cuya imagen fabricas tú, el segundo desaparece: tiras la VM y el estado se va con ella.

El primero **no**, y esto contradice lo que dice la mayoría de guías sobre el tema.

### Sembrar `TCC.db` NO funciona en macOS 26

Probado en una VM real de macOS 26.0 con SIP desactivado, y falla en las cuatro variantes:

| Intento | Resultado |
|---|---|
| `INSERT` por ruta, `client_type=1` | ignorado |
| Identificador de firma, `client_type=0`, sin `csreq` | ignorado |
| Identificador de firma, `client_type=0`, **con `csreq` válido de 172 bytes** | ignorado |
| Lo anterior sobre toda la cadena de proceso responsable (`bash`, `zsh`, `ssh`, `sshd`) | ignorado |

Con `tccd` reiniciado en cada intento, y con un reinicio completo de la VM al final. `csrutil status`
confirmaba `disabled` todo el rato, y las filas quedaban en la base con `auth_value=2`.

Las guías que dicen que esto funciona —y el orb de CircleCI— son de macOS anteriores. En macOS 26,
escribir en la `TCC.db` del sistema deja de bastar aunque SIP esté desactivado.

### Lo que sí funciona: conceder por el ratón virtual del hipervisor

El permiso no se puede conceder **desde dentro** de la VM: cualquier forma de pulsar el botón de System
Settings necesitaría ya el permiso de accesibilidad que intentas conceder. Es circular.

Pero sí **desde fuera**. La VM tiene teclado y ratón virtuales a nivel de hipervisor
(`configuration.keyboards` y `configuration.pointingDevices` en `VM.swift` de tart), y tart expone un
servidor VNC contra ellos (`tart run --vnc-experimental`, que además funciona antes del login y en modo
recuperación). Para el guest, esos eventos son **hardware**, no eventos sintéticos de un proceso — y TCC
sólo gobierna los sintéticos.

Verificado en una VM de macOS 26.0 con cero permisos concedidos:

```sh
tart run mav-macos-test --no-graphics --vnc-experimental
# imprime: VNC server is running at vnc://:<password>@127.0.0.1:<puerto>

vncdo -s 127.0.0.1::<puerto> -p <password> capture pantalla.png   # → framebuffer completo
vncdo -s 127.0.0.1::<puerto> -p <password> move 52 28 click 1     # → abre el menú Apple
```

La captura salió (2,5 MB de escritorio real) y el click abrió el menú. Desde ahí, `System Settings…` está
en ese mismo menú: el camino entero hasta activar la casilla de Accesibilidad es clickable por este canal.

**Consecuencia**: el bootstrap de permisos SÍ es automatizable, y no hace falta una imagen distinta por
combinación de permisos. Se conduce la primera concesión por VNC, y a partir de ahí el guest ya puede
usar sus propias herramientas.

Ni crabbox ni Peekaboo hacen esto: crabbox no menciona TCC en toda su documentación, y la de Peekaboo
dice explícitamente que hay que concederlo a mano.

## La receta

**Requisito de versión, y es la trampa que más tiempo cuesta**: hace falta **tart >= 2.29** (probado con
2.35.0). En 2.28.1 `tart exec` calcula el tamaño del terminal **siempre**, con un `try!` sin guard, así que
revienta cuando no hay TTY:

```
tart/Exec.swift:91: Fatal error: 'try!' expression unexpectedly raised an error:
failed to get terminal size: Inappropriate ioctl for device
ssh key injection failed
```

crabbox usa `tart exec` para inyectar la clave SSH, así que con tart 2.28.1 **el provider entero no
arranca desde ningún contexto no interactivo** — que es justo para lo que existe. En `main` está envuelto
en `if tty` y ya no pasa. No es un bug que reportar: es actualizar.

`.crabbox.yaml` en la raíz del repo (ver `crabbox.yaml.example` aquí al lado):

```yaml
jobs:
  mav-macos:
    provider: tart
    target: macos
    idleTimeout: 30m
    shell: true
    command: >
      export PATH=/usr/local/bin:/opt/homebrew/bin:$PATH &&
      mav doctor &&
      mav --profile mac run flows/smoke.yaml
    stop: always
```

La imagen se elige con `CRABBOX_TART_IMAGE` o `--tart-image`. Comprobado end-to-end con
`ghcr.io/cirruslabs/macos-tahoe-xcode:latest`: crabbox alquila la VM, inyecta la clave, sincroniza el
checkout sucio, ejecuta, y libera el lease al terminar. Cuando el comando falla deja un bundle de fallo
local y sugiere los `crabbox ssh` / `run --id` / `stop` concretos para retomar sobre el mismo lease.

### Quién monta qué

La separación de responsabilidades es de crabbox, no invención nuestra — su propia documentación la fija:
*"Crabbox owns the lease lifecycle, sync, execution and cleanup. The repository owns the command string,
package-manager setup, test environment"*.

| Capa | Quién | Qué monta |
|---|---|---|
| Imagen | `scripts/build-mav-vm-image.sh`, una vez | mav, peekaboo, axcli |
| Máquina | **crabbox** | lease, sync del checkout, ejecución, limpieza |
| App | **mav** (`fixtures`) | el estado de la app antes de lanzarla |
| Permisos | quien prueba, contra su app | TCC — ver abajo |

Ojo con la palabra "fixture": crabbox no la usa para esto. Su `warmup`/`prewarm` prepara **la caja**, no
la app. El estado de la app es cosa de mav. E instalar mav y los drivers **no va en un hook de crabbox**:
va en la imagen, o acabas reinstalándolo en cada run.

### Por qué la imagen NO trae permisos concedidos

Se podría hornear Accessibility y Screen Recording, porque van a las *herramientas* y son las mismas
siempre. Pero la app bajo prueba pide los suyos —Nokoru quiere micrófono, calendario y Apple Events— y
esos cambian con cada app. Una imagen "con permisos" sería **una imagen por app**, que es justo lo que no
queremos.

Así que la imagen trae herramientas y nada más. Los permisos se conceden después, contra la app concreta,
por el canal del hipervisor descrito arriba. Eso mantiene una sola imagen sirviendo a cualquier app.

## Lo probado de verdad, y dónde para

Ejecutado contra NokoruMac en una VM de macOS 26.0:

| Paso | Resultado |
|---|---|
| Imagen con mav + peekaboo + axcli | ✅ construida y verificada |
| crabbox: lease, sync, run, cleanup | ✅ end-to-end |
| App corriendo dentro de la VM | ✅ |
| Fixture (`vacio` borra el contenedor) | ✅ la app arrancó mostrando el onboarding de primer uso |
| Accessibility + Event Synthesizing | ✅ **concedidos por el canal del hipervisor, sin tocar el host** |
| Screen Recording | ❌ ver abajo |
| `mav ui tree` / `mav capture` | ❌ bloqueados por lo anterior |

### Tres cosas que salieron al ejecutarlo

**1. Una app firmada para desarrollo no arranca en la VM.** NokoruMac lleva `embedded.provisionprofile`
y entitlements restringidos (iCloud, push) atados a un equipo y unos dispositivos. En una VM limpia,
AMFI la mata con SIGKILL sin mensaje. Para validar UI no hacen falta esos entitlements: re-firmar ad-hoc
(`codesign -f -s - --deep`) la deja arrancar. Es un compromiso consciente — se pierde iCloud y push — y
hay que decirlo, porque ya no estás probando exactamente el binario que distribuyes.

**2. `mav ui tree` en macOS necesita los DOS permisos, no sólo Accessibility.** `peekaboo see` enumera
ventanas con ScreenCaptureKit, así que sin Screen Recording falla con `WINDOW_NOT_FOUND` aunque
Accessibility esté concedida. Su propio log lo dice: *"rejected onDemand host … missing Screen
Recording"*.

**3. Screen Recording no se puede conceder a un binario CLI por la vía normal.** A diferencia de
Accessibility —donde macOS registró `sshd-keygen-wrapper` solo y bastó activar el interruptor— el panel
de Screen Recording aparece vacío y su botón "+" abre un selector pensado para `.app`. Provocar la
petición desde un proceso sin GUI no registra ninguna entrada.

Vías que quedan por explorar, ninguna probada aún: conceder a `Terminal.app` y conducir desde una
terminal dentro de la sesión gráfica en vez de por SSH; o capturar la pantalla **desde fuera** por el
propio VNC del hipervisor, que no necesita permiso alguno — la evidencia visual de esta prueba se obtuvo
justo así.

**4. La captura acotada a la app choca además con un filtro de Peekaboo v4, no sólo con TCC.** Corriendo
en la sesión gráfica, `peekaboo see --app nNokoru` enumera las ventanas candidatas y las descarta una a
una diciendo por qué:

```
Desktop observation target was not found: shareable window for nNokoru.
Candidates: #2 id=107 '<untitled>' 640x640 alpha=1.00 reason=layer != 0
```

`id=107` **es** la ventana visible de la app. Peekaboo sólo acepta ventanas en la capa 0, y el onboarding
de Nokoru es una ventana flotante. O sea: cualquier app cuya UI viva en un panel flotante, un HUD o un
popover queda fuera de la captura por app de Peekaboo aunque los permisos estén perfectos. `axcli
snapshot` lee el árbol de esa misma ventana sin problema (9 elementos interactivos, botón "Get started"
incluido), así que el árbol y la captura no fallan por lo mismo y no se arreglan igual.

**5. Ejecutar `mav` por SSH lo deja en la sesión equivocada.** Un proceso lanzado desde SSH no está en la
sesión Aqua: `screencapture` responde `could not create image from display` porque no ve pantalla
ninguna. `sudo launchctl asuser 501 …` sí entra en la sesión gráfica, pero entonces se pierde la
atribución TCC que le daba el bridge de Peekaboo.app y la respuesta pasa a ser `Screen Recording
permission is required`. Son dos fallos distintos con la misma causa de fondo — la identidad del proceso
responsable — y ninguno se arregla concediendo más permisos a `mav`.

**7. La captura por app dentro de la VM sólo funciona por el bridge de Peekaboo.app, y el fallo
alternativo es silencioso.** Concedido Screen Recording a `axcli` —se puede, aunque el panel no registre
el intento denegado: hay que añadirlo con "+" y escribir la ruta a mano—, `axcli screenshot` reporta
éxito, escribe el PNG y acierta las medidas de la ventana (`Capturing window 640x640 at (192,52)`). Lo
que hay dentro del PNG es **el fondo de escritorio**. Con `--legacy` igual. Peekaboo, sobre la misma
ventana y en el mismo instante, devuelve el contenido real. La diferencia es que su CLI delega en
Peekaboo.app, que vive en la sesión gráfica, mientras que axcli captura desde su propio proceso — y ese
proceso, lanzado por SSH, no tiene sesión gráfica. Control con TextEdit: mismo resultado, así que no es
cosa de la app bajo prueba.

Que un driver devuelva un PNG plausible en vez de un error es peor que fallar: por eso axcli queda como
escotilla de escape (`--prefer-driver axcli`) para las ventanas flotantes que Peekaboo rechaza, y no como
camino por defecto.

**6. `cg-pid` puede fallar en silencio al pulsar, y aquí lo hizo.** El riesgo anotado como hipótesis en el
plan quedó demostrado: `axcli click --strategy cg-pid "text=Get started"` reportó éxito
(`cg-pid click pid=1740 wid=107 screen=(512,641)`) y el onboarding **no avanzó** — seguía en "Step 1 of
5". El mismo clic con `--strategy ax` (AXPress) avanzó a "Step 2 of 5" a la primera. Para botones SwiftUI
conviene AXPress; para lo demás, `cg-pid` sigue siendo lo que no roba el foco. Es la razón por la que
`mav ui tap --verify` existe: sin comprobar el efecto, un tap que no hizo nada se reporta como `ok`.

## Lo que no funciona todavía

`crabbox run --artifact-glob` **rechaza los targets nativos de macOS**, que es justo el
mecanismo con el que sacarías `.mav/runs/<id>/` de la VM. Está identificado aguas arriba en
[crabbox#1393](https://github.com/openclaw/crabbox/issues/1393). Mientras tanto:

```sh
crabbox warmup --provider tart          # imprime el slug del lease
crabbox run --id <slug> -- mav run flows/smoke.yaml --profile mac-vm
rsync -a "$(crabbox ssh --id <slug> --print-target)":.mav/runs/ ./.mav/runs/
```

La salida de `mav` en sí no necesita nada de esto: su línea `ok cmd=… k=v` vuelve por
stdout tal cual. Lo que se queda dentro es la evidencia visual.

## Dos límites que conviene saber antes de montarlo

- **Máximo 2 VMs de macOS concurrentes.** Es límite de `Virtualization.framework` *y* del
  EULA de macOS, no se salta con más RAM. Por eso el leasing de crabbox pasa de cómodo a
  obligatorio.
- **El provider `tart` de crabbox no expone `--audio`.** Si lo que validas necesita
  micrófono, esa VM no lo tendrá aunque tart sí sepa hacerlo.
