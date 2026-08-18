# Porting to Go

## Why

Not for speed. The Python version answers a page in milliseconds and idles at ~50
requests an hour; nothing here is CPU-bound. The reasons are the ones that made the
refresh loop awkward to write:

* **Triggers want to be concurrent.** A scheduled clause raid, an auction closing and a
  live match are three independent timers. In Python they share one loop and one sleep,
  so the code has to compute a single next-wake and reason about which of the three it
  belongs to. Three goroutines on three timers is the shape of the problem.
* **A write should be able to interrupt.** Today a nudge event cuts the sleep short. With
  channels the loop is a `select` and interruption stops being a special case.
* **Deploying is a binary.** The Pi runs it as one static file with no interpreter, and
  cross-compiling for ARM is one command.
* **Types would have caught real bugs.** Two of the ones found this week were a
  misspelled cache tag and a shadowed variable holding the wrong week's calendar. A
  struct field and a compiler catch both.

## What is *not* being ported

The two things Python does better here stay:

* **The futbolfantasy scrapers**, and with them the cross-source name matching. They are
  regex-and-heuristics over HTML that changes without notice, and they are the most
  fragile code in the project. Rewriting them buys nothing and risks the parsing that
  took the longest to get right. `fantasy.py bridge` dumps everything derived from them,
  already keyed by LaLiga player id, and Go runs it as a subprocess: what crosses the
  boundary is data, never parsing.
* **The report's CSS and JS**, and the static HTML of the pitch, the drawer and the bid
  modal. Already written, already validated; they live in `assets/` and both implementations
  serve the same bytes rather than each keeping a copy.

So the target is not a rewrite but a split: Go takes the engine (scheduler, API client,
writes, HTTP server, policies), Python keeps the scraping, and they talk over a
subprocess boundary with JSON.

## Order of work

Each step ends with something runnable and something proven, and the Python version
keeps working throughout.

| # | Step | Done when |
|---|---|---|
| 1 | Module skeleton, XDG config, cache with tags + stats | `fantasy-go cache` prints the same figures as `fantasy.py cache` |
| 2 | Auth: bearer, expiry, refresh with rotation, env seeding | `fantasy-go auth status` matches `fantasy.py auth status` field for field |
| 3 | API client and types for the fifteen endpoints in use | `fantasy-go probe` returns the same digest as the Python probe |
| 4a | The model's structure: identity, ownership, market, fixtures | **harness green**: 729 players × 22 fields, 53 listings, 10 fixtures |
| 4b | The scoring half: xPts, price prior, score, ranks | **harness green**: 729 players × 48 fields, identical to 6 decimals |
| 4c | Cash reconstruction, activity, offers, favourites, raids | **harness green**: 52 fields, 82 events, 13 managers' cash to the euro |
| 5 | Scheduler and the refresh cycle, on channels | **36 decisions identical** to Python across 9 scenarios × 4 cadences; 7 engine tests green under `-race` |
| 6 | Writes with the two-step guard and the id semantics | **11 calls byte-identical, 15 validations identical**, 12 guard tests green; no live write yet |
| 7a | The page's CSS and JS become files | **page byte-identical** once the clock decimals are masked |
| 7b | HTTP server, JSON API, SSE | **/api/state identical live**: 729 players x 52 fields, 82 events, 13 managers' cash |
| 7c | The HTML rendering: primitives first | **52 cells identical** |
| 7d | The sections, one table at a time | **15 tables + 2 shapes byte-identical**, plus every empty state |
| 7e | The page shell: head, widgets, tabs, footer | page renders from Go, SSE swaps, drag-and-drop still works |
| 8 | Policies and the automation | plan parity on recorded payloads, then armed |

## The differential harness

This is what makes the port verifiable rather than hopeful, and it is why step 4 comes
before anything that spends money.

Both implementations read the **same frozen cache directory** — the scrapes and API
responses already on disk — and write their model to JSON. A comparator walks both trees
and reports every field that differs, with a tolerance for floats:

```bash
# One frozen snapshot: the cache both sides read, plus the session and settings.
snap=/tmp/frozen
mkdir -p $snap/cache && cp data/{tokens,settings}.json $snap/ && cp data/cache/*.cache $snap/cache/

FANTASY_FREEZE=1 FANTASY_DATA_DIR=$snap python3 fantasy.py report --json --output $snap/report.html
FANTASY_FREEZE=1 FANTASY_DATA_DIR=$snap fantasy-go model --json > /tmp/go.json
python3 tools/diff_model.py $snap/report.json /tmp/go.json
```

`FANTASY_FREEZE=1` is the guarantee: TTLs are ignored and the network is **refused**, so
a cache miss fails loudly instead of being fetched — which would make the two runs read
different bytes and compare nothing. It also stops the session being renewed, on both sides:
a snapshot whose token expires would otherwise stop being replayable a couple of hours after
it was taken, which is exactly what happened the first time one was reused.

Rules that keep it honest:

* **Frozen inputs.** `FANTASY_DATA_DIR` points at a copy of the cache and the TTLs are
  set to infinity, so neither side reaches the network and both see identical bytes.
* **Every field, not a summary.** Comparing totals hides compensating errors. The
  comparator walks player by player and reports the first mismatch per field with both
  values.
* **Floats need a tolerance, ordering does not.** Scores may differ in the last bits;
  rankings must not differ at all.
* **Only fields Go actually builds are compared**, and the rest are listed as pending.
  A comparison that quietly skips what is missing goes green for the wrong reason.
* **Clock-derived fields are excluded by name.** `clause_hours_left` and `hours_left`
  are counted from now, so they differ between two runs of the *same* implementation
  seconds apart — verified, and they are the only 19 fields that do.
* **A green run is a claim about those inputs only.** Several snapshots are kept — one
  mid-market, one during a live match, one with offers pending, one with a locked clause
  about to open — because the interesting bugs live in the states that are rare.

## The two harnesses

`tools/diff_model.py` compares the model, `tools/diff_wake.py` compares the decisions:

```bash
go build -o fantasy-go ./cmd/fantasy
python3 tools/diff_wake.py ./fantasy-go
```

The scenarios are the states that matter and are rare — an auction about to close, an offer
about to expire, our own match under way, somebody else's, a finished one, a matchday
closing — each at four cadences including "the page is open" and "the periodic rebuild is
overdue". Both sides take `now` as an argument, because a scheduler that can only be
observed live cannot be compared.

`tools/diff_render.py` pins the page's formatters before any section is ported. Every table
is these primitives repeated a few thousand times, so a comma where the other side writes a
dot makes every section differ and a section-level diff worthless. The inputs are the edges:
999,500 (which a naive implementation rounds to "1.000K", a thousand-fold lie in the unit),
a negative whose sign belongs in front of the separators, an absent value that is an em dash
and not a zero, a flat series whose sparkline span would be a division by zero, and a series
of four, which is not enough history to draw and must be omitted rather than faked.

`tools/diff_sections.py` compares whole rendered tables byte for byte. The rows are handed
to both implementations rather than read from a live model on each side: otherwise a
difference could be two data reads disagreeing rather than two renderers, and the point of a
byte comparison is that it has exactly one explanation.

`tools/diff_api.py` compares the two servers *live*, on the same frozen cache: it starts
from /healthz, lists which keys of /api/state each side publishes, and then reuses the model
comparator on the rows — comparing them a second way would only mean two places to keep in
step. What is still Python-only is printed rather than skipped: `budget`, `clauses`,
`rivals`, `favourites`, `policies` and `policy_actions` all come from the advice and policy
layers, which are steps 7c and 8.

`tools/diff_writes.py` compares the writes without sending any: the eleven calls literally
(method, path, body key by key) and the fifteen validation rows that decide whether an
amount is refused. Python's own guard is run with the cash reader stubbed, because a harness
must not depend on a session and must certainly not spend anything.

The cycle itself is covered by `go test ./internal/engine -race`, with the network and the
clock injected: a probe that finds nothing must not rebuild, one that finds a transfer must
say `traspaso` rather than `mercado`, a deadline must rebuild without wasting the two probe
requests, the *same* deadline must not fire twice, a nudge must cut the wait short, and a
probe that errors must still rebuild — silence is the failure mode to avoid.

## Where the two languages disagree about numbers

Found the hard way, so written down rather than rediscovered:

* **Go's default float formatting goes scientific at ten million**, which is the middle of
  the range every price in this game lives in: `%v` and `fmt.Sprint` turn 17761424.4 into
  `1.7761424e+07`. Python's `str()` only does that at an exponent of 16 or under -4. As a
  sort key it sorts wrongly while looking fine; as visible text it is simply wrong. There is
  one `render.PyFloat` now, used everywhere a number becomes text, because the failure is
  invisible and would otherwise creep back one call site at a time.
* **JSON encoding differs too**, and neither is wrong: Python writes `130960400.0` and
  `1e+16`, Go writes `130960400` and `10000000000000000`; Python writes `1e-05` where Go
  writes `0.00001`. It affects nothing that is compared today, because every JSON comparison
  parses both sides and compares values. It *will* matter the moment the Go page embeds a
  JSON blob the way the activity feed does for an unknown event — a page compared byte for
  byte would differ on a whole number's `.0`.

## Risks, and what each one costs

* **Cash reconstruction is the subtlest code in the project** — it anchors the whole
  league on one figure and folds rewards into a base. A port that is quietly wrong there
  produces plausible numbers, which is the worst failure mode. It gets its own comparison
  on every manager, not just ours.
* **The id semantics are hard-won and unobvious**: the squad-slot id where the player id
  looks right, the market id for a direct offer, factor 2 for a clause raise. They are
  written down in `writes.py` comments and must be carried across as-is, with the
  comments.
* **The two-step write guard must not get looser.** Same TTL, same single-use token,
  same refusal to act without an explicit amount.
* **Automation stays off until the harness agrees.** No standing instruction runs from Go
  until plan parity holds on the recorded payloads, and the first live run is with
  `--read-only`.
