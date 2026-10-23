# Internals

## Code layout

```
cmd/fantasy/              the commands
internal/config/          endpoints, ids, squad rules, paths
internal/httpx/           HTTP client with on-disk TTL cache, retries and a freeze switch
internal/auth/            token store, B2C refresh, the login flow
internal/api/             official API client
internal/futbolfantasy/   the four scrapers
internal/matching/        team and player identity resolution across sources
internal/model/           the universe: squads, clauses, cash, absences, xPts, score
internal/advice/          what to do: signings, clauses, sales, offers
internal/policies/        standing instructions, and what counts as a good offer
internal/render/          the HTML page, cell by cell
internal/server/          HTTP surface: JSON API, SSE push, writes, session setup
internal/state/           the built world, its version and who is watching
internal/engine/          when to wake up and what to invalidate
internal/schedule/        deadlines, live matches, change detection
internal/writes/          the operations, their validation and the two-step guard
internal/cli/             terminal tables and colour
assets/                   the page's CSS, JS and HTML pieces
Dockerfile                dependency-free image for the homeserver
deploy/docker.md          running it as a container, endpoints, write flags
```

## API notes

Season 26/27 moved the API to `fantasy-api.llt-services.com` and put most routes under
`/v1/competition/1/`. The old `api-fantasy.llt-services.com` host is frozen on 25/26.
Watch out for the inconsistency the official app also carries: standings and squads live
under `/leagues/{id}/...`, market and buyout under `/league/{id}/...`.

Public (no session): `/v1/competition/1/players`, `/v3/teams-master`,
`/v1/competition/1/week/current`, `/v1/competition/1/calendar?weekNumber=N`,
`/v1/competition/1/player/{id}`, `/v1/competition/1/player/{id}/market-value`.

Session required: `/v4/user/me`, `/v1/competition/1/leagues`, `.../leagues/{id}/standing`,
`.../leagues/{id}/teams/{teamId}`, `.../leagues/{id}/activity/{index}`,
`.../teams/{teamId}/money` (own team only), `.../league/{id}/market`.

`/money` answers `{"teamMoney": N, "teamInvestment": N}`. Squad slots carry `buyoutClause` and
`buyoutClauseLockedEndTime` — at the start of a season every clause in the league is locked
for a couple of weeks, so the raid list being empty is the game's rule, not a bug.

Two fields to know about. `lastSeasonPoints` is **absent** for players the game re-registered
this season (Mbappé, Lamine, Vini, Bellingham, Pedri — ids 3100-3104) and zero for promoted
clubs, in the list *and* in the player detail. And `positionId` 5 is the coach: 23 of the 733
"players" are managers, excluded everywhere.
