# El flujo con los pasos escritos

Diseño de los pasos de flujo que resuelven un elemento por pantalla preguntando al modelo,
en vez de dejar que el modelo navegue libre. 21 sep 2026.

Manda sobre lo que diga `goto.md` donde se contradigan, y hereda el traspaso de Codex del
20 sep (`~/.claude/hive/traspaso-codex-mav-2026-09-20.md`), que corrige medidas anteriores.

## 0. Por qué esto y no `goal`

> **AVISO DEL 21 SEP, LEER ANTES QUE EL RESTO DE ESTA SECCIÓN.** Lo de abajo sigue siendo
> cierto, pero **no es el motivo por el que esto se construye**, y conviene no venderlo así.
>
> **`goto` ya llega con la frase plana**: 10/10 desde la raíz, 2 pasos, y las 10 tocaron
> `Test Category 1` —comprobado por los ids de lo que tocó en los logs crudos, no por su
> propio `arrived=true`, que es la clase de evidencia que no puede fallar—.
>
> **Y §12 no es falsa: la atribución sí lo era.** Remedido con 80 tiradas, la tabla de §12
> se reproduce **clavada sobre `mav ui find`** (5/5 donde dice 3/3, 0/5 donde dice 0/3, con
> el cruce incluido) y coincide con `goto` en siete de las ocho celdas. `goto` diverge en
> una, y **resolviendo mal**: elige `Test Category 1` las cinco veces cuando la caja que
> persigue la frase está en la 2. Adivina un lead, que es algo que su bucle absorbe y el
> llamante de `find` no. De las dos diferencias posibles manda **la pregunta, no el
> veredicto**: el veredicto resultó inerte —`jevi` devolvió `yes` 40 de 40, incluidas las 15
> abstenciones— y esa guarda ya se ha quitado.
>
> Así que este documento **no se justifica porque el flujo sea lo único que llega**. Se
> justifica por el requisito que puso David —**igual o más rápido, y llegando**— y por §1.4:
> en un flujo la redacción de cada paso se corrige mirando **una** pantalla, mientras que en
> `goto` la misma frase tiene que sobrevivir a todas a la vez, sin que nadie sepa que eso es
> lo que hay que conseguir.
>
> **El listón se cumplió**: medido en la misma tanda y alternando carriles, el flujo con su
> paso de comprobación queda por debajo de `goto`. Los números están en §6.

Esto no es una corazonada: es la conclusión de `goto.md` §12, y está medida.

`goto` le pasa **el mismo objetivo a `find` en cada pantalla**, así que una frase sólo sirve
si resuelve en **todos** los saltos. Sobre el recorrido de dos saltos de Boxy —lista de
categorías → lista de cajas—, 3 tiradas por celda:

| objetivo | lista de categorías | lista de cajas |
|---|---|---|
| `"the box inside Test Category 2"` | **3/3** | **0/3** |
| `"the box inside this category"` | **0/3** | **3/3** |
| `"open the box in Test Category 2"` | **3/3** | **0/3** |

**La que ancla arriba desancla abajo.** Ninguna redacción aguanta el recorrido entero, y no
es un problema de palabras: es que una frase plana tiene que significar dos cosas distintas
en dos pantallas distintas.

Leída al revés, la misma tabla dice lo que sí funciona: **por separado, las dos piezas
resuelven 3/3 hoy**. El modelo resuelve bien **un elemento por pantalla**; lo que no resuelve
es la ruta.

Así que la ruta la pone quien escribe el flujo, y el modelo resuelve sólo lo que ya sabe
resolver. §12 dejaba abierto si la solución era *"un objetivo por paso, una reformulación por
pantalla, o que `find` reciba dónde está el bucle"*. **Es la primera**, y quien escribe el
flujo ya la tiene escrita.

La diferencia con lo que proponía §13 —una llamada previa que **el modelo** trocee en
subobjetivos— es que aquí el modelo no interviene en la ruta en ningún momento. Una llamada
menos, y un sitio menos donde equivocarse.

### Y por qué una frase plana sí llega, que es el argumento entero

Parecía que el 10/10 tumbaba §12. No la tumba: **la explica**, y de paso dice por qué este
diseño es el correcto.

Remedido el 21 sep, 80 tiradas, 4 frases × 2 pantallas × los dos comandos: la frase que
llega 10/10 —*"los contenidos de la primera categoría, primera caja"*— **no es ninguna de
las de la tabla de §12**. Lleva **un ancla por pantalla, y en orden**: *"la primera
categoría"* resuelve en la lista de categorías y *"primera caja"* resuelve 5/5 en la de
cajas. Las tres frases de §12 anclan en **una sola** pantalla cada una, y por eso ninguna
aguanta los dos saltos.

O sea que la regla de §12 se sostiene entera, y lo que aprendemos es la forma de cumplirla:

> **Enumerar la ruta dentro de la frase es el flujo con pasos escritos comprimido en una
> línea.**

Eso es el argumento de este documento dicho con datos en vez de con opinión. Lo que `goto`
consigue cuando el objetivo enumera la ruta, un flujo escrito lo consigue **siempre**, sin
depender de que quien escriba la frase acierte a poner un ancla por pantalla y en el orden
correcto — que es una habilidad que nadie tiene por qué tener y que no está documentada en
ningún sitio.

La navegación libre (`goto` sin pasos) se queda como investigación.

### Dos cosas de §11 que decide cualquiera que mida esto

- **Una categoría con UNA sola caja: §11 sigue en pie, y parecía que no.** §11 aisló que con
  UNA fila de **número pelado** *"the first box"* acertaba **0/4** y con DOS **4/4**. Hoy esa
  misma pantalla de una sola fila resuelve **10/10**, lo que parece contradecirlo — y no lo
  hace: **la fila de este fixture no es un número pelado**, lleva `id=boxRow_1001`. §11 ya
  decía que el modelo necesita que algo en la fila diga de qué clase de cosa es, y que puede
  sacarlo de un identificador, de la etiqueta **o** de tener hermanas iguales. Aquí lo saca
  del identificador. Las dos medidas son ciertas y no se tocan.
- **Y "primera caja" es hoy un aserto casi vacío**: con una caja por categoría, acertar es
  gratis. El escenario prueba bien "primera categoría" y casi nada de "primera caja". Para
  probar el ordinal de verdad hace falta un fixture con dos cajas o más por categoría.
- **La alerta de permiso sustituye el árbol entero**: con una delante, `mav ui tree` devuelve
  sólo sus dos botones y nada de la app, y un recorrido sale vacío como si la app estuviera
  rota. `xcrun simctl privacy <udid> reset all <bundle>` antes de cada tirada, y aun así
  mirar el árbol.

## 1. La forma: ni un comando nuevo, ni una envoltura

El requisito de David es que la API quede sólida **para YAML y para CLI, las dos, no una
envoltura de la otra**. Eso ya está resuelto en la forma del repo y sólo hay que no
romperlo: `Selector` es **una sola struct** que leen los dos lados —`selectorFromCLI` en
`internal/mav/selector.go` para la línea de comandos, las etiquetas `yaml:` para el flujo—.

Por eso esto **no es un comando nuevo**. Son tres cosas pequeñas:

1. una clase de selector nueva,
2. una fuente de texto nueva,
3. un caché del árbol.

Y con eso, **todas** las acciones que ya existen —`tap`, `type`, `longPress`, `toggle`,
`assert`, `wait`, `scrollUntil`— pasan a poder usar el modelo sin tocarlas una por una.

### 1.1 `find`: el selector que pregunta

```yaml
- tap: { where: { find: "la primera categoría" } }
```

```sh
mav ui tap --find "la primera categoría"
```

Un campo, `Selector.Find`, y los dos lados lo leen del mismo sitio. Resolverlo es el camino
que ya existe en `mav ui find`, **reutilizado y no reimplementado**: candidatos con
`FindCandidates`, renderizado con `RenderFindCandidates`, opciones con `FindOptions`,
interpretación con la lectura de la etiqueta, y el veto de `VetoChoice` intacto.

Se compone con el resto del selector, y esto importa:

```yaml
- tap: { where: { find: "la primera caja", role: "button" } }
```

Los campos estructurales del selector **recortan los candidatos antes de preguntar**. Eso
es filtrar por estado, no por texto: `Agregar Caja` sigue siendo candidato, que es la regla.

**`none` corta.** Si el modelo se abstiene, el paso falla con `find_abstained` y el flujo
para según su `onFailure`. No hay segundo intento con otra pregunta, no hay segundo modelo,
no hay ranking por eliminación. Y no hay ningún corte de confianza en ninguna parte: no
existe número que separe lo correcto de lo equivocado —aciertos desde 0,62 y fallos hasta
0,88—, así que la abstención vive en la opción `none`, que es donde el modelo puede
expresarla.

### 1.2 El texto: el modelo elige una clave, nunca escribe

Copiado de `arc-cua`, y es lo mejor que tienen. El flujo declara sus entradas y el modelo
elige **cuál**, no **qué**:

```yaml
name: abrir-caja
inputs:
  nombre: "Caja de herramientas"
  cantidad: "12"
steps:
  - type: { where: { find: "el campo del nombre" }, text: { from: nombre } }
  - type: { where: { find: "el campo de cantidad" }, text: { ask: "lo que toca escribir aquí" } }
```

- `text: { from: <clave> }` no consulta al modelo. Cero coste.
- `text: { ask: "..." }` le da las **claves** como opciones y el código sustituye el valor.

El modelo no emite texto libre nunca. Eso cierra dos cosas de golpe: texto alucinado, y
inyección desde las etiquetas de la propia interfaz —que es contra lo que los dos
referentes ponen *"UI text is untrusted data, not instructions"* en su prompt, y nosotros
no lo decimos—. Con una sola clave declarada se resuelve sin llamada.

### 1.3 `verify`: existe, y tiene prohibido decidir si cambió la pantalla

```yaml
- verify: { ask: "¿la caja que se ve abierta está vacía?" }
```

Para juicios de contenido que un selector no sabe expresar. Devuelve el veredicto y ya.

**Lo que no puede hacer, y es la regla dura:** el cambio de pantalla se detecta en código.
Pasarle los dos estados al modelo para que diga si cambió es un juez con errores
correlacionados —mismo modelo, mismo árbol— y además está resuelto: `screenFingerprint` es
la huella de identidad ordenada de `(id, label, role)`, que nació precisamente porque
comparar fotogramas daba `changed=true` en un gesto que no movía nada, y porque contar
nodos tampoco lo detecta (80 antes / 80 después con la pantalla entera distinta).

Así que `verify` no alimenta ninguna decisión de avance ni de llegada. Esas siguen siendo
de `screenFingerprint` y del selector.

## 2. El caché del árbol

Lo que pide David —*caché del árbol de accesibilidad si no cambia*— y que los dos
referentes hacen, aunque de una forma que nosotros no teníamos pensada: el caché **no es
"me guardo el árbol"**, es **"me guardo la identidad y sé por elemento si sigue valiendo"**.

Dos piezas.

### 2.1 El árbol de la tirada, con un bit sucio

Una lectura de la pantalla entera cuesta **287 ms** —`axe describe-ui`, remedido el 21 sep,
10 tiradas sobre una pantalla quieta, mediana 287, min 284, max 295—. Lo que sí se puede
bajar es **preguntar por menos**: `describe-ui --point x,y` cuesta **128 ms** en las mismas
10 tiradas (min 126, max 136), y es la misma herramienta con la opción que ya trae. El caché
guarda el último
`[]Element` leído y un bit `dirty`:

- cualquier acción que pueda mover la pantalla pone `dirty`;
- un paso de sólo lectura con `dirty == false` **reutiliza** lo que hay y se ahorra los
  287 ms enteros;
- la primera lectura después de una acción refresca y limpia el bit.

Sucio por defecto ante cualquier gesto. Sin TTL y sin adivinar: un caché que caduca por
tiempo es un caché que devuelve una pantalla que ya no está.

Dónde se nota: `find` + `assert` + `tap` sobre la misma pantalla hoy son tres lecturas.

### 2.2 El guard por elemento, releído justo antes de tocar

El hueco real que enseñan los dos referentes. Hoy `goto` resuelve una coordenada de una
lectura, le pregunta al modelo —550 ms— y **toca esa coordenada sin comprobar nada**. En
medio la pantalla ha podido terminar de dibujarse y mover la fila.

Los dos guardan un hash de los campos de identidad del elemento elegido y lo revalidan en
vivo antes de disparar; `jev-ultrafast` además hace hit-test con `elementFromPoint` por si
hay algo encima. Aquí el equivalente existe y es barato: `axe describe-ui --point x,y`.

Regla: al resolver se captura el guard —`(id, label, role, value, enabled)`, los mismos
campos que `sameElement`, **sin el frame**, porque las coordenadas se mueven fracciones de
punto entre dos lecturas de una pantalla quieta—. Antes de tocar se comprueba. Si no
cuadra: **una** relectura y una reresolución, y si vuelve a no cuadrar el paso falla. Una,
no un bucle.

**Implementado el 21 sep, y aquí está lo que costó equivocarse.** La primera versión
comprobaba el guard **releyendo el árbol entero**, porque en `mav` no había lectura por
punto. Medido con un shim sobre `axe`: el flujo hacía **5 lecturas de pantalla entera**, las
mismas 5 que `goto`, y el ahorro del asiento de llegada se iba entero en eso. Con
`--point x,y` el flujo hace **3 lecturas enteras y 2 por punto**.

Dos cosas que decide cualquiera que toque esto:

- **El punto es un sí rápido, nunca un no.** `describe-ui --point` contesta con el elemento
  donde cae el hit-test **y sus descendientes**, así que una elección que sea **ancestro** de
  ese elemento no aparece en la respuesta aunque no se haya movido nada. Por eso un no del
  punto sólo compra la relectura entera, que es la que decide; ese caso lo absorbe el
  `Holds(fresh)` de después y nunca llega a `element_moved`.
- **La segunda comprobación estaba muerta.** Preguntaba si la reresolución seguía estando en
  `fresh` —el árbol del que acababa de salir—, lo que sólo puede contestar que sí:
  `element_moved` no podía saltar en ninguna pantalla, por rápido que se moviera. Ahora la
  segunda comprobación vuelve a preguntarle a la pantalla, por punto.

### 2.3 Consumir la decisión antes de actuar

De `jev-ultrafast`, y cuesta cero: la elección se borra del estado **antes** de mover un
dedo, para que un reintento no pueda tocar dos veces.

### 1.3bis Escribe el objetivo LARGO, que es más fiable que el corto

Contraintuitivo y medido el 21 sep sobre la pantalla real de tres categorías, 10 tiradas:

| objetivo | resultado |
|---|---|
| `"la primera categoría"` | `Test Category 1` **3/10** — falla |
| `"los contenidos de la primera categoría, primera caja"` | **10/10** |

La causa no es la forma del mensaje ni `jevi`: es **ambigüedad de verdad**. Con tres
categorías en pantalla, *"la primera"* se puede leer como **orden en pantalla**
(`Moving Boxes`, que es la de arriba) o como el **nombre** `Test Category **1**`. Las dos
lecturas son razonables y el modelo elige una.

La frase larga desambigua sola, porque **cada trozo ancla en una pantalla distinta** y el
conjunto sólo tiene una interpretación coherente. Es la misma regla de §0 vista desde el
otro lado: enumerar la ruta dentro de la frase no sólo la hace sobrevivir a todos los
saltos, también **elimina ambigüedades dentro de un salto**.

> **Regla para quien escriba un paso o un objetivo: si acortas, comprueba.** Lo corto
> parece más limpio y aquí acierta un tercio de las veces.

### 1.4 `find` devuelve cosas que no son lo pedido, y son TRES celdas, no una

> **AVISO DEL 21 SEP, Y VA ANTES QUE LA TABLA: ESTA SECCIÓN MIDE UN FIXTURE QUE NO ES LA
> PANTALLA QUE SE VE.** `categories-view.txt` tiene **dos** categorías y ningún
> `Moving Boxes`; la pantalla real tiene **tres**. Sobre la real, `"la primera caja"`
> devuelve `Moving Boxes` **10/10** — o sea que **el defecto del campo de búsqueda no
> existe en la pantalla que mira nadie**. Es un artefacto de una captura de dos categorías,
> y se pasaron horas persiguiéndolo antes de que alguien comparara el fixture con la
> pantalla viva.
>
> Lo que sigue vale para entender el camino de resolución, no para predecir lo que se va a
> encontrar un usuario. **Cuando los dos discrepan, decide la pantalla viva**, porque es la
> que decide para él.

> **AMPLIADA EL 21 SEP.** Esta sección documentaba un solo caso —el campo de búsqueda— y
> el defecto es tres veces más grande. Y hay una confusión de lectura debajo que hay que
> deshacer primero, porque si no, dos medidas nuestras parecen contradecirse.

**El banco de ablación no mide aciertos: mide RESOLUCIONES.** `TestAblationTable` cuenta
cuántas veces `find` devuelve *algún* elemento y cuántas se abstiene, e imprime **qué**
eligió. Nunca compara contra una respuesta correcta. Así que un "5/5" de esa tabla —y los
"3/3" de `goto.md` §12, que salen del mismo sitio— significa **"resolvió 5 de 5"**, no
**"acertó 5 de 5"**. Se leyeron como aciertos, y eso fue una interpretación, no una medida.

Deshecha la confusión, las tres celdas malas de la **lista de categorías** son:

| frase | qué devuelve | ¿es un fallo? |
|---|---|---|
| `"la primera caja"` | el **campo de búsqueda** | **Sí, siempre.** No hay ninguna caja en esa pantalla y un campo de texto no es una caja para nadie. |
| `"the box inside Test Category 2"` | el botón `Test Category 2`, 10/10 | **Para `find` sí, para `goto` no.** |
| `"open the box in Test Category 2"` | el botón `Test Category 2`, 10/10 | Igual que la anterior. |

**Y esa última columna es lo importante**, porque las dos últimas celdas son un fallo y un
acierto *a la vez*, según quién pregunte:

- `FindQuestion` pregunta **cuál ES** lo descrito. Una categoría no es una caja, así que
  `find` debería **abstenerse** y no lo hace. Es un fallo suyo.
- `GotoStepQuestion` acepta además **cuál LLEVA** hacia ello. Tocar la categoría es el
  camino correcto hacia la caja de dentro, así que para `goto` es la respuesta buena.

Las dos comparten el mismo camino de candidatos y el mismo modelo, y **difieren sólo en la
pregunta** — que es exactamente lo que se midió en `goto.md` §12. O sea que no hay
contradicción entre nuestras medidas: hay una tabla que cuenta resoluciones y dos comandos
con criterios de acierto opuestos en las mismas celdas.

**Consecuencia práctica, y es la que vale:** `find` es más flojo de lo que la tabla sugiere.
De las cuatro frases probadas sobre la lista de categorías, **tres devuelven algo que no es
lo pedido**. Quien escriba un paso `find` no debe suponer que una abstención le protege: en
esta pantalla no se abstiene casi nunca.

**Lo que NO lo arregla**, y ya son cinco cosas medidas, dos de ellas por dos nodos
distintos con dos bancos distintos: filtrar por texto, un corte de confianza, recortar por
`role`, redactar el prompt en contra, y **mandar cada opción con su registro estructurado
en vez de con su número** — esto último medido a 30 tiradas, 0/30 hoy y 1/30 con el mejor
brazo, o sea que sólo cambia unos fallos por otros.

#### Y un sexto, medido el 22 sep: la frase que nombra el objetivo. Tampoco.

Era el candidato con mejor pinta que ha habido, porque no era una corazonada: en v0.28.0 se
midió que **esa frase, y sólo esa, se llevaba una quinta parte de los aciertos de `goto`**
(29–34/40 con *"Someone is trying to reach: X"*, 40/40 con *"A user described where they
want to get to as: X"*). La pregunta de `find` sigue diciendo *"what they want to **tap**"*,
que es un marco de **acción** — y tocar la categoría es, en efecto, la acción que abre la
caja de dentro. Pasarlo a un marco de **identidad** parecía exactamente el mismo cambio que
allí lo arregló todo:

```
- "A user described what they want to tap as: " + goal
- "Which numbered element is it?"
+ "A user described the element they are looking for as: " + goal
+ "Which numbered element IS that element?"
```

Medido sobre el banco entero, ocho celdas, dos pasadas: **idéntico, celda por celda y pick
por pick**. Las dos celdas malas siguen devolviendo el botón `Test Category 2` 5/5, y las
seis buenas no se movieron. No es que el arreglo sea pequeño: es que no existe.

**Y el aviso del ordinal (`goto.md` §14) no rescata estos descartes.** Envenena las medidas
de la celda `"la primera caja"`, que lleva ordinal; **estas dos no llevan ninguno**. El
mecanismo de §14 —un número dentro de un nombre absorbiendo un *"primera"*— no está
operando aquí.

#### Lo que sí sabemos ahora, y es una regla más afilada que la que había

El defecto no es *"`find` no se abstiene en esta pantalla"*. Es más estrecho, y se lee
directamente de la tabla:

| frase, misma pantalla, mismos candidatos | nombra una fila que está en pantalla | resultado |
|---|---|---|
| `"the box inside Test Category 2"` | sí, `Test Category 2` | la devuelve, 5/5 |
| `"open the box in Test Category 2"` | sí, `Test Category 2` | la devuelve, 5/5 |
| `"the box inside this category"` | no | **se abstiene 5/5** |

Las tres quieren decir lo mismo. La única que se abstiene es la que **no nombra ninguna
fila visible**. O sea: **`find` no se abstendrá mientras el objetivo nombre algo que está en
la pantalla**, aunque lo nombre como contenedor de lo que se busca. Y eso no se puede quitar
desde la pregunta sin quitárselo también a `goto`, que comparte el camino y para quien esa
respuesta es la correcta.

**Se queda sin cerrar, a propósito.** La mitigación sigue siendo de quien escribe el paso, y
ahora con una regla que se puede comprobar de un vistazo: **si tu frase nombra una pantalla
o un contenedor por su nombre, `find` te devolverá ese contenedor.** Nombra la cosa
(`"la fila de la caja 1000"`), o llega a ella en dos pasos.

> **Y una advertencia sobre cómo leer cualquier medida nuestra anterior al 21 sep.**
> `jevi` **reordenaba alfabéticamente las claves** de toda pregunta que reenviaba: usaba
> `serde_json` sin `preserve_order`, así que una pregunta escrita `{goal, context, rules}`
> le llegaba al modelo como `{context, goal, rules}`, y una opción escrita
> `{role, name, id}` como `{id, name, role}`. Su propio comentario decía que la pregunta
> viajaba sin tocar: cierto de los valores, falso del orden.
>
> **Eso no es un detalle, es la explicación de una medida entera.** Una tanda de nueve
> formas de llamada distintas dio respuestas byte a byte idénticas en ocho de ellas — no
> porque la forma diera igual, sino porque las nueve llegaban aplanadas a la misma. Medido
> sobre una celda de este mismo fixture: por HTTP a mano con el orden de quien llama,
> **28/30**; alfabetizada como la dejaba `jevi`, **3/30**. Y por el binario, con ablación
> limpia, **2/30 antes y 29/30 después**. Una línea en `Cargo.toml`.
>
> Las medidas viejas **no se retiran**: son correctas para *"lo que se podía mandar con
> aquel `jevi`"*. Lo que no son es comparables con nada tomado sobre otra versión.
>
> De ahí sale también que **copiar la forma nativa entera es peor que la nuestra**:
> reproducida exactamente la de `jev-ultrafast`, mide igual que la de hoy. Lo que paga es
> la lista numerada en prosa **más** los registros estructurados **más** `instructions`
> como objeto, las tres a la vez; quitar cualquiera devuelve al punto de partida. No
> éramos poco nativos — es que no podíamos mandar la única combinación que gana.

### No escribas un sustantivo desnudo en un paso

Medido el 21 sep, y es lo único que hay que saber para escribir un paso que funcione.

`find "la primera caja"` en la **lista de categorías** —una pantalla sin una sola caja—
devuelve **el campo de búsqueda**, 5/5, sin que salte ningún veto. Es justo lo que la opción
`none` existe para evitar.

La hipótesis obvia era que en castellano *"caja"* también es la caja de búsqueda. **Está
refutada**: `"the first box"` en inglés elige el mismo campo, 5/5. Y no es que la abstención
esté rota en esa pantalla — `"the delete button"` y `"the shopping cart icon"` se abstienen
0/5 ahí mismo.

Lo que falla es el **sustantivo desnudo**, que en los dos idiomas denota también una caja de
texto. En cuanto la frase desambigua, vuelve a abstenerse bien: `"a cardboard box for
storing things"` 0/5, `"the list row for a box"` 0/5.

**Cuatro arreglos probados y los cuatro rechazados**, para que nadie los repita:

| intento | por qué no |
|---|---|
| filtrar candidatos por texto | prohibido, y se lleva por delante `Agregar Caja` |
| corte de confianza | prohibido, y no separa nada (aciertos desde 0,62, fallos hasta 0,88) |
| recortar con `role: "button"` | **medido: no arregla** — cambia el campo de búsqueda por `Test Category 1` 4/5, que es peor, porque una categoría sí navega |
| redactar el prompt en contra | las siete celdas buenas aguantaron y **la del defecto se quedó en 5/5**: el modelo no lee el campo como *donde buscarías* una caja, lo lee como que *es* una caja |

Así que el defecto se queda en pie a propósito, con un test determinista que cierra la
puerta al arreglo prohibido, y la mitigación es de quien escribe el flujo:

> **Un paso nombra la cosa, no su categoría gramatical.** `"la fila de la caja 1000"` o
> `"la primera caja de la lista"`, no `"la primera caja"`.

Y es un argumento más a favor de esto frente a la navegación libre: en un flujo escrito la
redacción de cada paso **se escribe una vez y se corrige mirando una pantalla**, mientras
que en `goto` la misma frase tiene que sobrevivir a todas las pantallas del camino a la vez.

## 3. Las esperas se asumen

Como el flujo declara la ruta, no hay que descubrir cuándo ha terminado una transición.

`goto` paga `gotoSettle` antes de declarar llegada: dos lecturas más que comparar entre sí,
~1.260 ms, y las paga porque no sabe a dónde va. Un flujo escrito sí lo sabe.

Así que por defecto un paso **no asienta**: hace su gesto y sigue. **La lectura del paso
siguiente es la espera.** Un árbol leído a medias en un paso intermedio no cuesta nada,
porque el paso siguiente vuelve a leer de todas formas.

Cuando un paso concreto necesite esperar de verdad, ya existe la forma de decirlo y no hace
falta inventar otra: `after: { wait: { ... } }`, con una condición estructural.

## 4. Una cabeza o dos, pero una sola llamada

Los dos referentes mandan en la **misma petición** la operación y todos los objetivos
posibles, y **tiran las cabezas que no tocan**. Pagan tokens de más para ahorrar un
round-trip por paso.

Aquí sólo aplica a un caso, pero es el caso que David nombró: un paso de escribir necesita
*dónde tocar* y *qué escribir*. Son dos preguntas, y mandarlas juntas ahorra ~550 ms.

Y hay una medida nuestra que lo hace gratis: **la latencia de jev no depende del número de
candidatos** —358 ms con 3, 414 con 12, 387 con 20—. No hay nada que ganar mandando menos,
y por tanto tampoco nada que perder mandando la segunda pregunta a la vez.

Esto es lo último que se implementa, cuando el resto mida.

## 5. Filtrar candidatos por estado

Los dos referentes descartan lo invisible y lo deshabilitado **antes** de preguntar.
`FindCandidates` hoy no lo hace: manda los deshabilitados y los pinta con `enabled=false`.

Es filtrado por estado, no por texto, así que no choca con la regla. Y no se hace por
ahorrar latencia —no la hay que ahorrar, §4— sino porque un candidato que no se puede tocar
es una respuesta que no se puede ejecutar.

## 6. Por qué debería ser más rápido, y cómo se comprueba

Es un requisito de David, no un deseo: **igual o más rápido que `goal`, y medido**. Si sale
más lento, no sirve.

**Los números de `goto.md` §10 están caducados y cualquiera que calcule con ellos se pasa de
largo.** Vuelto a medir el 21 sep con el `cost` de `mav ui find`, n=5:

| | §10 (20 sep) | hoy (21 sep) |
|---|---|---|
| leer la pantalla | 630 ms | **320 ms** |
| jev decidiendo | 550 ms | **350 ms** |
| el toque | 845 ms | **803 ms** |
| un paso | 2.025 ms | **≈1.470 ms** |

El toque es lo único que no se ha movido, y es ya **el 55% de un paso**. Es de `axe` y no se
baja desde aquí.

La predicción con los números de hoy:

| | lectura | modelo | toque | asiento | total |
|---|---|---|---|---|---|
| paso de `goto` | 320 | 350 | 803 | +640 al final | 1.470 |
| paso de flujo literal (`text:`) | 320 | **0** | 803 | 0 | **1.125** |
| paso de flujo con `find` | 320 | 350 | 803 | 0 | 1.470 |
| paso de flujo con árbol cacheado | **0** | — | — | 0 | según el paso |

Contra el recorrido real de Boxy: `goto` hace 2 pasos y mide **4.095 ms**, que es más que
2×1.470 porque paga además la lectura inicial y la verificación final. Un flujo de dos pasos
`find` sin asiento debería quedar sobre **3,3 s**. Esa es la predicción; la medida decide.

### La medida, ya hecha: el flujo gana por 332 ms

Cara a cara del 21 sep, mismo binario, mismo slot, 10 tiradas por carril **alternadas**, la
alerta de reconocimiento de voz quemada en un calentamiento, y en igualdad de condiciones
—`goto --arrived-when` contra un flujo de **tres** pasos, los dos toques y un `assert` sobre
`id: voice_record_button`, que es el carril que también comprueba la llegada—:

| carril | n | mediana | min | max | llegó | tocó `Test Category 1` |
|---|---|---|---|---|---|---|
| `goto --arrived-when` | 10 | **4.306 ms** | 4.141 | 4.574 | 10/10 | 10/10 |
| flujo con `assert` | 10 | **3.974 ms** | 3.854 | 4.371 | 10/10 | 10/10 |

Llegada comprobada por fuera leyendo el árbol y buscando `id=voice_record_button`, nunca por
lo que el propio comando dijera de sí mismo; `Test Category 1` sacado de los ids que cada
tirada tocó, nunca de un código de caja —`1000` y `1001` se reparten sin orden, y en estas
10 salieron los dos—.

**332 ms de ventaja en mediana, y los rangos ya no solapan salvo por la primera tirada del
flujo** (4.371 ms, la única por encima de 4.032). La tanda anterior, con el guard releyendo
el árbol entero, daba 4.206 contra 4.194: empate.

De dónde sale la ventaja, y conviene ser honesto: **un paso con `find` no es más rápido que
un paso de `goto`**. Gana en otras tres cosas:

1. los pasos que nombran el elemento no pagan modelo;
2. no hay asiento de llegada, que son dos lecturas;
3. no hay abstenciones que reintentar, ni detección de bucles, ni presupuesto de pasos
   quemado en leads equivocados.

**La comprobación**: mismo recorrido de Boxy, varias tiradas, `goto` contra el flujo,
tiempo de pared y `outcome`. Ni una medida contra el fixture no determinista:
`createTestBoxes()` reparte las cajas sin orden, así que abrir `Test Category 2` **no
garantiza** la caja `1000` —4/10 dieron `boxRow_1000`, 5/10 `boxRow_1001`, 1/10 un menú
contextual, y eso **sin usar `goto`**—. Asertos estructurales: `boxes-view` o cualquier
`boxRow_`, nunca un código concreto.

## 7. Lo que no se toca

- El cambio de pantalla se detecta en código (§1.3).
- Nada de un segundo LLM, ni ranking por eliminación, ni filtrar candidatos por texto.
- Ningún corte de confianza en ninguna parte. Ordenar no es umbralizar.
- `none` corta el bucle.
- El veto de `VetoChoice` y la guarda de destructivo siguen igual: sólo pueden quitar un sí,
  nunca ponerlo.
- No se integra el spike `action-space-systemone` ni el commit `92e5982`.
- Nunca se parchea código de terceros: si el límite es de `axe`, es de `axe`.

### Una premisa de §13 que ya no vale, y que aun así no cambia nada

§13 dice *"jevi no tiene ranking… así que un ranking sólo se puede construir por eliminación:
preguntar, quitar la elegida, volver a preguntar"*. Leyendo el código de los dos referentes,
que van al mismo backend —`api.typesafe.ai/v1/systemone`, modelo `jev-latest`— eso es falso
a nivel de API: la respuesta trae un `probabilities` **completo, un valor por cada id, en una
sola llamada**. Los dos lo validan igual: el conjunto de claves tiene que coincidir con el de
ids, todo finito en [0,1], la suma ≈1 (±0,02), y el `choice` tiene que ser el argmax.

O sea que el ranking no costaría N llamadas, costaría cero llamadas extra. Lo que limita es
`jevi`, no el servicio.

**Y aun así no se construye.** Por dos razones, las dos medidas y ninguna de latencia:

1. **Ordenar no fabrica conocimiento.** En la pantalla que hoy falla, "tocar el primero"
   tocaría `Agregar Caja` —el botón que **crea** una caja— en vez de abrir la que existe. Eso
   es peor que abstenerse: crea datos.
2. **Ningún corte de confianza separa nada**: aciertos desde 0,62 y fallos hasta 0,88. Y es
   consistente con los referentes, que tampoco umbralizan en ninguna parte — su validación
   sólo comprueba que el `choice` sea el argmax, nunca lo compara contra un corte.

Queda escrito aquí para que nadie vuelva a medir si `jevi` puede ordenar. Puede el servicio;
la decisión de no usarlo es de diseño, no de capacidad.

## 8. Lo que no verifica nadie, y aquí sí

Los dos referentes se leyeron por código justamente porque el README de `browser-use` nos
hizo dar por bueno que confirmaba la llegada. No la confirma:

- `jev-ultrafast`, cuando el modelo dice `DONE`, sólo comprueba que la página no haya
  cambiado desde la decisión, y devuelve. Su única verificación real está fuera de la
  librería, hardcodeada para el ejemplo de vuelos.
- `arc-cua` rellena sus `observations` repitiendo literalmente los criterios del caller:
  `f"Policy judged criterion satisfied: {c}"`.

En los dos, "he llegado" es la palabra del modelo. **Progreso** sí lo miden los dos en
código, por cambio de huella, y los dos cortan a los tres pasos sin cambio.

O sea que el `--arrived-when` de `goto` no tiene arte previo que copiar y va por delante de
los dos. El flujo escrito hereda ese contrato tal cual: sin criterio, `arrived=unverified`,
nunca una afirmación.

## 9. `goto` dentro del flujo: el criterio obligatorio y `unverified` como fallo

Construido y medido el 21 sep 2026 sobre Boxy, `iPhone 17 Pro` / iOS 26.3 desde el slot-5 de
simpool. Carga 2,1–4,5 (load1) y 3,2–4,1 (load5) de principio a fin. Canario de toque —md5 de
la huella ordenada de `(id,label,role)` antes y después de un toque conocido— verde antes de
la tirada 1 y después de la 10 en las dos tandas. Ningún `F81D81A1`.

### Las dos cintas de seguridad

**El criterio de llegada es obligatorio dentro de un flujo**, y su ausencia es un error de
carga, o sea de lint, no una sorpresa a tres pantallas de distancia.

**`arrived=unverified` FALLA el paso.** Fuera de un flujo `unverified` es una respuesta
honesta: el comando dice que no puede confirmar y quien lo lee decide. Dentro de un flujo es
una premisa sin comprobar sobre la que van a actuar los pasos siguientes, y un flujo que
sigue adelante sobre una llegada falsa toca donde no toca. Un flujo que falla es molesto; uno
que hace cosas raras en la app de alguien es otra cosa.

### La llegada deducida es real, y está medida

Sin `--arrived-when`, en vivo:

```
mav goto "create a category called Test Category"
ok cmd=goto arrived=true criterion_observed="title:\"Create Category\"" criterion_source=observed outcome=arrived route="screen=categories-view title=Create Category" steps=3
```

Leído el árbol justo después, sin tocar nada: las categorías son las tres del fixture
—`Moving Boxes`, `Test Category 1`, `Test Category 2`—, **no hay ninguna con la etiqueta
exacta `Test Category`**, y `categoryNameTextField` sigue con su marcador
`value="Category name"`. Los tres pasos tocaron **el mismo botón de la barra**, y el 2 y el 3
con `changed:false`.

O sea: abrió la hoja y llamó a eso haber creado la categoría. **`goto` sabe comprobar que ha
llegado a un SITIO, no que ha hecho una COSA.** Con criterio obligatorio esa ruta no se toma
nunca dentro de un flujo.

### Por qué el `goto` del flujo va a la hoja y no a la rejilla

La app arranca en la rejilla, y `goto` rechaza por diseño un criterio que ya se cumple en la
pantalla de partida —sería cantar victoria en el paso cero sin tocar nada—. Un `goto` a la
rejilla desde la rejilla devuelve `arrived=unverified`, que dentro de un flujo falla. Así que
el destino es la hoja, que además es justo el trayecto donde la llegada deducida mintió.

### El caché de árbol servía a `goto` su propia pantalla de antes del toque

**Primera tanda: 0/20.** Las veinte idénticas, `outcome=stuck`,
`next="two taps in a row changed nothing"`, `route=screen=categories-view` sin título — con
la hoja abierta en pantalla al terminar.

La causa no era el flujo ni la redacción. El caché de árbol se arma durante todo un `mav run`
y no para un `mav goto` suelto. Un paso de flujo lo ensucia al entrar y al salir, que es lo
correcto para todos los demás: leen, actúan y entregan la pantalla al siguiente. `goto` es el
paso que **toca y relee muchas veces dentro de sí mismo**, y entre su propio toque y su
propia siguiente lectura no había nada que ensuciara el caché. Cada lectura después de la
primera recibía la pantalla de antes del primer toque.

Esto también explica la divergencia que parecía de matching: con `criterion_source=observed`
`goto` veía `title=Create Category` y con `explicit` no lo veía nunca. Misma app, misma
pantalla, distinto estado de caché.

`gotoSettle` estaba roto igual y peor: decide que una pantalla ha dejado de moverse
leyéndola dos veces y comparando, y dos lecturas de una entrada de caché son iguales por
construcción. Declaraba asentada cualquier pantalla al instante, incluida la medio dibujada
que es lo único que existe para descartar.

Arreglo: las lecturas de `goto` son siempre lecturas. Ablación hecha — los dos tests de
regresión fallan quitando la invalidación y pasan poniéndola.

### La tasa

| tanda | acierto | fallo honesto | escritura falsa |
|---|---|---|---|
| antes del arreglo del caché | 0/20 | 20/20 | 0 |
| después | **19/20** | 1/20 | **0** |

Mismo listón que el flujo **sin** `goto` con los tres pasos declarados, que va 19/20 con cero
escrituras falsas.

Acierto comprobado **leyendo el árbol**, nunca por el código de salida: existe un nodo
`category_<UUID>` con la etiqueta **exactamente** `Test Category`, y las categorías tras cada
acierto son `Moving Boxes`, `Test Category`, `Test Category 1`, `Test Category 2`.

El único fallo, la tirada 9, murió en el **paso 3** —el toque que confirma— con `tap_failed`
a 1,2 s, con el teclado todavía en pantalla y `Create` ya habilitado. El árbol lo clasifica
como fallo honesto y no como escritura falsa: `categoryNameTextField` tenía
`value="Test Category"`, el campo de búsqueda seguía en `value=Search`, y no se creó nada. Es
un flake del toque con el teclado levantado, no un problema de `goto` ni de escritura.

**Cero escrituras falsas en las 40 tiradas de las dos tandas.**
