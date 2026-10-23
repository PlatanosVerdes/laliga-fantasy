<p align="center">
  <img src="assets/favicon.png" width="96" alt="">
</p>

<h1 align="center">laliga-fantasy</h1>

<p align="center">
  <a href="https://github.com/PlatanosVerdes/laliga-fantasy/tags"><img src="https://img.shields.io/github/v/tag/PlatanosVerdes/laliga-fantasy?label=release&color=2f7d3a" alt="Latest release"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/PlatanosVerdes/laliga-fantasy" alt="Go version">
</p>

Decision support for [LaLiga Fantasy](https://fantasy.laliga.com/): who to sign, who to sell and
which buyout clauses to pay, given your cash, your squad and your rivals' squads.

It merges the **official API** (prices, points, your league, squads, cash and clauses) with
**futbolfantasy.com** (value trends, odds of starting, next fixture, jerarquía). It can also act
(bid, sell, accept an offer, pay a clause, save a lineup), and nothing moves without being shown
with its amount and confirmed first. `--read-only` refuses every operation.

Go 1.26 and the standard library, no dependencies.

## Quick start

```bash
go build -o fantasy ./cmd/fantasy

./fantasy auth browser              # prints the login URL
./fantasy auth code '<redirect>'    # exchanges what the browser was redirected to
./fantasy advise                    # the main command
./fantasy serve --port 8777         # the panel, with the write API
```

LaLiga Fantasy is app-only, so logging in uses the app's own OAuth client and one copy from the
browser's DevTools. [docs/login.md](docs/login.md) walks through it; after that the session renews
itself.

## Commands

| Command | Purpose |
|---|---|
| `advise` | Signings, payable clauses, sell candidates and clause-risk warnings |
| `squad` | Your squad valued, with xPts and trend per player |
| `market` | Global ranking. `--sort score\|value\|trend\|xpts\|price`, `--position DEF`, `--max-price`, `--free` |
| `player <name>` | Full profile. `--history 10` adds the per-matchday table from futbolfantasy |
| `standings` / `leagues` | League table and your leagues |
| `activity` | The league's transfer log (`--pages N`) |
| `lineup` | Who is on the pitch, who cannot play, and the best legal eleven. `--fix` saves it |
| `report` | Self-contained HTML dashboard with sortable tables (`--open`, `--json`) |
| `usage` | What the page is used for: tabs by time, clicks and sorts (`--days N`) |
| `serve` | Serve the report over HTTP with a JSON API and SSE push — see [deploy/docker.md](deploy/docker.md) |
| `probe <what>` | Dump a raw endpoint payload — use it when the API shape changes |
| `cache` | Cache size, `--clear` to wipe |

Useful flags: `--budget N` to override the cash reading, `--deep` (see [the model](docs/model.md)), `--fresh` to
bypass the futbolfantasy cache, `-v` for verbose logs.

## Docs

| | |
| :--- | :--- |
| [Logging in](docs/login.md) | The two-step login, sessions encrypted at rest, other ways in |
| [The model](docs/model.md) | xPts, the score, clauses worth paying, rival cash, the matchday in play, jerarquía |
| [Where things live](docs/data.md) | Directories, accounts and leagues, who may sign in, seeding a container |
| [Refreshing](docs/refreshing.md) | When the loop wakes up, matches, what an operation reports, clause re-reads |
| [Measuring and logging](docs/observability.md) | What the page records about its own use, and the logs |
| [Internals](docs/internals.md) | Code layout and notes on the API |
| [Running in Docker](deploy/docker.md) | The container, its endpoints and write flags |

Design notes: [clauses](docs/clauses.md), [clause window](docs/clause-window.md),
[shield](docs/shield.md), [daily reward](docs/daily-reward.md), [state](docs/state.md),
[multi-tenant](docs/multi-tenant.md), [phase 3](docs/phase-3.md).
