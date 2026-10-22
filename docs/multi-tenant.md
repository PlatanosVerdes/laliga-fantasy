# One instance, many players — plan, October 2026

The goal: anyone can host one container and everyone who uses it signs in with their own
LaLiga Fantasy account, with no admin portal and no roles. Several accounts and leagues live in
one process, the data every league shares is fetched once, and the parts that only make sense
for one league (the Necroporra, house rules) are switched on per league.

## Where it stands

- One process runs one profile (one account plus one league). Switching profile writes
  `active.json` and re-execs the binary (`cmd/fantasy/main.go`, `restart()`).
- The HTTP cache lives under the profile's folder, so data that is the same for every league
  is fetched and stored once per profile: player catalogue, LaLiga teams, current week,
  calendar, market value history, player detail, and every futbolfantasy page. Only
  `ff_ceilings.json`, crests and avatars sit in `CacheRoot` and are shared.
- The door is the network or Cloudflare Access (`internal/server/access.go` checks the
  `Cf-Access-Jwt-Assertion` on its own port). Past the door, every visitor is the same person.
- Writes are already guarded: only JSON reaches the API, and each operation is confirmed with
  a single-use token that expires after 120 s.
- House rules are per league data already (`internal/rules`): hold days, debt cap, notes.
- The Necroporra (`internal/necroporra`) is hard-wired to `tebasfury.vercel.app`, with a
  cookie and action id captured from the browser and a hand-kept roster.

## Data layers

| Layer | Key | Holds |
|---|---|---|
| Global | none | catalogue, LaLiga teams, week, calendar, market values, player detail, futbolfantasy (market, trends, absences, hierarchies, details, ceilings), crests |
| League | `leagueId` | market listing, standings, rival squads, activity |
| Account in league | `accountId + leagueId` | cash, received offers, lineup, favourites, settings, policies, forecasts, outcomes, dismissed |

Rivals are keyed by league, not by person: a manager has a different squad in every league.
Two users in the same league share the league layer and nothing else.

## Identity

The LaLiga login is the app login. The OAuth + PKCE flow in `internal/auth/login.go` already
returns a token carrying the LaLiga user id; that id becomes the user. No passwords of our
own, no user table to maintain.

- **Sessions** are server-side, referenced by a random id in a cookie that is `HttpOnly`,
  `Secure`, `SameSite=Lax`, with an idle and an absolute expiry, and revoked on logout.
  No JWT in `localStorage`.
- **Every request** resolves to one user, and every read or write is scoped to the accounts
  that user signed in with. A league is visible only to users with an account in it.
- **CSRF**: the JSON-only rule stays, plus an `Origin` check on every write. The single-use
  confirmation token is bound to the session that prepared it.
- **Tokens at rest** (LaLiga refresh tokens, Necroporra cookies) are encrypted with AES-GCM
  under a key the instance generates on first start, kept outside the data folder or given
  through `FANTASY_SECRET_KEY`.
- **HTTPS**: the host terminates TLS (Caddy, Traefik, a tunnel). The app refuses to issue a
  session cookie over plain HTTP except on `localhost`.
- **Rate limits** on login attempts and on writes, per user and per IP.
- **Who may sign in**: closed by default. `FANTASY_ALLOW` lists LaLiga user ids or emails, or
  `*` for anyone. An open instance lends its IP to strangers, and a LaLiga block on it blocks
  the host too.
- **A proxy in front** (Cloudflare Access, Authelia, oauth2-proxy) stays supported as an extra
  layer. It never replaces the app's own session.

The redirect goes to `authredirect://com.lfp.laligafantasy`, so the user pastes the address
bar back. That stays unless LaLiga's client accepts another redirect; the page should explain
the step better.

## Extras as modules

A module is something one league has and another does not. Each module declares its settings,
its tab and its fetches, and is enabled per league. Disabled, it shows nothing and fetches
nothing.

- **Necroporra** first: base URL, session cookie and action id become league settings,
  editable from the page. The roster is matched against the league's standings by name,
  with manual overrides.
- **House rules** stay data, become editable from the page, and the automatic engine always
  enforces the ones it can act on (hold days, debt cap). New rule kinds are added only when a
  real league needs them.

## Phases

Each phase ships on its own and leaves the current single-user deploy working.

1. **Shared global cache.** Global tags are stored in `CacheRoot/global` instead of the
   profile folder. `ff_ceilings.json` is written atomically under a file lock, so two
   processes no longer drop each other's entries. Switching league stops being a cold start.
2. **Identity and sessions.** Sign in with LaLiga, server-side sessions, encrypted tokens,
   `FANTASY_ALLOW`, rate limits, confirmation tokens bound to the session. With one user, it
   behaves as today after one login.
3. **Many leagues in one process.** One global world built once, one league world per active
   league on top of it, the engine and the render taking the league they work for. The
   profile switch becomes a selector instead of `syscall.Exec`. The automatic engine runs
   per account in league and never shares cash or writes across them.
4. **Modules.** Necroporra moved behind the module interface with its settings in the league
   config.
5. **Editable house rules** in the page.
6. **Self-hosting docs**: one compose file, the env vars, the reverse proxy, the first login.

## Risks

- The automatic engine spends real money. Phase 3 needs tests proving that one account's
  plan never uses another account's cash, squad or tokens.
- Migration: the first start of each phase moves files. Back up `appdata/laliga-fantasy` on the
  Pi before deploying any of them.
- LaLiga rate limits grow with the number of accounts. Phase 1 removes the duplicated global
  fetches, but per-league fetches still scale with leagues; a global request budget may be
  needed.
- Memory on the Pi: one global world plus N league worlds must stay well under the ~3 GB free
  today. Measure after phase 3 with two leagues.
