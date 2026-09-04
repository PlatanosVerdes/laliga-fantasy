# Where this stands — August 2026

Written to be picked up months later without re-reading the code.

## What exists

One Go binary (`cmd/fantasy`), no dependencies. Python is gone: the port finished on
18 August 2026 and `fantasy/`, `fantasy.py` and the thirteen comparison harnesses in
`tools/` were deleted with it.

It runs on the Raspberry Pi as `laliga-fantasy`, behind Caddy at
`https://fantasy.platanosverdes.com` — reachable inside the tailnet only, with no basic
auth, because the network is the door. The homepage card sits under *Services* with a
`siteMonitor` on `/healthz`.

**The comparator** answers the question the player card never did: not "how good is he" but
"instead of whom". A `+` on the card and on every squad row fills a tray at the bottom of the
page (kept in `localStorage`, so the live re-render cannot wipe it); the tray also has a search
box, because adding a player should not mean finding him in a table first (`?q=` searches the
same world, accent-insensitive, prefix matches first). `Comparar` lays them out in one table, and
`+ mis MED` pulls in your own players of that line, best score first. With one
outsider against your own, a verdict line says whether he improves your best in that position,
only your worst, or none of them, and what the difference costs. `GET /api/compare?ids=…`
(`internal/server/compare.go`) builds it from the world already in memory, so it costs no LaLiga
request; only the profitable ceiling is fetched from futbolfantasy, in parallel and cached.

**Writes are on.** Nothing executes without being confirmed on the page: every operation is
prepared, shown with its amount and what it leaves in the bank, and confirmed with a
single-use token that expires after 120 s. `--read-only` refuses all of them.

## Deploying

1. Branch, PR against `main`, squash merge.
2. The `auto-tag` workflow creates `vYYYY.MM.DD.N` — **with a dot**. A dash suffix breaks
   rpi-services' `sort -V`, which reads `v2026.08.18-36` as older than `v2026.08.18.7`; that
   is how a Python image once got deployed on top of the Go one without anybody noticing.
3. In `rpi-services`, point `LALIGA_FANTASY_VERSION` at the new tag and push. **The Pi's
   webhook deploys on its own.**
4. Do not also run `docker compose up --build` over ssh: it queues a second Go build on ARM
   (five minutes each) and seven of them once piled up.
5. Do not deploy while somebody is confirming an operation. A container replaced mid-request
   leaves the confirmation without an answer — and the bid it was confirming still went
   through.

## Logs

The container prints JSON on stdout (`FANTASY_LOG_JSON=1`) at info level; Vector's docker
source ships every container to VictoriaLogs with no per-service configuration. Query by
`container:laliga-fantasy`. Port 9428 is not published on the host, so a query has to go
through a container on the same network (`docker exec vector wget -qO- ...`) or the Grafana
data source.

## Things only learnt by breaking them

* The API **refuses a second bid** on the same listing with a bare 400. Your own bid travels
  *inside* the market entry (`bid: {id, money, status}`) and that is the only place it is
  exposed: `GET .../bid` is 405.
* The profitable ceiling (`ideal_bid`) is not in the model. It lives in futbolfantasy's page
  as `parsePujaIdeal(N)`. Without reading it the server warned "futbolfantasy sees no margin"
  on every single bid, whatever the amount.
* Player photos are absent from the public players list. They appear only in squads and in
  the market payload, which is why faces are collected from both.
* `display:flex` beats the `hidden` attribute. Any class hidden that way needs its own
  `[hidden]{display:none}` rule.
* Go's `%v` goes scientific at ten million — the middle of every price in this game. One
  `render.PyFloat` decides how a number becomes text, everywhere.
* Blind surname matching once marked 24 healthy players injured (Pau López inherited Diego
  López's injury). Absences match on unique surnames plus compatible first names.
* Cash cannot be read for rivals. It is reconstructed from the transfer log and anchored on
  the one balance that *can* be read — ours — so the residual error is only the difference in
  daily rewards claimed.

## What the port was checked against, before Python was deleted

Every piece was compared against the Python implementation over the same frozen snapshot:
the model (52 fields × 729 players), 141 scraped pages, 596 matcher pairs, 22 advice buckets,
the policy plan on real and synthetic squads, 74 render cells, 15 tables, the whole page byte
for byte (796,081 bytes) built from Go's own world, 36 scheduler decisions, and 11 write calls
with 15 validation rows — none of them sent. The Go halves of those harnesses are still here
and still useful on their own: `model --json`, `advise-json`, `plan`, `match`, `scrape`,
`cells`, `section`, `shell`, `page`, `calls`, `checks`, `wake` and `probe`. With
`FANTASY_FREEZE=1` they read from cache and refuse the network.

## Loans (premium, so not built)

The game added loan offers, and the routes were mapped by probing before finding out the feature
is premium — so nothing reads them. Kept here rather than in dead code, because reading them cost
twelve API calls on every rebuild for something the account cannot use:

* `POST   .../league/{league}/loan` — make a loan offer (body unknown, never sent)
* `GET    .../league/{league}/playerTeam/{playerTeamId}/loan` — the ones received, `[]` today
* `.../league/{league}/loan/{id}/accept | reject | cancel` — exist under other verbs

## What is missing

* **Writes not yet exercised live**: `modify_bid`, `cancel_bid`, `accept_offer`,
  `decline_offer`, `pay_clause`, `raise_clause`, `shield_player` and `save_lineup` are
  implemented and checked through `prepare` + `dry_run`, but only `bid` has actually been
  executed.
* **Tests**: only `engine` and `writes` have any. The swap planner (`advice/swaps.go`) and the
  renderer have none, and the planner is the part most likely to change.
* **The swap plan** only proposes one-for-one, same-position moves. Selling two to buy one is
  not modelled, and neither is the fixture calendar — an easy run of three matchdays is worth
  more than a hard one and the plan cannot see it.
* **Crest cache**: `crests.json` is filled by hand, so a newly promoted club has no badge
  until it is regenerated.
* The CLI still prints `(C)`/`(F)` for home and away while the page uses 🏠/✈️: emoji width
  varies by terminal and the CLI tables align by counting columns.
