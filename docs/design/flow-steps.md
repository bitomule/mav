# El flujo con los pasos escritos

Diseño de los pasos de flujo que resuelven un elemento por pantalla preguntando al modelo,
en vez de dejar que el modelo navegue libre. 21 sep 2026.

Manda sobre lo que diga `goto.md` donde se contradigan, y hereda el traspaso de Codex del
20 sep (`~/.claude/hive/traspaso-codex-mav-2026-09-20.md`), que corrige medidas anteriores.

## 0. Por qué esto y no `goal`

> **AVISO DEL 21 SEP, LEER ANTES QUE EL RESTO DE ESTA SECCIÓN.** Lo que sigue era cierto
> el 20 sep y **hoy no se reproduce**. Vuelto a medir con `mav` 0.25.1, 10 tiradas fresh:
> `mav goto "los contenidos de la primera categoría, primera caja"` **llega 10/10** desde la
> raíz, en 2 pasos, **4.095 ms de mediana**. Las 10 tocaron `Test Category 1` —comprobado por
> los ids de lo que tocó en los logs crudos, no por su propio `arrived=true`—. Ninguna murió
> en `no_route`.
>
> **Cuidado con concluir de aquí que §12 es falsa.** §12 y §11 midieron, hasta donde se
> sabe, `mav ui find`; esto mide `mav goto`, y **no son el mismo resolvedor**:
> `InterpretFindAnswer` exige `verdict == "yes"` y se abstiene si no, mientras
> `InterpretGotoAnswer` lee sólo la etiqueta e ignora el veredicto —hay una tabla en su
> propio comentario: leyendo el veredicto 4/8, leyendo la etiqueta 8/8—; y `FindQuestion`
> pregunta *"cuál ES"* mientras `GotoStepQuestion` acepta también *"cuál LLEVA"*. Las dos
> medidas pueden ser ciertas a la vez. Está midiéndose; si divergen, lo que hay que hacer con
> §11 y §12 es **corregirlas diciendo sobre qué comando valen**, no borrarlas — y el hallazgo
> de verdad sería que `find` y `goto` no se comportan igual con la misma frase.
>
> Así que **el flujo con pasos escritos ya no se justifica porque sea lo único que llega**.
> Se justifica sólo si cumple el requisito que puso David: **igual o más rápido, y llegando**.
> El listón es **4.095 ms y 10/10**. Si no baja de ahí, no hay flujo que vender.
>
> La sección se queda escrita porque explica de dónde viene el diseño, y porque la forma
> —un objetivo por pantalla— sigue siendo la correcta por lo que dice §4 de este documento.

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

- **Una categoría con UNA sola caja era el peor caso — y el 21 sep ya no lo es.** §11 lo
  aisló en una variable: con UNA fila de número pelado *"the first box"* acertaba **0/4**, y
  con DOS filas **4/4**. Hoy, con el fixture dejando **una sola caja por categoría**, esa
  misma pantalla resuelve **10/10**. Se deja escrito porque el razonamiento de §11 sigue
  siendo bueno —una fila sin hermanas no dice de qué clase de cosa es— pero **la medida ya
  no lo respalda**, y nadie debería diseñar contra ella sin volver a medirla.
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

Una lectura cuesta 630 ms —`axe describe-ui`, y no se puede bajar: `describe-ui` sólo
acepta `--udid` y `--point`, no hay nada que saltarse—. El caché guarda el último
`[]Element` leído y un bit `dirty`:

- cualquier acción que pueda mover la pantalla pone `dirty`;
- un paso de sólo lectura con `dirty == false` **reutiliza** lo que hay y se ahorra los
  630 ms enteros;
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

### 2.3 Consumir la decisión antes de actuar

De `jev-ultrafast`, y cuesta cero: la elección se borra del estado **antes** de mover un
dedo, para que un reintento no pueda tocar dos veces.

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
