# Running it as a service

The report is a single self-contained page and `fantasy serve` is all that runs: it
refreshes on an interval, pushes changes to connected browsers over SSE, and serves the
last good copy from memory so a failed refresh never takes the page down.

## Build and run

```bash
docker build -t laliga-fantasy .
docker run -d --name laliga-fantasy \
  -p 8000:8000 \
  -v laliga-fantasy:/data \
  laliga-fantasy
```

Or with compose:

```yaml
services:
  laliga-fantasy:
    build: .
    container_name: laliga-fantasy
    restart: unless-stopped
    ports:
      - "8000:8000"
    environment:
      TZ: Europe/Madrid
      # JSON on stdout instead of the terminal format, for a log collector.
      FANTASY_LOG_JSON: "1"
      # Optional: push the same JSON straight into VictoriaLogs / Loki / anything
      # that accepts JSON lines over HTTP.
      # FANTASY_LOG_URL: "http://logs:9428/insert/jsonline?_stream_fields=service"
    volumes:
      # Session, cache, favourites, standing instructions and the report all live
      # here. The session is the only thing that cannot be regenerated.
      - laliga-fantasy:/data

volumes:
  laliga-fantasy:
```

The image sets `FANTASY_DATA_DIR=/data`, which collapses config, state and cache into that
one directory so there is a single volume to mount. Outside a container the files follow the
XDG spec instead — see [Where things live](../README.md#where-things-live).

The image is a two-stage build: `golang:1.26-alpine` compiles a static binary, `alpine:3.21`
carries it plus the page's assets. There are no dependencies to download, so it builds
anywhere, ARM included, and the result is about 24 MB.

## The session

**Just open the page.** With no session, `/` serves the login instead of the report: it prints
the authorize URL, you sign in, the browser fails to open `authredirect://…` — which is
expected — and you paste the address bar back into the box. The container exchanges the code,
stores `tokens.json` at `0600` and builds the world. It also accepts a whole `tokens.json`
pasted in, and checks it against `/user/me` before accepting it, so a session that cannot read
anything is rejected there and then rather than hours later.

The league resolves itself afterwards, so there is nothing to configure before the first run.

The two older ways still work. **Copy the file into the volume**:

```bash
./fantasy auth browser
./fantasy auth code '<the redirect URL>'
docker cp ~/.config/laliga-fantasy/tokens.json laliga-fantasy:/data/tokens.json
```

**Or pass it in the environment**, which is friendlier to a secrets manager:

```yaml
environment:
  # The whole tokens.json...
  FANTASY_TOKENS: ${FANTASY_TOKENS}
  # ...or just the refresh token, exchanged on startup.
  # FANTASY_REFRESH_TOKEN: ${FANTASY_REFRESH_TOKEN}
```

Either variable is read **only when no token file exists**, is written to the volume at
`0600`, and rotation takes over from there — the refresh token changes on renewal, so the
variable goes stale and the file is the truth. You can drop it after the first start.

From then on the refresh token keeps the session alive, the same way the official app
does. `/healthz` reports how much life it has left, so a blackbox probe against it turns
"my token died" into an alert instead of a surprise.

## Endpoints

| Path | Purpose |
|---|---|
| `/` | the page |
| `/api/state` | everything as JSON |
| `/api/lineup` | the lineup, formations and the bench |
| `/api/player/{id}` | one player: stats, value history and the actions available now |
| `/api/events` | SSE stream: a message whenever the state version moves |
| `/api/fragments` | each section rendered, for partial swaps |
| `/healthz` | `200` when the last refresh worked, `503` otherwise; includes session TTL |
| `/refresh` | force a refresh now |
| `/api/session` | POST the pasted redirect (or a `tokens.json`) when there is no session |
| `/api/bid/prepare` · `/api/bid/confirm` | the two steps of every operation that moves money |
| `/api/always` · `/api/raid` · `/api/favourite` | standing instructions and stars |

## What it is allowed to do

By default the page can bid, sell, accept offers and pay clauses, and it carries out the
standing instructions (keep-listed, scheduled clause raids) on its own — that is what they
are for, since a clause opens when it opens.

| Flag | Effect |
|---|---|
| *(none)* | buttons work, standing instructions execute |
| `--no-auto` | buttons work, standing instructions are shown but not executed |
| `--read-only` | nothing that moves money is possible |

Writes go through a two-step confirmation with a single-use token, and the standing
instructions only ever touch players you named explicitly, never below a floor you set.

**Keep it off the open internet.** Anyone who can reach the page can spend your money. A
private network (a tailnet, a VPN) is the minimum, with basic auth on top if more than one
person can reach it; do not expose it to the internet.
