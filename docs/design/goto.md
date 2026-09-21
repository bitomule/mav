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
| se abstiene sin haberse movido | **2 seguidas** | para, `outcome=no_route` |
| se abstiene después de haber recorrido | **2 seguidas** | para, `outcome=dead_end` |
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
  "outcome": "arrived | exhausted | timeout | stuck | looping | no_route | dead_end | refused | out_of_app | ambiguous_criterion",
  "criterion_source": "explicit | inferred | none",
  "criterion": "title:\"...\"",
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

## 10. Dónde se va el tiempo, y por qué no se puede bajar más con `axe`

Medido el 20 sep 2026 en `iPhone-17-Pro@26.3` de simpool, `mav` 0.23.0 + esta rama.
**Esta sección incluye una corrección a una medida mía anterior**, porque la primera versión
recomendaba construir algo que no habría servido.

### Un paso, sin huecos

```
un paso de goto ≈ 2.025 ms

  leer la pantalla       630 ms   31%    de los cuales mav:   7 ms
  jev decidiendo         550 ms   27%
  el toque               845 ms   42%    de los cuales mav:  94 ms
```

`mav` pone **101 ms de 2.025**. Todo lo demás es `axe` y el modelo.

### Lo arreglado

**−157 ms en cada toque** (1.003 → 846, 7/7 entregando). `resolveCapabilities` costaba 310 ms
y **192 eran un `idb --version`** — `idb` es Python, arrancarlo son 116 ms, y lo pagaba cada
comando para una pista que se lee sólo cuando un tap ya falló y en `doctor`. Ahora es perezosa.

### La corrección: la sesión persistente no serviría

Medí que `axe` tenía ~615 ms fijos de sesión y ~139 ms por gesto, usando su modo de lote, y
concluí que mantener una sesión viva bajaría un paso a ~700 ms. **Estaba mal, y el fallo era el
de siempre: medí gestos que no entregaban.**

`axe batch` usa por defecto `tapAt` de FBSimulator, la ruta débil — un lote de tres toques
respondió *"Batch completed successfully"* y **la pantalla no cambió ni una vez**.

Rehecho con `--tap-style physical` y comprobando la huella en cada tirada:

| toques en un lote | tiempo (sin las esperas) | cambió la pantalla | marginal por toque |
|---|---|---|---|
| 2 | 1.868 ms | 3/3 | — |
| 4 | 3.685 ms | 3/3 | ~908 ms |
| 6 | 5.252 ms | 3/3 | ~784 ms |

**El coste es por gesto, no por sesión.** No hay nada que amortizar y una sesión persistente
ahorraría cero.

Y la otra vía tampoco existe: **`axe batch --stdin` no es streaming.** Acumula las líneas y las
ejecuta al cerrar la entrada — escribí un toque, esperé tres segundos, la pantalla quieta. Para
un bucle que mira entre toque y toque es inservible por construcción.

### Todos los drivers, no sólo dos

`mav` registra `axe`, `simctl`, `idb`, `baguette`, `network`, `simtime` y cuatro de macOS. Sólo
tres pueden tocar la interfaz de un simulador:

| driver | leer | tocar | veredicto |
|---|---|---|---|
| **axe** | 462–630 ms, 124–177 nodos | 845 ms, **5/5 entregados** | el único viable |
| **idb** | 186 ms, **13 nodos** | 117 ms, **0/5 entregados** | más rápido y equivocado en las dos |
| **baguette** | 1.080 ms, 18 nodos (árbol del *sistema*) | resuelve a `axe` igual | no aplica |

`simctl` instala y lanza, `network` y `simtime` no son de interfaz, y los cuatro de macOS no
pueden dirigirse a un simulador. **Pregunta cerrada.**

### Y `axe` no se puede configurar para que cueste menos

`describe-ui` sólo acepta `--udid` y `--point`; no hay nada que saltarse. Y los retardos del
tap **ya son cero**: 821 ms con los valores por defecto contra 817 poniendo `--pre-delay 0
--post-delay 0`. **El coste es trabajo, no espera.**

Bajar de aquí es pedirle a `axe` un modo servidor. No se parchea desde `mav` ni se forkea.

### Un aviso de método que me engañó dos veces

**Cualquier medida de un gesto tiene que comprobar que entregó**, y cualquier comparación de
drivers tiene que ir **a través de `mav`**. Un `axe tap -x -y` pelado y un `axe batch` por
defecto caen los dos en `tapAt`; `mav` elige el touch físico. Midiendo los binarios directamente
obtuve **0/5 en los dos drivers** y una división 615/139 que no existía.

### Y jev: la latencia no depende del número de candidatos

358 ms con 3, 414 con 12, 387 con 20. **No hay nada que ganar mandando menos candidatos.**

### Una optimización probada y revertida, para que nadie la repita

La idea parecía buena: al confirmar la llegada el bucle **ya tiene** un árbol recién leído, y
`gotoSettle` volvía a leer dos veces más para compararlas entre sí. Pasarle el que ya tiene
debería ahorrar una lectura entera, ~630 ms.

**Medido, va peor.** Cinco tiradas de Ajustes → General, misma pantalla, misma máquina:

```
dos lecturas de asentamiento   mediana 4.569 ms   [4521, 4566, 4568, 4625, 4798]
una lectura  (reusando)        mediana 4.838 ms   [4633, 4773, 4837, 4993, 5281]
```

Dos razones, y la segunda es la que la mata: para comparar hacía falta una espera **antes** de
la primera lectura en vez de después, así que se añadía un retardo fijo; y el árbol leído justo
tras el toque **todavía difiere** del siguiente lo bastante a menudo como para que haga falta
la segunda lectura de todas formas. O sea que se pagaba la espera y no se ahorraba la lectura.

Revertido. La versión con dos lecturas de asentamiento es la que se queda.

---

## 11. Por qué `goto` abre la caja equivocada, y dos cosas que cuestan una tarde

Medido el 20 sep 2026 sobre Boxy con sus mocks, `mav` 0.25.1.

### La causa, aislada tras refutar tres hipótesis

David pidió que `mav goto "los contenidos de la primera categoría, primera caja"` llegue. Hoy
navega dos saltos y llega al contenido de **una** caja, pero no a la primera. Cuatro hipótesis,
tres refutadas con medida:

- **¿El ordinal no se entiende?** No. Sobre una lista de categorías: *"the first category"* 4/4,
  *"the second category"* 4/4 —o sea que **distingue**, no elige siempre la opción 1—,
  *"la primera categoría"* 4/4, y *"the third category"* devuelve `none` 4/4 porque no existe.
  **La numeración de la lista de candidatos ya lleva la posición y el modelo la lee.**
- **¿El orden del árbol no es el de pantalla?** No, en este caso: medidos los `frame` de las
  filas, el orden del árbol y el de pantalla **coinciden**.
- **¿Los identificadores pelados?** No. Con números pelados el ordinal acierta 4/4 igual que con
  `id=box_0`. Los identificadores arreglan el objetivo **descriptivo** (§ anterior), no el
  **ordinal**. Son dos problemas distintos y sólo uno lo cura la app.
- **¿El idioma?** Tampoco. La pantalla sale en castellano aunque se lance con `--language en`
  —defecto de Boxy— pero preguntando *"la primera caja"* **también se abstiene**. Las cuatro
  redacciones, en los dos idiomas, dan `none`.

**Lo que sí es la causa, aislado en una sola variable:**

```
la misma lista, mismo idioma, misma todo, cambiando SÓLO cuántas cajas hay

  UNA fila de número pelado    "the first box" 0/4      "la primera caja" 0/4
  DOS filas de número pelado   "the first box" 4/4      "la primera caja" 4/4
```

**Una fila que pone `1000` y nada más no se lee como "una caja".** Dos filas iguales se leen como
una **serie del mismo tipo de cosa**, y entonces "la primera" significa algo. Con una sola no hay
serie, no hay tipo, y no hay nada que ordenar.

Eso unifica lo de la sección anterior: **el modelo necesita algo en la fila que diga qué clase de
cosa es**, y puede sacarlo de tres sitios — un identificador (`id=box_0`), la palabra en la
etiqueta (`label="Box 1000"`), **o tener hermanas de la misma forma**. Lo tercero es gratis y
aparece solo cuando la lista tiene más de un elemento, que es por lo que este fallo **se esconde
en cuanto hay datos de verdad**.

**Consecuencia para quien mida:** una pantalla con un solo elemento de una lista es el peor caso,
no el más sencillo. Un banco de pruebas con una caja por categoría prueba lo contrario de lo que
parece.

### Dos cosas que cuestan una tarde a quien venga después

**1. La alerta de permiso SUSTITUYE el árbol entero.** Cuando está delante, `mav ui tree` devuelve
**sólo sus dos botones** y nada de la app. Es distinto de una hoja modal normal —la de bienvenida
de Boxy deja el árbol de debajo visible— y la consecuencia es que **una alerta superviviente de
una tirada anterior deja cualquier recorrido vacío**: `goto` no encuentra ruta, no encuentra
candidatos, y parece que la app esté rota. Límpiala antes de medir o de grabar:

```
xcrun simctl privacy <udid> reset all <bundle>    # antes de lanzar
# y aun así, mira el árbol y despacha lo que haya quedado
```

Me costó dos medidas enteras descubrirlo, las dos dando resultados vacíos que interpreté como
fallos de navegación.

**2. Boxy recuerda que el asistente ya se vio**, así que el recorrido del asistente existe **una
vez por instalación** y hace falta `mav open --clear-state` antes de cada toma.

---

## 12. El objetivo tiene que resolver en TODAS las pantallas del camino

> **CORREGIDA EL 21 SEP: la tabla vale, la atribución no.** Esta sección decía que `goto`
> le pasa el mismo objetivo **a `find`**, y eso dejó de ser cierto en `e5a413e` (20 sep):
> desde entonces `goto` tiene su propia pregunta y su propia lectura de la respuesta. La
> tabla de abajo mide **`mav ui find`**, y sobre `find` se reproduce clavada — 5/5 donde
> dice 3/3 y 0/5 donde dice 0/3, con el cruce incluido, remedida con 5 tiradas por celda.
>
> Sobre `goto` coincide en **siete de las ocho celdas**. Diverge en una: con
> `"the box inside this category"` en la lista de categorías, `find` se abstiene 5/5 y
> `goto` resuelve 5/5 — **y resuelve mal**, eligiendo `Test Category 1` las cinco veces
> cuando la caja que persigue la frase está en la 2. `goto` no acierta ahí: adivina un lead
> equivocado, que es algo que su bucle está hecho para absorber y el llamante de `find` no.
>
> **Cuál de las dos diferencias manda: la pregunta, no el veredicto.** `FindQuestion`
> pregunta *"cuál ES"* y prohíbe adivinar; `GotoStepQuestion` acepta además *"cuál LLEVA"*.
> El veredicto resultó **inerte**: en las 40 tiradas de `find`, `jevi` devolvió
> `verdict: "yes"` **40 de 40**, incluidas las 15 abstenciones. La rama `verdict != "yes"`
> de `InterpretFindAnswer` no se disparó ni una vez — **hoy esa guarda no guarda nada**, y
> todas las abstenciones fueron el modelo respondiendo `none`. La tabla del comentario de
> `InterpretGotoAnswer` (veredicto 4/8, etiqueta 8/8) describe un defecto real, pero no es
> el que se ve en estas pantallas.
>
> **Y por qué una frase plana sí llega**, que es lo que parecía contradecir esto: el
> objetivo que llega 10/10 —*"los contenidos de la primera categoría, primera caja"*— **no
> es ninguna de estas cuatro**. Lleva **un ancla por pantalla y en orden**: *"la primera
> categoría"* resuelve en la lista de categorías y *"primera caja"* en la de cajas. Las de
> la tabla anclan en una sola pantalla cada una. O sea que la regla de esta sección se
> sostiene entera — lo que cambia es que **enumerar la ruta dentro de la frase es una forma
> de cumplirla**, y es el flujo con pasos escritos comprimido en una línea.

`goto` le pasaba **el mismo objetivo a `find` en cada pantalla**, así que una frase sólo
sirve si resuelve en todos los saltos. Eso hace que **una redacción que suena mejor rinda
peor**, y está medido, 3 tiradas por celda, sobre las dos pantallas de un recorrido de dos
saltos en Boxy (lista de categorías → lista de cajas):

| objetivo | en la lista de categorías | en la lista de cajas |
|---|---|---|
| `"the box inside Test Category 2"` | **3/3** | **0/3** |
| `"the box inside this category"` | **0/3** | **3/3** |
| `"open the box in Test Category 2"` | **3/3** | **0/3** |

Ninguna de las tres aguanta el recorrido entero, y las dos que nombran la categoría
fallan **de la misma manera**: resuelven donde hay que tocar la categoría y se abstienen
donde hay que tocar la caja, que es la pantalla cuyo título ya es el nombre de la categoría. la que nombra la categoría resuelve arriba y
se abstiene abajo, y la que dice "esta categoría" hace justo lo contrario. Por eso tres tomas
seguidas de la demo murieron en `steps=1` con `no_route`, y por eso **escribir el objetivo no
es cosmética: es la variable que decide si el bucle llega**.

Lo que queda abierto, y no está medido: si la solución es un objetivo por paso, una
reformulación por pantalla, o que `find` reciba también dónde está el bucle además de adónde
va.
