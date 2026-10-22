# Phase 3: many leagues and accounts in one process

Design for phase 3 of `docs/multi-tenant.md`, read from `main` after phases 1 and 2a. Nothing here
is built yet. Section 6 holds the decisions the owner makes before the code starts.

## 0. Findings that change the plan

1. **The market response belongs to an account, not to the league.** `model.listingOf`,
   `findBidID` and `myBid` (`internal/model/model.go`) read our own bid and offer inline from
   `/league/{id}/market`. Cached per league, user B would see and act on user A's bids. Only the
   probe's digest of the listings is league-wide. The data-layer table in `multi-tenant.md` is
   corrected accordingly.
2. **The cache key has no account in it.** `httpx.cachePath` is `sha1(tag|URL)` under
   `config.TagDir(tag)`. `/v4/user/me`, `/leagues`, money, offers, market and the own lineup are
   told apart only because `CacheDir` is per profile today. A cache per league without an account
   scope would hand one account's cash and offers to another: the likeliest way for money to cross.
3. **A data race is already waiting.** `buildDocument` (`cmd/fantasy/main.go`) assigns
   `render.Build`, `render.FFPages` and `render.Crests` on every render; two tenants rendering at
   once race on them.
4. **Most types are already instances.** `state.State`, `engine.Engine` (inputs in `Deps`) and
   `writes.Guard`. The work is in what `cmdServe` closes over, not in the types.

## 1. Globals that carry one account or league

| Global | Carries | Replacement |
|---|---|---|
| `config.ConfigDir/StateDir/CacheDir`, the `*File` paths, `Account`, `League`, `Use()` | the active profile's paths | a `config.Profile` value from `config.ProfileFor(account, league)`; the globals are deleted last (3.11) |
| `auth` mutexes, `Load`/`Save` on `config.TokenFile`, `Bearer()` | the one session | `auth.Source` per account (`auth.For(account)`), shared by that account's tenants so refresh-token rotation stays serial |
| `auth.Store` → `config.SetActive` | the new account becomes the active one | `Keep` only; the session's selection replaces it |
| `api.Client` + `auth.Bearer()` | an account with nothing in it | four bound types (section 2) |
| `config.TagDir`, `httpx.cachePath/Invalidate` | which cache folder | `httpx.Request.Scope`, a `CacheScope` only `config.Profile` can make, plus a level per tag |
| `writes.Send` → `auth.Bearer()`, `Args.LeagueID/TeamID`, `Rehearsal` | who pays, where | `Member.Send`; the guard stamps league and team; rehearsal becomes a `Guard` field |
| policies, outcomes, dismissed, rewards, rules, forecast, clausulazos, necroporra, services | per-league files through `config` paths | a store per package, opened from a `Profile`, each with its own lock |
| `model` build tracker, `server.Rendering` | progress of the one build | a `*model.Tracker` per build (global, league, tenant) |
| `model.absenceWatch` | global | stays; only the global builder writes it |
| `render.Crests/Build/FFPages` | global, written per render | set once at start (fixes the race) |
| `cmdServe` closure vars (`leagueID`, `teamID`, `world`, `guard`, `engineRef`, `automate`, `fixLineup`, `claimReward`…) | the whole tenant | `tenant.Tenant` and its methods |
| `server.Server.state`, `opts.Client/Guard/LeagueID/MyTeamID/...`, page cache, `leagueID()/myTeamID()` | one tenant | resolved per request; the page cache moves into the tenant |
| `gate.go` `owns(who, config.Account)`, `useAccount/logout` (`SetActive` + restart), `HasSession` | the panel follows one profile | a selection per session plus a registry lookup |

**Scope is an explicit typed value, not a `context.Context` value**, which is not type-checked.
Deleting `policies.Load()`, `auth.Bearer()`, `api.New()`, `writes.Send` and the config globals
makes every caller that forgot the scope fail to compile.

## 2. Types

**API bound by what it reads** (`internal/api`):
- `api.Global`: no token, global cache. Week, calendar, teams, player, market value, players,
  daily rewards, formations.
- `api.Account`: `Me`, `Leagues`, `Member(league, team)`, which checks the team against `Me` or
  the standings.
- `api.League`: standings, squads, activity, weekly lineups, read with a member's token (6.2).
- `api.Member{account, league, team}`: money, favourites, own lineup, offers, market, reward
  status, `Send`. **No method takes a league or team id**, so `Money(otherTeam)` cannot be written.

**Cache levels.** Global: today's `GlobalTags`. League: `squad, standing, activity, lineup`.
Account: `me, leagues`. Member: `market, money, offers, favourites, own_lineup` (split from
`lineup`), `reward`. `httpx` refuses a request whose scope level differs from its tag's;
`Invalidate(scope, tags...)` drops only that scope.

**Three worlds** (new `internal/world`):
- `world.Global`, one per process: an immutable snapshot behind `atomic.Pointer` with a version.
  Week, teams, raw players, the futbolfantasy bridge, fixtures, schedule, team strength, price
  curves, base player rows and scores. One refresher goroutine owns it; on a live match it alone
  invalidates `players/week/calendar`.
- `world.League`, one per league with an active tenant, reference-counted: standings, ownership,
  league teams, activity, lineups, the activity digest. Built through `singleflight` per league
  and stamped with the global version it used, so it is never built twice.
- `tenant.Tenant`, one per account in a league, made only by `Registry.Open`: a `Profile`,
  `*api.Member`, its stores, `*state.State`, `*engine.Engine`, `*writes.Guard`, the render cache,
  trackers, an `acting` mutex and a cancel func.

**Composing.** `model.Compose(global, league, member, state, house)` copies the ~600 player rows
and attaches market, offers, money, favourites, ownership, the hold rule, the cash model and the
clause calendar. `Universe` keeps its shape, so render, advice and policies do not change.

**Engine: one goroutine per tenant**, `engine.Engine` unchanged with `Deps` closing over the
tenant. The probe reads activity through `world.League`, coalesced per league; only the market
read is per member. `claimReward`, `automate`, `fixLineup` move from closures to tenant methods.

**Locking.** `t.acting` covers `automate`, `claimReward`, `fixLineup` and confirmations, so the
manual and automatic paths never act on the same cash at once. Tenants never share a lock that
matters for money (cash belongs to a team in a league). Process-wide: the read-only global
snapshot, one `auth.Source` mutex per account, a render semaphore of 1–2 on the Pi, and a
`recover` per tenant goroutine.

**Server.** Each session records its selected `Account, League`. `requireSession` resolves
`registry.For(user, account, league)` after `owns` and league membership, else 403/404. Handlers
take the tenant (`s.scoped(...)`). Switching sets the selection and calls `registry.Ensure`: no
`SetActive`, no restart. `active.json` stays as the CLI default and first selection.

## 3. LaLiga request budget

Read from the code, not measured. T teams (~10), M own players with a player-team id (~18),
A activity pages (3–5).

- **Probe:** 2 requests, every 2 min while watched or busy, every 8 min idle: 360–1,440 a day.
- **Rebuild:** activity, market, money (A+3); offers (M, 1 min TTL); lineups (T, 5 min); standings
  and squads (1+T, 30 min, dropped on each movement); player status (M, global, 10 min); market
  values (≤18, 12 h); favourites (1). About 25–30 typical, ~70 after a transfer.
- **Per league:** roughly 3–5k a quiet day, 10–12k a matchday.
- **Scaling:** global O(1) after phase 1; league reads O(leagues) with singleflight; member reads
  O(tenants). Three users in two leagues ≈ 2.3× today, not 3×. The danger is bursts on the same
  15-minute ceiling and on kick-offs.

**Limiter**, in `httpx` as an `http.RoundTripper` (also the test seam):
- A token bucket per host (`fantasy-api.llt-services.com`, `login.laliga.es`); start at ~2 req/s,
  burst 10, 2 in flight. futbolfantasy keeps its own throttle.
- Priorities: P0 writes and the reads right before a write; P1 interactive pages; P2 probes;
  P3 rebuild reads; P4 enrichment. A clause raid never waits behind another tenant's rebuild.
- Tenant schedules jittered by a hash of (account, league).
- On 429, P2–P4 stop for every tenant for `max(Retry-After, 1→2→4…30 min)` with jitter; P0 goes
  on, one every 5 s.
- Soft daily budgets per tenant and global, shown in `/healthz`; going over stretches the tick,
  never blocks writes.

## 4. Memory on the Pi

Today ~65 MB: runtime ~20, global data 5–8, universe 2–4 (two during a compare), render peak
10–20, page cache 2–5, GC headroom. Shared once: runtime, global world, ceilings, crests, the
futbolfantasy detail cache. Per league 1–3 MB; per tenant 5–10 MB. Four tenants in two leagues
≈ 90–115 MB. Safeguards: `GOMEMLIMIT` (e.g. 512 MiB), drop a tenant's page cache after 30 min
unwatched, render after a rebuild only for tenants with watchers.

## 5. Pull requests, in order

Each leaves the single-user deploy working; until 3.10 the registry holds one tenant built from
`config.Active()`.

- **3.0** Test seams and the render race: swappable `RoundTripper`, a fake LaLiga server that
  records bearer, method, path and body; `render.*` set once. *Test:* two concurrent renders
  under `-race`.
- **3.1** `config.Profile`. *Test:* every path equals today's.
- **3.2** A store per profile for policies, outcomes, dismissed, rewards, forecast, rules,
  clausulazos, necroporra, services. *Tests:* writes on A invisible on B; parallel updates under
  `-race`.
- **3.3** `auth.Source` per account. *Tests:* concurrent refreshes write only their own
  `tokens.json`; A stopped leaves B working; one account's tenants refresh once.
- **3.4** Cache scope and tag levels, singleflight, the own-lineup tag split. *Tests:* the same
  market URL for A and B gives two files and B never reads A's; `Invalidate(A)` leaves B; a
  member tag with a global scope is refused.
- **3.5** Bound API types; `api.New()` removed. *Test:* every request from `Member A` carries A's
  bearer and only A's league and team.
- **3.6** Writes bound to `Member`. **Money-safety:** for every operation, A's call path holds A's
  league and team and never B's; `Guard.Cash` reads only A's team; a token prepared on A fails
  on B; a sale or acceptance for a player not in A's universe is refused.
- **3.7** The model split: `BuildGlobal`, `BuildLeague`, `Compose`. *Test:* on the frozen
  fixtures, `Compose` equals today's `Build` field for field.
- **3.8** The limiter. *Tests:* P0 jumps 100 queued P3; a 429 pauses P2–P4 everywhere and lets P0
  through spaced; jitter spreads N engines.
- **3.9** `world` and `tenant.Registry`; `cmdServe` becomes registry plus default tenant.
  **End-to-end money-safety on the fake API**, two tenants in one league with different cash
  raiding the same player: each `pay_clause` uses its own bearer, team and an amount within its
  own cash and limit; A's policies never write with B's token; A stopped leaves B acting; both
  `automate` concurrently under `-race`; one squad fetch per URL per TTL across both.
- **3.10** The server scoped per request and the selector. *Tests:* every route, user U1 against
  U2's tenant → 403/404; A's live events never reach B; switching does not restart; logging an
  account out stops its engines.
- **3.11** Delete the config globals; a test fails if `internal/` mentions them.
- **3.12** Measure heap and RSS with 1, 2 and 4 tenants and request counts over a week; set
  `GOMEMLIMIT`; update the docs.

Back up `appdata/laliga-fantasy` on the Pi before deploying 3.2 or later. The config and state
layout on disk does not change; new cache folders are additions.

## 6. Decisions for the owner

1. **Automation for a tenant nobody opens.** A newly added tenant starts manual, with an explicit
   per-tenant "auto" switch (recommended). The existing tenant keeps today's mode.
2. **Whose token reads shared league data.** Any member, a fixed reader, or each member for
   itself (no sharing, more requests).
3. **Logs.** `fantasy.log` and `usage.log` are process-wide and record automatic actions. They
   need a tenant field and must not be readable by another user.
4. **House rules** shared by members of the same league, or a copy per account (phase 5 decides
   the editor; the storage is chosen here).
5. **Limits.** Unknown whether LaLiga limits per IP or per token; rates and budgets are guesses.
   A week of per-scope counters on today's deploy, before 3.9, is worth it.
6. **CPU.** A render costs ~4 s on the Pi; rendering only for watched tenants is required.
7. **Necroporra's automatic vote** runs only for tenants that have `necroporra.json` until the
   module work in phase 4.
8. **Ship the limiter (3.8) before the registry (3.9)**, so it is proven with one tenant first.
