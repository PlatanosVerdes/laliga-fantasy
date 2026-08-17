# Hosting the report on the Raspberry

The report is a single self-contained HTML page, so `fantasy serve` is all that runs: it
regenerates on an interval and serves the last good copy from memory. Nothing in
`rpi-homeserver` needs to change — this is a personal service, so it goes in `rpi-services`
and Caddy reaches it by container name over `media-network`, per `docs/add-service.md`.

## 1. Pin the version

`rpi-services/versions.env`:

```bash
LALIGA_FANTASY_VERSION=v0.1.0
```

## 2. Add the service

`rpi-services/docker-compose.yml`:

```yaml
  laliga-fantasy:
    build: https://github.com/PlatanosVerdes/laliga-fantasy.git#${LALIGA_FANTASY_VERSION}
    container_name: laliga-fantasy
    restart: unless-stopped
    profiles: [fantasy, all]
    mem_limit: 256m
    memswap_limit: 256m
    environment:
      TZ: ${TZ}
      # JSON on stdout, which Vector already collects from every container.
      FANTASY_LOG_JSON: "1"
    volumes:
      # Session, cache, settings and report all live here. The session is the only
      # thing that cannot be regenerated, so this must persist.
      - ${APP_CONFIG_PATH}/laliga-fantasy:/app/data
    networks:
      - media-network
```

No `ports:` — Caddy proxies it. `--interval 3600` is the default; the market moves once a
day, so hourly is already generous.

## 3. Caddy route

`rpi-services/config/caddy/services.caddy` (all `*.caddy` there are auto-imported):

```caddyfile
https://fantasy.platanosverdes.com {
    log
    import cf_tls
    reverse_proxy laliga-fantasy:8000
}
```

DNS needs nothing: `sync-pihole-dns.sh` reads the hostname out of the Caddy config on every
deploy.

## 4. Bootstrap the session

The container cannot complete a login on its own — the flow needs a browser. Do it once on
the Mac and copy the result:

```bash
python3 fantasy.py auth browser
scp data/tokens.json raspi:/mnt/appconfig/laliga-fantasy/tokens.json   # APP_CONFIG_PATH
```

From then on the container refreshes the token by itself. `/healthz` reports how much life
the session has left, so a Prometheus blackbox probe against it (they already run one) turns
"my token died" into an alert instead of a surprise.

When the refresh token does eventually expire, repeat those two lines.

## 5. Homepage tile (optional)

`rpi-homeserver/config/homepage/services.yaml`:

```yaml
- Personal:
    - LaLiga Fantasy:
        icon: laliga.png
        href: https://fantasy.platanosverdes.com
        server: my-docker
        container: laliga-fantasy
```

## Endpoints

| Path | Purpose |
|---|---|
| `/` | the report |
| `/report.json` | same data as JSON, for anything else you want to build |
| `/healthz` | `200` when the last refresh worked, `503` otherwise; includes session TTL |
| `/refresh` | force a regeneration now |

`/refresh` and `/report.json` are unauthenticated, so keep the route behind Tailscale as the
rest already are. It only ever reads.

## Alternative: no container

If you would rather not run a service, a cron entry in `rpi-services/scripts/crontab` writing
into a directory Caddy serves works too — but Caddy lives in `rpi-homeserver`, so it would
need a new volume mount there. That is why the container is the better fit.
