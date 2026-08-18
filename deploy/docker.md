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

The image is `python:3.13-alpine` with the package copied in: there are no dependencies
to install, so it builds in seconds and runs anywhere Python does, ARM included.

## The session

The container cannot log in on its own — the flow needs a browser once (see the README).
Do it on your machine, then hand the result over one of two ways.

**Copy the file into the volume** (`~/.config/laliga-fantasy/tokens.json` is where the
login leaves it):

```bash
python3 fantasy.py auth browser
python3 fantasy.py auth code '<the redirect URL>'
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

**Put authentication in front of it.** Anyone who can reach the page can spend your money.
A reverse proxy with basic auth on a private network is the minimum; do not expose it to
the internet.
