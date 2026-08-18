# Estado, agosto 2026

Donde se quedo el proyecto y que falta. Escrito para retomarlo dentro de meses sin releer
el codigo entero.

## Que hay montado

Un solo binario Go (`cmd/fantasy`), sin dependencias. Python se borro entero en el PR #1:
ya no existe `fantasy/`, `fantasy.py` ni los trece comparadores de `tools/`. Lo que el
arnes diferencial verifico antes de desaparecer esta anotado en `go-port.md`.

Corriendo en la Raspberry como `laliga-fantasy`, detras de Caddy en
`https://fantasy.platanosverdes.com` (solo tailnet, sin basic_auth: la puerta es la red).
La tarjeta esta en el homepage bajo el grupo *Services*, con `siteMonitor` a `/healthz`.

**Escrituras activas.** Nada se ejecuta sin confirmarlo en la pagina: cada operacion se
prepara, se ensena con su importe y lo que deja en el banco, y se confirma con un token de
un solo uso que caduca a los 120 s. `--read-only` las rechaza todas.

## Como se despliega

1. PR contra `main` en `laliga-fantasy` y merge en squash.
2. El workflow `auto-tag` crea `vAAAA.MM.DD.N` **con punto**. El sufijo con guion rompe el
   `sort -V` de rpi-services y deja el despliegue fijado a un tag viejo: paso, y costo
   desplegar la imagen de Python encima de la de Go sin darse cuenta.
3. En `rpi-services`, `versions.env` -> el tag nuevo, y push. **El webhook despliega solo.**
   Lanzar ademas un `docker compose up --build` a mano encola una segunda compilacion Go en
   ARM (cinco minutos cada una) y llegaron a juntarse siete.
4. Un reinicio a medias de una confirmacion la deja sin respuesta: no desplegar mientras
   alguien esta pujando.

## Cosas que solo se saben rompiendolas

- La API **rechaza una segunda puja** sobre el mismo anuncio con un 400 pelado. La puja
  propia viaja *dentro* del anuncio del mercado (`bid: {id, money, status}`) y es el unico
  sitio donde se expone: `GET .../bid` es 405.
- El techo rentable (`ideal_bid`) no esta en el modelo, vive en la ficha de futbolfantasy
  (`parsePujaIdeal(N)` en su HTML). Sin leerlo, el servidor avisaba de que "futbolfantasy no
  le ve rentabilidad" en todas las pujas.
- Las fotos de los jugadores no estan en la lista publica: solo en las plantillas y en el
  mercado.
- `display:flex` gana al atributo `hidden`. Toda clase que se oculte asi necesita su regla
  `[hidden]{display:none}`.

## Que falta

- **Escrituras sin probar en vivo**: `modify_bid`, `cancel_bid`, `accept_offer`,
  `decline_offer`, `pay_clause`, `raise_clause` y `save_lineup` estan implementadas y
  validadas con `prepare` + `dry_run`, pero solo `bid` se ha ejecutado de verdad.
- **Tests**: solo `engine` y `writes` tienen. El plan de cambios (`advice/swaps.go`) y el
  render no tienen ninguno, y el plan es el que mas se va a tocar.
- **El plan de cambios** solo propone intercambios de uno por uno y de la misma posicion.
  Falta: vender dos para comprar uno, y tener en cuenta el calendario (un rival facil las
  proximas tres jornadas vale mas que uno dificil).
- **Cache de crests**: se llena a mano con `crests.json`; si aparece un equipo nuevo, no
  tiene escudo hasta que se regenera.
- El CLI sigue con `(C)`/`(F)` para local y visitante mientras la pagina usa 🏠/✈️: los
  emojis miden distinto en cada terminal y descuadran las tablas.
