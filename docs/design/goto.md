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

## 2. Cómo sabe que ha llegado — la trampa, y cómo se sale de ella

Si se lo preguntas al mismo modelo que eligió el camino, se está calificando a sí mismo. Y
`browser-use` no tiene respuesta a esto (§0). Así que:

**La llegada la decide código, sobre una pregunta que no es la que se usó para navegar.**

Tres piezas, en orden, y la clave es que **la tercera nunca ve el objetivo del viaje**:

1. **Huella de pantalla estable** (§3). Si la huella no ha cambiado, no se ha llegado a ningún
   sitio y no hay nada que juzgar. Es código puro, sin modelo.
2. **Criterio de llegada declarado por quien llama, no por el modelo.** `goto` acepta
   `--arrived-when "<texto o id que tiene que estar en la pantalla>"`. Cuando se da, la llegada
   es una comprobación determinista sobre el árbol: ese id o ese texto está, o no está. **Sin
   modelo, sin ambigüedad, y es el modo recomendado.** Es el equivalente honesto del `assert`
   que `browser-use` no tiene.
3. **Y sólo si no se declaró criterio: un juez ciego.** Una segunda llamada a jev que recibe
   **únicamente** el árbol de la pantalla final y la pregunta *"¿qué pantalla es ésta?"*, en
   forma de elección entre nombres de pantalla. **No recibe el objetivo, no recibe el camino
   recorrido, no recibe las decisiones anteriores.** Después, **código** compara su respuesta
   con el objetivo pedido. El modelo no puede confirmar su propio trabajo porque no sabe cuál
   era su trabajo.

Eso es lo que evita la autocalificación: no es un segundo modelo revisando al primero —eso es
lo que hace el juez de `browser-use` y por eso no vale—, es **el mismo modelo respondiendo a
una pregunta distinta, sin el contexto que sesgaría la respuesta**, y código haciendo la
comparación.

**Y lo que se emite cuando no hay criterio declarado es `arrived=unverified`, nunca
`arrived=true`.** Un `goto` sin `--arrived-when` no puede afirmar la llegada, sólo reportarla.
Es la misma regla que `find`: nunca verde en silencio.

---

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
  "outcome": "arrived | exhausted | timeout | stuck | looping | no_route | refused | out_of_app",
  "steps": [ {"tapped": {...}, "fingerprint_before": "...", "fingerprint_after": "...", "changed": true} ],
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
