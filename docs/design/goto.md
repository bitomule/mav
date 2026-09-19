# `mav goto` — diseño

Nodo `mav-find-goto`, 19 sep 2026. **No hay código de `goto` escrito.** Esto es un diseño, y
entra en la misma PR que `mav ui find`, que sí está construido y medido.

`goto` es `find` en bucle: mirar la pantalla, decidir qué tocar para acercarse, tocar, volver a
mirar, hasta llegar o rendirse. Un bucle autónomo que pulsa botones en un simulador con la
sesión de David es lo más peligroso que se ha construido aquí, así que el diseño empieza por
cuándo para y qué no toca nunca, no por cómo avanza.

---

## 0. La premisa que me dieron era falsa, y hay que decirlo primero

Se me pasó como hallazgo establecido que **`browser-use`** —un agente que conduce un navegador
en bucle, el mismo patrón— ya había resuelto la parte difícil:

> *"No le pregunta al modelo si ha llegado. El modelo puede decir que terminó, pero quien lo
> confirma es código, comprobando el resultado real."*

**Leído su código fuente, eso no es lo que hace.** Lo comprobado, con fichero y función:

- **Llegar lo declara el modelo, y nadie lo verifica.** `Tools._register_done_action` en
  `browser_use/tools/service.py` devuelve `ActionResult(is_done=True, success=params.success,
  ...)`. `success` es literalmente el valor que pasó el modelo. El bucle termina en
  `self.history.is_done()` sin comprobar nada.
- **El único verificador independiente es otro modelo, y llega tarde y sin poder.**
  `browser_use/agent/judge.py` mira la tarea, el texto del `done` y las últimas 10 capturas de
  pantalla. Su propio código lo dice: *"The judge verdict is attached to the action result but
  does NOT override last_result.success — that stays as the agent's self-report."* Corre
  **después** de que el agente ya haya parado.
- **Que un paso salga bien tampoco lo comprueba código.** Un paso "va bien" si el manejador no
  lanzó excepción. La única comprobación real entre acciones (`Agent.multi_act`) compara
  **la URL** y el id del target enfocado, y su propósito es abortar las acciones encoladas, no
  juzgar si funcionó. Juzgarlo es un campo de texto que rellena el modelo:
  `AgentBrain.evaluation_previous_goal`.

**Conclusión, y es la mala:** el problema duro —confirmar la llegada sin que el modelo se
califique a sí mismo— **no está resuelto por otros**. No se puede copiar. Es lo que hay que
inventar, y el diseño de abajo lo inventa sabiendo que no tiene prior art detrás.

Lo que **sí** se copia de ellos, y verbatim, está en §5.

---

## 1. Cuándo para

Dos topes, los dos, y los dos duros. Ninguno es configurable hacia arriba desde el flujo YAML:
un bucle que se puede desbloquear pidiéndolo no es un tope.

| condición | valor | qué pasa |
|---|---|---|
| pasos | **12** | para, `outcome=exhausted` |
| tiempo total | **90 s** | para, `outcome=timeout` |
| pasos sin que cambie la pantalla | **2** | para, `outcome=stuck` |
| toques repetidos sobre la misma huella | **2** | para, `outcome=looping` |
| `find` se abstiene | **2 seguidas** | para, `outcome=no_route` |
| pantalla destructiva por delante | inmediato | para, `outcome=refused` |

12 pasos porque una pantalla de una app iOS está a 1–4 toques de la raíz; 12 es holgado y
sigue siendo un número que se agota en menos de dos minutos. `browser-use` usa 500, que es
razonable en un navegador donde navegar es gratis y reversible, y no lo es aquí.

**El escalón intermedio de `browser-use` sí se copia**, porque es el que evita que un bucle
muera sin contar nada: a los 3 fallos seguidos mete un aviso de replanteo en el contexto, a los
5 le quita todas las herramientas menos `done`, y a los 6 corta. La forma que importa es la
última: **el último paso siempre es un paso de informe**, no un corte en seco. `goto` termina
siempre emitiendo dónde se quedó y qué veía, aunque se haya rendido.

---

## 2. Cómo sabe que ha llegado

Si se lo preguntas al mismo modelo que eligió el camino, se está calificando a sí mismo. Y
`browser-use` no tiene respuesta a esto (§0). La primera versión de esta sección proponía un
"juez ciego" —una segunda llamada a jev que recibía sólo el árbol final y "¿qué pantalla es
ésta?", sin el objetivo— y **se ha descartado tras revisarla**. Dos razones, y la segunda es
la que la mata:

- **La lista de candidatos filtra el objetivo.** El juez tiene que elegir entre nombres de
  pantalla, y esos nombres salen de algún sitio. Si se generan a partir del objetivo, los
  distractores son de paja y el juez acierta siempre sin medir nada. Si se sacan del árbol
  final, está leyendo el título de la barra de navegación, que `strings.Contains` resuelve
  gratis.
- **La ceguera es de prompt, no de evidencia.** El juez y el navegador son el mismo modelo
  leyendo el mismo árbol. Si el árbol engaña —título genérico, pantalla de carga— los engaña
  igual a los dos. Son errores correlacionados, no una segunda opinión.

Y lo decisivo: `arrived=unverified` con `juez=coincide` sólo tiene dos lecturas, y ninguna
sirve. O quien llama se lo cree, y entonces es `arrived=true` con un modelo calificando —justo
lo que se quería evitar—, o no se lo cree, y entonces es ruido.

**Lo que hace falta no es un segundo juez: es quitarle al navegador la capacidad de declarar
"hecho".** La autocalificación se elimina estructuralmente, no por prompt.

> **Nota para quien lea esto dentro de seis meses: el juez ciego se va a volver a proponer.**
> Es la idea que sale sola cuando piensas el problema —"pues que lo confirme otra llamada sin
> contexto"— y por eso queda escrito aquí como **rechazo razonado** y no sólo como un diseño
> que resultó ser otro. Los dos argumentos que lo matan son independientes, y hay que
> refutarlos **los dos** para reabrirlo:
>
> 1. **La lista de opciones filtra el objetivo por construcción.** No es un problema de cómo
>    escribas el prompt: el juez tiene que elegir entre algo, y ese algo procede del objetivo
>    (distractores de paja) o del árbol que está mirando (pregunta trivial). No hay tercera
>    fuente.
> 2. **Juez y navegador son el mismo modelo leyendo el mismo árbol.** La ceguera es de prompt,
>    no de evidencia. Un árbol que engaña —título genérico, pantalla de carga a medio poblar—
>    los engaña igual a los dos, así que sus errores están correlacionados y no son una segunda
>    opinión.
>
> Y aunque se salvaran los dos, sigue sin haber una acción distinta detrás:
> `arrived=unverified` con `juez=coincide` o se cree —y entonces es `arrived=true` con un
> modelo calificándose— o no se cree, y entonces no cambia nada de lo que haría quien llama.

### La ruta, que es lo más parecido a una URL que tiene iOS

El navegador **nunca termina**. Sólo devuelve un elemento o se abstiene. Quien termina es
código, y compara **rutas**, no etiquetas sueltas:

```
ruta = ( pestaña con el trait "selected",
         último título de navigation bar,
         título de alert o sheet si hay una encima )
```

Se calcula por roles y traits del árbol, nunca buscando texto libre en cualquier nodo. Ésa es
la diferencia que arregla el fallo de abajo.

### Las tres comprobaciones, y la primera existe por un fallo encontrado en revisión

1. **Negación de origen, y esto era un agujero real.** La versión anterior comprobaba si el
   texto de `--arrived-when` estaba *en el árbol*. Con objetivo `Notificaciones` y la lista de
   Ajustes delante, **esa palabra ya está en la pantalla de partida**: el bucle declaraba
   llegada en el paso 0 sin tocar nada. Lo mismo con una barra de pestañas, cuyas etiquetas
   están en todos los árboles, y con un botón Atrás que lleva el nombre de la pantalla
   anterior.

   Así que: **el criterio tiene que estar AUSENTE de la ruta inicial.** Si ya está presente al
   empezar, `goto` **rechaza el comando** —`outcome=ambiguous_criterion`, "ese criterio ya se
   cumple en la pantalla de origen"— en vez de declarar una llegada que no ocurrió.

2. **Negación de modal.** No hay llegada si hay un `alert` o un `sheet` encima, salvo que el
   criterio sea precisamente ese modal. Es también lo que detecta al bucle parándose sobre un
   paywall, y lo hace código leyendo un rol, no un modelo.

3. **Quiescencia.** Dos lecturas separadas unos cientos de milisegundos con la misma huella.
   Durante un push el árbol contiene nodos de las dos vistas a la vez —el título nuevo ya
   está y la huella ya cambió—, y una pantalla de esqueleto cambia al poblarse. Sin esto la
   llegada es real y la pantalla no está lista.

### Lo que sigue sin cubrirse, y se dice en la salida

**Pantallas parametrizadas.** Objetivo "Detalle del pedido 123", el bucle toca la primera fila
y abre el pedido 456: la huella cambia, el título coincide, todo pasa. Pantalla correcta,
instancia incorrecta. Por eso `--arrived-when` acepta varios términos y los exige todos:

```
--arrived-when 'title:"Detalle del pedido" text:"123"'
```

Sin varios términos ese caso queda abierto, y la salida tiene que decirlo en vez de dejar que
se descubra.

### Y lo que emite

**Evidencia, no veredicto.** `mav` es un verificador: devuelve lo que vio y quien llama juzga,
porque quien llama es casi siempre otro agente con más contexto que un jev sin él. Salen la
ruta inicial, la ruta final, la huella, la lista de toques y si hubo abstención. `arrived` es
`true` sólo cuando un criterio declarado se cumplió contra la ruta; sin criterio declarado es
`unverified` y nunca `true`.

## 3. Cómo detecta que da vueltas

**Contar nodos no vale, y esto está medido hoy, no razonado.** En el slot-5 de simpool, sobre
Ajustes de iOS: 80 nodos antes del toque, 80 nodos después, y la pantalla había cambiado entera
(de la lista de Ajustes a los ajustes de Cámara). Un contador habría dicho "no tocó nada". Es,
además, el método con el que está escrita la tabla de cinco intentos del **issue #90**, lo cual
merece una nota en ese issue.

La huella es **el conjunto de identidades de los elementos**, no su número:

```
fingerprint = sha256( sorted( (id, label, role) por cada elemento del árbol sin recortar ) )
```

Tres decisiones dentro de eso:

- **Sin `frame`.** Las coordenadas se mueven fracciones de punto entre dos lecturas de una
  pantalla quieta, y una huella que cambia sola no detecta nada.
- **Sin `value`.** Un reloj, un contador o un spinner cambian de valor en cada lectura. Es el
  fallo que avisa `browser-use` con su `PageFingerprint`: si no normalizas, el detector de
  estancamiento no se dispara nunca.
- **Ordenado.** El orden del árbol es determinista hoy, pero ordenar cuesta nada y quita una
  dependencia.

Dos señales sobre esa huella, y las dos **cortan** el bucle, a diferencia de `browser-use`
donde son sólo texto de aviso que el agente puede ignorar:

- **La pantalla no cambió tras un toque** → el toque no hizo nada. Dos seguidos y para con
  `outcome=stuck`. Esto pasa de verdad: hoy, en el mismo slot, `mav ui swipe` devolvió `ok` con
  la huella y los 213 nodos idénticos antes y después. Un bucle que no detecte eso gira hasta
  agotar el tope.
- **Una huella ya visitada vuelve a aparecer** → está dando vueltas. Se guarda la lista de
  huellas del viaje; repetir una es `outcome=looping`.

El **hash de la acción** también se copia de `browser-use`, y con su matiz: se calcula sobre
**la etiqueta y el rol del elemento, nunca sobre su índice**. El índice cambia entre lecturas
del mismo árbol; la etiqueta no.

---

## 4. Y qué no toca nunca

La guarda destructiva de `find` **ya está construida y es la pieza que hace `goto` aceptable**.
No es opcional aquí, y en `goto` es más estricta que en `find` por una razón: en `find` quien
llama lee la respuesta antes de tocar nada; en `goto` nadie lee nada entre la decisión y el
dedo.

- **`goto` nunca toca un elemento destructivo. Punto, y sin la puerta de escape de `find`.**
  En `find`, un objetivo que pide explícitamente borrar sí devuelve el botón de borrar, porque
  quien llama va a leer la respuesta. En `goto` esa puerta no existe: si el único camino pasa
  por un elemento destructivo, **para** con `outcome=refused` y dice cuál era. Llegar ahí es
  decisión de una persona.
- **El léxico es el de `find`** (`IsDestructive` en `internal/mav/uifind.go`), multilingüe y
  deliberadamente amplio: un falso positivo cuesta una parada, un falso negativo cuesta los
  datos de David.
- **Y una lista de bundles fuera de alcance.** `goto` se niega a conducir si el bundle activo
  no es el de la configuración: si un toque saca a Ajustes, a Mail o a SpringBoard, el bucle
  para en vez de seguir pulsando en una app que no es la suya. `mav ui tree` ya expone
  `active_bundle`, así que es una comparación, no trabajo nuevo.
- **Sin recuperación por deshacer.** `browser-use` se saca de un callejón con `go_back` y
  `navigate`, y las dos son maquinaria de URL que aquí no existe. El botón atrás de una app
  puede no estar en un modal o en la raíz de una pestaña. Así que **"no puedo volver" es un
  estado terminal legítimo**, no un caso raro: se para y se informa. Si algún día hace falta
  reponerse, el reinicio es relanzar la app, nunca intentar desandar a ciegas.

---

## 5. Lo que sí se copia de `browser-use`, y lo que no traslada

**Se copia tal cual:**

- **Índices reconstruidos en cada paso.** Rehacen el mapa de selectores desde cero cada vez y
  el modelo sólo nombra un índice válido para el estado que acaba de ver. Es exactamente
  nuestra situación —el árbol de accesibilidad no da manejadores estables— y es la razón de que
  su diseño funcione sin ellos. `find` ya lo hace así.
- **Filtro de candidatos estructural, nunca por parecido al objetivo.** Su serializador no ve
  jamás el texto de la tarea: filtra por *operable y visible*, y nada más. `FindCandidates`
  ya es eso (`isActionable` + tiene texto). El matiz que me llegó —"filtran por compatibilidad
  de operación"— es medio cierto: hay **un solo** conjunto de candidatos, no uno por acción.
- **La escalera de fallos** (3 → replanteo, 5 → sólo `done`, 6 → corte) y **el último paso
  siempre de informe**.

**No traslada, porque depende de la URL o de manejadores estables:**

- **El guardián de cambio de página a mitad de paso**, que es lo que les deja encolar hasta 5
  acciones por llamada al modelo. Sin URL no hay sonda barata entre toques: releer el árbol
  cuesta lo mismo que el paso siguiente. **El puerto honesto es una acción por paso**, que
  cuesta rendimiento y no cuesta corrección.
- **La marca de "elemento nuevo"**, que guardan comparando `backend_node_id` de CDP entre
  pasos. Un árbol de accesibilidad releído no da nada equivalente. Se puede aproximar con una
  clave de contenido, pero falla en listas de celdas idénticas, así que sería una pista en el
  texto de la pregunta y **nunca una comprobación en código**.
- **La URL como mitad de su huella de pantalla.** Se cae; las otras dos mitades (recuento y
  hash del texto) siguen sirviendo, que es lo de §3.

---

## 6. La forma del comando

```
mav goto "<la pantalla que quieres>" [--arrived-when "<id o texto>"] [--max-steps 12] [--timeout 90s]
```

Sale, como `find`, una línea `ok` y un documento JSON:

```json
{
  "arrived": "true | false | unverified",
  "outcome": "arrived | exhausted | timeout | stuck | looping | no_route | refused | out_of_app | ambiguous_criterion",
  "steps": [ {"tapped": {...}, "fingerprint_before": "...", "fingerprint_after": "...", "changed": true} ],
  "route_before": {"tab": "...", "nav_title": "...", "modal": null},
  "route_after":  {"tab": "...", "nav_title": "...", "modal": null},
  "final_screen": {"fingerprint": "...", "candidates": 14},
  "refused_element": null,
  "next": "..."
}
```

**El código de salida no lleva respuestas**, igual que en `find`: 0 siempre que `goto` pudo
ejecutarse, `outcome=refused` incluido. Y **se niega en CI** por el mismo camino
(`findIsRefusedHere`), porque un bucle que gasta dinero y toca una pantalla no pinta en una
tubería.

---

## 7. Cómo se probaría, y esto es la condición de aceptación

Sin esto, `goto` no se da por construido:

1. **Una pantalla a la que sí hay camino.** Ajustes → Cámara → Estilos fotográficos, que son
   dos toques y está verificado hoy que los taps llegan en este slot.
2. **Una pantalla a la que no existe camino.** Pedir una pantalla que la app no tiene. **Si el
   bucle "llega" a ésta, está roto**, y este caso se ejecuta *antes* que el primero.
2b. **Un criterio que ya se cumple en la pantalla de partida.** `--arrived-when "Notificaciones"`
   estando en la lista de Ajustes, donde esa palabra ya está. Tiene que salir
   `outcome=ambiguous_criterion` sin dar un solo paso. Es el fallo que encontró la revisión y
   el que declaraba llegada en el paso 0.
3. **Una pantalla detrás de un botón destructivo.** Tiene que parar con `outcome=refused` y
   nombrar el elemento. Y el control de ese control: comprobar primero que el botón destructivo
   está de verdad en el árbol, porque hoy una prueba de la guarda pasó por las tres ramas
   estando en la pantalla equivocada — las tres abstenciones eran correctas y no probaban nada.
4. **Un toque que no hace nada.** Reproducible hoy con `mav ui swipe` en este slot. El bucle
   tiene que parar con `outcome=stuck` en 2 pasos, no agotar los 12.

---

## 8. Lo que bloquea construirlo

**`mav ui swipe` no mueve la pantalla.** Medido hoy en `iPhone-17-Pro@26.3/slot-5` sobre
Ajustes → General: `ok cmd=ui.swipe direction=up driver=axe`, huella idéntica, 213 nodos
idénticos antes y después. Los taps en ese mismo slot y esa misma sesión sí funcionan.

Eso es lo contrario de lo que dice el **issue #90** (*"no es el fallo conocido de tap … ahí el
swipe sigue funcionando. Aquí no pasa ningún gesto"*): aquí el tap funciona y el swipe no.

Importa para `goto` más que para nada: una pantalla que no está en los primeros 80 nodos y
requiere desplazarse es inalcanzable para el bucle. Se puede construir `goto` sin swipe y sólo
con taps, pero entonces su alcance es "lo que cabe sin desplazar", y eso hay que decirlo en vez
de descubrirlo.

---

## 9. ¿Ahorra lecturas de pantalla? Sí: la mitad. Y depende de un comando roto

Medido el 19 sep 2026 en `iPhone-17-Pro@26.3` de simpool, con Ajustes de iOS y `mav` v0.21.0.
Esta sección existe porque la pregunta *"¿cuántas lecturas te ahorras de verdad?"* tenía que
contestarse **antes** de escribir el bucle, no después.

### La causa que nadie había nombrado: el tap por selector relee el árbol

| primitiva | coste | ¿lee el árbol? |
|---|---|---|
| `mav ui tree` | ~500 ms | sí, una |
| `mav ui tap --x --y` | **277 ms** | **no** |
| `mav ui tap --text` | 1.480 ms | **sí, otra vez** |
| `mav ui tap --id` | 1.680 ms | **sí, otra vez** |
| `mav ui find` | ~1.010 ms | sí, una (+ ~480 ms de jev) |

Un tap por selector cuesta **cinco veces** lo que un tap por coordenadas, y la diferencia es
que tiene que releer la pantalla para resolver el selector.

**De ahí sale por qué `find` no puede ganar en tiempo, y no es culpa de jev.** Hoy, un paso ya
cuesta **dos lecturas**: una para que el agente mire y otra dentro del tap para resolver. `find`
no sustituye ninguna de las dos — sustituye el criterio del agente, no su lectura — así que
añade jev encima de dos lecturas que siguen ahí.

### Los tres caminos, ejecutados de punta a punta, dos toques cada uno

| camino | tiempo | lecturas | jev |
|---|---|---|---|
| **A** hoy: agente lee el árbol + `tap --text` | 10.130 ms | **5** | 0 |
| **B** con `find` + `tap --text` | 14.041 ms | **5** | 2 |
| **C** forma de un paso de `goto`: `find` + `tap --x --y` | 7.210 ms | **3** | 2 |

- **B es un 39% más lento que A y no ahorra ni una lectura.** Concuerda con los 13,8 s contra
  15,5 s medidos en la demo sobre Undolly: `find` compra contexto, no tiempo.
- **C hace 3 lecturas donde A hace 5: un 40% menos**, porque tapa el punto que ya resolvió en
  lugar de pedir que alguien lo resuelva otra vez.

**Honestidad sobre C:** su segundo `find` se abstuvo, así que dio un toque menos que A y su
tiempo absoluto está inflado. Lo que no depende de eso es el recuento de lecturas, que sale
de la estructura y no de esa ejecución: **A cuesta dos lecturas por paso, C cuesta una.**

### Y el bloqueo, que es exactamente la primitiva de la que depende todo

**El tap por coordenadas devuelve `ok` y no entrega el gesto.** Con su control al lado, mismo
slot y mismo minuto:

```
tap --x/--y  (el punto que ocupa "General")  -> ok   y la pantalla NO cambió
tap --text   (esa misma fila, por nombre)    -> ok   y la pantalla SÍ cambió
```

El punto era correcto —salió del `frame` de esa misma fila— así que no es puntería: es la ruta
HID. Y el control importa: si no cambiara nada con ninguno de los dos, el hallazgo sería "el
simulador está sordo" y no diría nada sobre coordenadas.

**Así que toda la ventaja de `goto` descansa sobre el único comando que está roto.** Construir
el bucle usando `tap --text` funcionaría y **no ahorraría ni una lectura**: sería `find` en
bucle, o sea el camino B, que ya sabemos que pierde.

`hive/mav-demo` está en ello (PR #94): añade `delivered=unconfirmed` cuando no verificas y
`verified=changed|unchanged` con `--verify`, y arregla el verificador, que comparaba con
`TreeDiff` —que mira `frame`— y por eso daba `changed` en dos lecturas de una pantalla quieta.

### Recomendación

**No escribir el bucle hasta que el tap por coordenadas entregue el gesto.** El diseño está
completo y la medida dice que merece la pena: la mitad de las lecturas. Pero construirlo ahora
obliga a tapar por selector, y entonces `goto` no gana nada y habríamos gastado el
presupuesto en demostrarlo.

Dos cosas más que condicionan el bucle cuando se escriba:

- **El swipe no está roto siempre**, es dependiente de pantalla: no movió Ajustes de iOS en mis
  pruebas y sí movió la pantalla de inicio de Undolly en las de `hive/mav-demo`. El bucle no
  puede dar por hecho ninguna de las dos cosas; necesita `--verify` para distinguir "no se
  movió" de "ya está al final", que es la confusión que lo dejaría girando hasta el tope.
- **`tap --id` falla cuando el id está duplicado, y en las listas de iOS lo está siempre.** En
  Ajustes → General **no hay una sola etiqueta ni un solo id únicos**: iOS mete cada fila dos
  veces, un botón contenedor y uno interior. Eso además deja sin efecto la ruta literal de
  `find` en esas pantallas, que se abstiene por ambigüedad — correctamente, pero significa que
  el 14,4% de resoluciones literales medido sobre el corpus es bastante más débil en listas
  reales.
