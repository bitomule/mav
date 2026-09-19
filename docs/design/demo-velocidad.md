# La demo de velocidad — diseño y aritmética

Nodo `mav-demo-velocidad`, 19 sep 2026. **No hay demo grabada ni medida tomada.** Esto es el
diseño, y sobre todo la aritmética que hay que hacer **antes** de grabar nada.

La demo es un A/B: **el mismo recorrido en la misma app, dos veces, lado a lado.** El lado
"antes" es como se hacía —el agente se vuelca el árbol de accesibilidad y elige él—; el lado
"después" es `mav ui find`. No cambia nada más.

---

## 0. La aritmética, primero, porque es lo que evita la decepción

Lo medido hasta hoy, y son números nuestros:

- jev decide en **378 ms**; el modelo grande tarda **3,05 s**. Ocho veces más rápido.
- Pero **decidir es el 9–11% del reloj de una tarea.** El resto es el simulador: arrancar,
  instalar, esperar a que pinte.
- Y todo el trabajo de decidir es **menos del 1% de lo que cuesta una sesión.**

De ahí sale el techo, y conviene escribirlo antes de que alguien monte el vídeo:

> **Si decidir es el 9–11% del reloj, el máximo de mejora en tiempo total es ~8–9% aunque el
> modelo fuera instantáneo.**

Un 9% no se ve en un vídeo lado a lado. Así que **el reloj no puede ser el titular de esta
demo**, y una demo montada esperando que lo sea sale decepcionante por construcción.

El reloj sólo se mueve de verdad si el lado "después" hace **menos vueltas al modelo**, no
sólo vueltas más rápidas. Por eso se miden las tres cosas y el orden del titular es:

1. **Lo que entra en el contexto del agente** — y esto no hay que instrumentarlo, se mide
   solo: el árbol de una pantalla densa son **177 líneas y 19.819 bytes** (iOS Ajustes →
   General, medido el 19 sep 2026), y la respuesta de `find` es **una línea**. Ésa es la
   comparación de tokens, y vive en el lado del agente, no dentro de `mav`.
2. **Vueltas al modelo** — cuántas veces hay que preguntar para hacer el mismo recorrido.
3. **Reloj** — número secundario. **Puede salir empate, y si sale, se dice.**

**`find` no reporta tokens y no debe hacerlo.** Llama a jev, no al modelo grande: lo que
gasta `find` no es donde está el ahorro. El ahorro es el árbol que el agente deja de meterse
en su propio contexto, y eso se cuenta por fuera.

### Por qué el A/B y no un cronómetro suelto

Cronometrar sólo la decisión enseña el 8x de verdad pero mide una rebanada pequeña, y siempre
se puede sospechar que la rebanada está elegida a favor. Cronometrar un flujo entero enseña
sobre todo el simulador y diluye la mejora.

El A/B quita el problema: **los dos lados pagan el mismo simulador, el mismo arranque y el
mismo pintado, así que eso se cancela en la resta.** Lo que sobra en la diferencia es nuestro.

### Y la historia honesta no es la velocidad

La velocidad es el gancho. **El cuerpo es la corrección**, y ahí los números son mejores:

- **Cero respuestas equivocadas en 10 pruebas** de `find`, incluidos 4 objetivos que no
  estaban en la pantalla y se resolvieron como "no está".
- **29 pantallas con botones de Borrar y Cerrar sesión: ningún acierto equivocado pasó la
  guarda.**
- **26 objetivos que no comparten ni una palabra con el elemento correcto: 16 de 16.**
- El árbol **recortaba a 80 elementos en silencio**, así que había cosas en pantalla que
  ningún comando podía enseñarte. Eso ya no pasa.

---

## 1. Qué hacen los demás, y por qué esto es más riguroso que lo publicado

Verificado el 19 sep 2026, leyendo código donde había código que leer.

**`browser-use`** — su README dice más de lo que hace su código, y esto se comprobó a mano:
`browser_use/tools/service.py`, las **dos** variantes de `done()` devuelven
`success=params.success`, que es literalmente el booleano que escribió el modelo. Nadie lo
verifica. Su `agent/judge.py` corre **después** de que el agente ya haya parado y su propio
comentario dice que no sobrescribe ese valor. Qué enseñan: vídeos de flujos enteros, **sin
cronómetro**. Qué esconden: que "terminado" es autodeclarado, y que un paso "va bien" si el
manejador no lanzó una excepción.

**UI-TARS** (paper) — cronometra **una sola decisión**, no un flujo: 4,97 s por consulta en el
7B y 12,1 s en el 72B sobre *element grounding*, con la implementación por defecto de
HuggingFace. Esconde que eso no es la latencia de un modelo servido en producción, y no enseña
ningún recorrido.

**Holo3.1** (2 jun 2026) — se presenta como el primer agente de *computer use* local de grado
producción, y su demo es **local contra nube**: el argumento es el viaje HTTPS de cientos de
milisegundos por paso. Cronometra latencia por paso en hardware de consumo. Esconde qué
hardware, y admite que el sub-segundo por paso es expectativa para Q4 2026.

**Móvil nativo**, que es donde estamos y donde hay menos:

- **`sim-use`** (CLI sobre las APIs de accesibilidad de Apple y el pipeline HID del simulador)
  afirma **"~300 ms por vuelta observar-actuar"** gracias a un demonio por dispositivo, y que
  un árbol completo en iPhone físico "cuesta unos segundos", con `ui --fast` un 40% más
  rápido. **Sin benchmark, sin vídeo, sin suite reproducible**: son observaciones escritas en
  el README. No tiene bucle de decisión — sólo primitivas que llama el agente.
- **`agent-device`** (Callstack; iOS, Android, macOS, HarmonyOS, TV, web) tiene la misma
  forma: CLI + MCP + API tipada, primitivas y no bucle. *Su blog no se pudo leer, así que esto
  queda sin verificar.*

**Y lo que importa para decidir hacer la demo: nadie ha publicado un A/B con vueltas y
tokens.** Lo que hay son vídeos sin cronómetro, latencias de una sola decisión en papers, o
porcentajes de ahorro de tokens en blogs de tooling sin vídeo ninguno. Si se miden las tres
cosas sobre el mismo recorrido, **la demo es más rigurosa que nada de lo publicado en este
espacio.** Ése es el argumento para hacerla; no hay forma de presentación que copiar.

---

## 2. El montaje

### Estado de partida, idéntico y **declarado**

Un estado preparado que no se cuenta es lo que convierte una comparación en un truco. Así que
se prepara y **se dice en el texto de la demo**:

- Mismo slot de simpool, mismo dispositivo y misma versión de iOS. **Slot de simpool siempre**;
  nada de `simctl create`.
- App instalada y terminada antes de cada pasada, lanzada con la misma receta.
- **Onboarding ya completado** en el estado guardado, para que ningún lado lo pague. Esto se
  declara.
- Mismo modelo y mismo marco de prompt en los dos lados.

### El recorrido

**Home → Ajustes → disposición de grupos → volver a Ajustes.** Tres toques, y elegido para que
**no haga falta desplazarse**: `mav ui swipe` devuelve `ok` sin mover la pantalla en los slots
probados, así que un recorrido que necesite scroll enseña un fallo nuestro en mitad de la
demo. Si la lista de Ajustes pide scroll en el dispositivo elegido, se recorta a dos toques.

Dos avisos sobre la app elegida, que condicionan el recorrido:

- En el simulador **Undolly no ejecuta su algoritmo de grupos**: la pantalla de revisión sale
  de un simulacro con datos puestos a mano. Sirve para navegar y tocar, no para enseñar
  resultados de análisis.
- **En las listas de iOS ningún literal resuelve nunca**: el sistema duplica cada fila, así que
  no hay etiquetas únicas y `find` se va siempre por la ruta del modelo, la cara. **Se dice.**
  Que gane por la ruta cara vale más que esconder por qué la toma; esconderlo sería justo lo
  que le criticamos a las demos de los demás.

### Los dos lados

- **Antes**: el agente pide `mav ui tree`, filtra con criterio y toca. **Se escribe como lo
  escribiría alguien que sabe lo que hace**: un árbol por pantalla, sin re-volcados, sin dar
  tumbos. Un "antes" torpe convierte la demo en propaganda.
- **Después**: `mav ui find "<objetivo>"` y tocar.

### Cómo se mide

Por lado y por paso: **reloj, vueltas al modelo, tokens de entrada y de salida.** Total por
pasada.

**Tres repeticiones por lado, y se publica mediana y rango.** Una sola pasada de un bucle con
un modelo dentro es ruido, y con una sola pasada se elige la favorable sin darse cuenta.

**Y se graban los dos vídeos, aunque el de antes sea aburrido.** Un antes que no se enseña es
un antes que nadie se cree.

### La regla que protege el resultado

**Si el lado nuevo no gana, eso es el resultado y se publica.** No se ajusta el "antes" hasta
que pierda.

---

## 3. Lo que ya se instrumentó, y lo que sigue sin existir

**Hecho:** `mav ui find` emite un bloque `cost` con `total_ms`, `tree_ms` (leer la pantalla,
que en un simulador real es la mayor parte), `model_ms` (el viaje a jev, leído de la medida
que jev hace de sí mismo y no cronometrando el subproceso) y `local_ms` (lo que queda: elegir
candidatos, renderizar, los vetos). Así el 8x se vuelve a sacar corriendo el comando, en vez
de citando un fichero que está en un portátil.

**Sin tokens, y a propósito:** `find` llama a jev, no al modelo grande, así que lo que gasta
`find` no es donde está el ahorro.

**Sin hacer, y no hace falta para el vídeo:** un arnés para el lado "antes" — un bucle de
agente que pida árbol, elija y toque contando vueltas. Para el vídeo ese lado se conduce a
mano, y lo que se enseña es el bulto del árbol llenando la pantalla, que es la imagen y es
cierta.

---

## 4. Por qué `find` y no `goto`

`goto` es el bucle que llega a una pantalla; una demo de `goto` es la app moviéndose sola y es
mucho más vistosa. **No para ésta**, por dos razones que no dependen de opinión:

- **No hay una línea de `goto` escrita.** El diseño está en `docs/design/goto.md`; el código no
  existe.
- **Desplazarse está roto**: `mav ui swipe` devuelve `ok` con el árbol idéntico antes y
  después. Un bucle autónomo que necesite scroll gira hasta agotar su tope.

`goto` va después, con las dos reglas que su diseño deja cerradas: el criterio de llegada tiene
que estar **ausente** de la pantalla de partida, y **no hay juez** que decida si ha llegado.
