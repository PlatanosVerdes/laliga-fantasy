"""Live server: JSON API, server-rendered fragments and a push channel.

The page is not regenerated on a schedule the browser knows nothing about. The
server refreshes on a short interval, and only the two things that actually move
minute to minute cost a request (market and activity: everything else is still
inside its cache TTL). When the data changes, the version number moves and every
connected browser is told over SSE, so it can swap just the sections that changed.

Serving is decoupled from refreshing: a failed refresh keeps the last good page up
and surfaces in `/healthz` instead of taking the page down with it.
"""
from __future__ import annotations

import hashlib
import json
import queue
import threading
import time
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Callable

from . import auth, favourites, laliga, policies, report, writes
from .logs import log

HEARTBEAT = 20


class State:
    """The rendered world, plus a version that only moves when something changed."""

    def __init__(self, builder: Callable[[], tuple[dict, dict | None, dict]]):
        self.builder = builder
        self.lock = threading.Lock()
        self.html = "<title>Generando</title><p>Generando el primer informe…</p>"
        self.sections: dict[str, str] = {}
        self.payload: dict[str, Any] = {}
        self.context: dict[str, Any] = {}
        self.version = 0
        self.generated_at: float | None = None
        self.last_error: str | None = None
        self.runs = 0
        self.fingerprint = ""
        self.universe: dict | None = None
        self.advice: dict | None = None
        self.subscribers: set[queue.Queue] = set()

    # --- refresh ------------------------------------------------------------

    def _fingerprint(self, universe: dict, advice: dict | None) -> str:
        """What counts as a change worth telling the browser about."""
        market = [(m["market_id"], m["min_bid"], m.get("bids"), m.get("offers"))
                  for m in universe.get("market") or []]
        latest = [(e.get("raw") or {}).get("id") for e in (universe.get("activity") or [])[:5]]
        digest = json.dumps({
            "market": sorted(map(str, map(tuple, market))),
            "activity": latest,
            "budget": (advice or {}).get("budget"),
            "week": universe.get("week", {}).get("weekNumber"),
            "favourites": sorted(favourites.ids()),
        }, sort_keys=True, default=str)
        return hashlib.sha1(digest.encode()).hexdigest()

    def refresh(self, *, force: bool = False) -> bool:
        try:
            universe, advice, context = self.builder()

            # Standing instructions run off the same data the page is about to show,
            # and before the payload is built so the API reports this cycle's actions
            # rather than the previous one's.
            settings = getattr(self, "policy_context", None) or {}
            if settings.get("league_id"):
                try:
                    self.policy_actions = policies.enforce(
                        universe["players"], league_id=settings["league_id"],
                        my_team_id=settings["my_team_id"],
                        allow_writes=settings["allow_writes"],
                        cash=(advice or {}).get("budget") or 0)
                except Exception as exc:
                    log.error("policies failed", extra={"reason": str(exc)[:200]})

            fingerprint = self._fingerprint(universe, advice)
            changed = force or fingerprint != self.fingerprint
            page = report.build(universe, advice, context=context,
                                activity=universe.get("activity"))
            payload = {
                "generated_at": datetime.now(timezone.utc).isoformat(),
                "week": universe["week"],
                "budget": (advice or {}).get("budget"),
                "market": universe.get("market"),
                "clauses": universe.get("clauses"),
                "activity": universe.get("activity"),
                "rivals": (advice or {}).get("rivals"),
                "cash_model": (advice or {}).get("cash_model"),
                "favourites": sorted(favourites.ids()),
                "policies": policies.load(),
                "policy_actions": getattr(self, "policy_actions", []),
                "players": universe["players"],
            }
        except Exception as exc:
            self.last_error = f"{type(exc).__name__}: {exc}"
            log.error("refresh failed", extra={"error_type": type(exc).__name__,
                                              "reason": str(exc)[:300]})
            return False

        with self.lock:
            self.universe = universe
            self.advice = advice
            self.html = page
            self.sections = report.split_sections(page)
            self.payload = payload
            self.context = context
            self.generated_at = time.time()
            self.last_error = None
            self.runs += 1
            if changed:
                self.fingerprint = fingerprint
                self.version += 1
        if changed:
            self.publish({"type": "state", "version": self.version,
                          "generated_at": payload["generated_at"]})
            log.info("state changed", extra={"version": self.version, "runs": self.runs})
        else:
            log.debug("state unchanged", extra={"runs": self.runs})
        return True

    def rerender(self) -> bool:
        """Repaint from the universe already in memory, with no network at all.

        Starring a player or arming an instruction changes what the page says but not
        the data behind it, and a full refresh for that meant a dozen requests and
        several seconds of the button looking dead.
        """
        with self.lock:
            universe, advice, context = self.universe, self.advice, self.context
        if not universe:
            return self.refresh(force=True)
        try:
            page = report.build(universe, advice, context=context,
                                activity=universe.get("activity"))
        except Exception as exc:
            log.error("rerender failed", extra={"reason": str(exc)[:200]})
            return False
        with self.lock:
            self.html = page
            self.sections = report.split_sections(page)
            self.payload = {**self.payload,
                            "favourites": sorted(favourites.ids()),
                            "policies": policies.load()}
            self.version += 1
        self.publish({"type": "state", "version": self.version})
        log.debug("rerendered", extra={"version": self.version})
        return True

    # --- push ---------------------------------------------------------------

    def subscribe(self) -> queue.Queue:
        channel: queue.Queue = queue.Queue(maxsize=8)
        with self.lock:
            self.subscribers.add(channel)
        return channel

    def unsubscribe(self, channel: queue.Queue) -> None:
        with self.lock:
            self.subscribers.discard(channel)

    def publish(self, message: dict[str, Any]) -> None:
        with self.lock:
            channels = list(self.subscribers)
        for channel in channels:
            try:
                channel.put_nowait(message)
            except queue.Full:
                pass    # a browser that cannot keep up will catch up on its next poll

    # --- status -------------------------------------------------------------

    def health(self) -> dict[str, Any]:
        tokens = auth.load_tokens()
        age = (time.time() - self.generated_at) if self.generated_at else None
        return {
            "status": "ok" if self.generated_at and not self.last_error else "degraded",
            "version": self.version,
            "generated_at": (datetime.fromtimestamp(self.generated_at, timezone.utc).isoformat()
                             if self.generated_at else None),
            "age_seconds": round(age) if age is not None else None,
            "runs": self.runs,
            "subscribers": len(self.subscribers),
            "last_error": self.last_error,
            "session": {
                "present": bool(tokens),
                "seconds_left": auth.seconds_left(tokens) if tokens else None,
                "has_refresh_token": bool(tokens and tokens.get("refresh_token")),
            },
        }


def _handler(state: State, *, allow_writes: bool, league_id: str | None,
             my_team_id: str | None):
    class Handler(BaseHTTPRequestHandler):
        server_version = "laliga-fantasy"
        protocol_version = "HTTP/1.1"

        # --- plumbing -------------------------------------------------------

        def _send(self, code: int, body: bytes, content_type: str) -> None:
            self.send_response(code)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            if self.command != "HEAD":
                self.wfile.write(body)

        def _json(self, code: int, payload: Any) -> None:
            self._send(code, json.dumps(payload, ensure_ascii=False, default=str).encode(),
                       "application/json; charset=utf-8")

        def _body(self) -> dict[str, Any]:
            length = int(self.headers.get("Content-Length") or 0)
            if not length:
                return {}
            try:
                return json.loads(self.rfile.read(length))
            except (ValueError, TypeError):
                return {}

        def log_message(self, fmt: str, *args: Any) -> None:
            log.debug("http request", extra={"client": self.address_string(),
                                             "request": fmt % args})

        # --- reads ----------------------------------------------------------

        def do_HEAD(self) -> None:
            self.do_GET()

        def do_GET(self) -> None:
            path = self.path.split("?", 1)[0].rstrip("/") or "/"
            if path in ("/", "/index.html"):
                with state.lock:
                    body = state.html.encode()
                self._send(200, body, "text/html; charset=utf-8")
            elif path == "/api/state":
                with state.lock:
                    payload = {**state.payload, "version": state.version,
                               "writes_enabled": allow_writes}
                self._json(200, payload)
            elif path == "/api/fragments":
                with state.lock:
                    payload = {"version": state.version, "sections": state.sections}
                self._json(200, payload)
            elif path == "/api/lineup":
                self._lineup()
            elif path.startswith("/api/player/"):
                self._player_detail(path.rsplit("/", 1)[-1])
            elif path == "/api/events":
                self._stream()
            elif path in ("/healthz", "/api/health"):
                health = state.health()
                self._json(200 if health["status"] == "ok" else 503, health)
            elif path == "/refresh":
                self._json(200, {"refreshed": state.refresh(force=True),
                                 "version": state.version})
            else:
                self._send(404, b"not found", "text/plain; charset=utf-8")

        def _lineup(self) -> None:
            """The pitch: who starts where, who sits, and each one's recent form.

            Merged with the analysis so a shirt on the pitch carries the same numbers
            as its row in every table — xPts, value trend, odds of starting.
            """
            if not my_team_id:
                self._json(400, {"error": "sin equipo resuelto"})
                return
            try:
                payload = laliga.lineup(str(my_team_id))
                free = laliga.formations()
                premium = laliga.formations(premium=True)
            except Exception as exc:
                self._json(502, {"error": f"no he podido leer la alineacion: {exc}"})
                return

            with state.lock:
                players = {str(p["id"]): p for p in state.payload.get("players") or []}
            formation = payload.get("formation") or {}

            def shirt(slot: dict) -> dict[str, Any]:
                master = slot.get("playerMaster") or {}
                pid = str(master.get("id") or "")
                extra = players.get(pid) or {}
                weeks = [{"week": s.get("weekNumber"), "points": s.get("totalPoints")}
                         for s in (master.get("lastStats") or [])]
                return {
                    "player_team_id": str(slot.get("playerTeamId") or ""),
                    "id": pid,
                    "name": master.get("nickname") or master.get("name"),
                    "position_id": master.get("positionId"),
                    "team_id": str(master.get("teamId") or extra.get("team_id") or ""),
                    "value": master.get("marketValue") or extra.get("value"),
                    "status": master.get("playerStatus") or extra.get("status"),
                    "week_points": master.get("weekPoints"),
                    "average": master.get("averagePoints"),
                    "last_season_points": master.get("lastSeasonPoints"),
                    "weeks": weeks,
                    # from the analysis, so the pitch agrees with the tables
                    "xpts": extra.get("xpts"),
                    "projected_pct": extra.get("projected_pct"),
                    "start_probability": extra.get("start_probability"),
                    "next_rival": extra.get("next_rival"),
                    "next_home": extra.get("next_home"),
                    "starred": extra.get("starred"),
                    "absence": extra.get("absence"),
                }

            lines = {line: [shirt(slot) for slot in (formation.get(line) or [])]
                     for line in laliga.LINEUP_LINES}
            starters = {s["id"] for group in lines.values() for s in group}
            # The payload's bench comes back empty, so the reserves are simply the rest
            # of the squad, rebuilt into the same shirt shape as the starters.
            bench = []
            for player in players.values():
                if not player.get("is_mine") or str(player["id"]) in starters:
                    continue
                bench.append(shirt({
                    "playerTeamId": player.get("player_team_id") or "",
                    "playerMaster": {
                        "id": player["id"], "nickname": player["name"],
                        "positionId": player["position_id"], "teamId": player["team_id"],
                        "marketValue": player["value"], "playerStatus": player["status"],
                        "lastStats": [],
                    },
                }))

            self._json(200, {
                "lines": lines, "bench": bench,
                "formation": formation.get("tacticalFormation"),
                "formations": {"free": free, "premium": premium},
                "updated_at": payload.get("updatedAt"),
                "writes_enabled": allow_writes,
            })

        def _player_detail(self, player_id: str) -> None:
            """Everything about one player, plus what can be done with him right now.

            The actions are computed here rather than in the page because only the
            server knows the current market, offers and clause state — and because
            the page must never offer a button the API would refuse.
            """
            with state.lock:
                players = state.payload.get("players") or []
                policies_now = dict(state.payload.get("policies") or {})
                budget = state.payload.get("budget") or 0
            player = next((p for p in players if str(p.get("id")) == str(player_id)), None)
            if not player:
                # Coaches are in the game (positionId 5) and even appear in the market,
                # but they are excluded from the analysis, so say that rather than 404.
                self._json(404, {"error": "sin datos para este id: puede ser un entrenador, "
                                          "que el juego lista pero el analisis no cubre"})
                return

            listing = player.get("market") or {}
            offers = player.get("offers") or []
            clause = player.get("clause") or 0
            actions: list[dict[str, Any]] = []

            if player.get("is_mine"):
                starred = str(player_id) in policies_now
                actions.append({"op": "always", "label": ("Quitar de siempre-en-mercado"
                                                          if starred else "Siempre en mercado"),
                                "kind": "toggle", "on": starred})
                if listing.get("market_id"):
                    actions.append({"op": "withdraw", "label": "Quitar del mercado",
                                    "kind": "confirm", "market_id": listing["market_id"]})
                else:
                    actions.append({"op": "sell_to_market", "label": "Poner en venta",
                                    "kind": "amount", "suggested": int(player["value"] or 0),
                                    "player_id": player_id})
                for offer in offers:
                    actions.append({"op": "accept_offer",
                                    "label": f"Aceptar {int(offer['money']):,}".replace(",", "."),
                                    "kind": "confirm", "offer_id": str(offer["id"]),
                                    "market_id": listing.get("market_id"),
                                    "amount": int(offer["money"])})
                    actions.append({"op": "decline_offer", "label": "Rechazar",
                                    "kind": "confirm", "danger": True,
                                    "offer_id": str(offer["id"]),
                                    "market_id": listing.get("market_id")})
                actions.append({"op": "raise_clause", "label": "Subir clausula",
                                "kind": "amount", "player_id": player_id,
                                "suggested": int((player["value"] or 0) * 0.5)})
            elif listing.get("kind") == "libre":
                actions.append({"op": "bid", "label": "Pujar", "kind": "amount",
                                "market_id": listing.get("market_id"),
                                "suggested": int(player.get("ideal_bid")
                                                 or listing.get("min_bid") or 0),
                                "min": int(listing.get("min_bid") or 0),
                                "bids": listing.get("bids"),
                                "expires": listing.get("expires")})
            elif player.get("owner"):
                scheduled = (policies_now.get(str(player_id)) or {}).get("raid")
                if player.get("shielded"):
                    actions.append({"op": "note", "kind": "note",
                                    "label": f"{player['owner']} lo tiene blindado: "
                                             "no se puede clausular"})
                else:
                    actions.append({
                        "op": "raid", "kind": "amount",
                        "label": ("Cambiar clausulazo programado" if scheduled
                                  else "Programar clausulazo"),
                        "player_id": player_id, "on": bool(scheduled),
                        "suggested": int((policies_now.get(str(player_id)) or {}).get("max_pay")
                                         or (clause * 1.2 if clause else player["value"] * 1.5)),
                        "note": ("Se paga en cuanto se libere la clausula, y solo si sigue "
                                 "por debajo de este importe.")})
                actions.append({"op": "direct_offer",
                                "label": f"Ofrecer a {player['owner']}", "kind": "amount",
                                "player_id": player_id,
                                "suggested": int(player["value"] or 0)})
                if listing.get("market_id"):
                    actions.append({"op": "bid", "label": "Pujar por su venta",
                                    "kind": "amount", "market_id": listing["market_id"],
                                    "suggested": int(listing.get("min_bid") or 0),
                                    "min": int(listing.get("min_bid") or 0)})
                if clause and not player.get("clause_locked"):
                    actions.append({"op": "pay_clause",
                                    "label": f"Pagar clausula ({int(clause):,})".replace(",", "."),
                                    "kind": "amount", "player_id": player_id,
                                    "suggested": int(clause), "min": int(clause),
                                    "blocked": clause > budget})

            history = []
            try:
                history = [{"date": point.get("date", "")[:10],
                            "value": point.get("marketValue")}
                           for point in laliga.player_market_value(str(player_id))][-90:]
            except Exception:
                pass

            self._json(200, {"player": player, "offers": offers, "listing": listing,
                             "actions": actions, "history": history,
                             "writes_enabled": allow_writes})

        def _stream(self) -> None:
            """Server-sent events: one line per change, plus a heartbeat."""
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Connection", "keep-alive")
            self.end_headers()
            channel = state.subscribe()
            try:
                self.wfile.write(f"data: {json.dumps({'type': 'hello', 'version': state.version})}\n\n"
                                 .encode())
                self.wfile.flush()
                while True:
                    try:
                        message = channel.get(timeout=HEARTBEAT)
                    except queue.Empty:
                        message = {"type": "ping", "at": time.time()}
                    self.wfile.write(f"data: {json.dumps(message, default=str)}\n\n".encode())
                    self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError, OSError):
                pass
            finally:
                state.unsubscribe(channel)

        # --- writes ---------------------------------------------------------

        def do_POST(self) -> None:
            path = self.path.split("?", 1)[0].rstrip("/") or "/"
            body = self._body()
            if path in ("/favourite", "/api/favourite"):
                player_id = str(body.get("id") or "")
                if not player_id:
                    self._json(400, {"error": "falta el id"})
                    return
                starred = favourites.toggle(player_id, body.get("name"))
                log.info("favourite toggled", extra={"player_id": player_id,
                                                    "starred": starred})
                # Keep the in-memory universe in step so the repaint shows the change.
                with state.lock:
                    for row in (state.universe or {}).get("players") or []:
                        if str(row.get("id")) == player_id:
                            row["starred"] = starred
                threading.Thread(target=state.rerender, daemon=True).start()
                self._json(200, {"id": player_id, "starred": starred})
                return

            if path == "/api/always":
                player_id = str(body.get("id") or "")
                if not player_id:
                    self._json(400, {"error": "falta el id"})
                    return
                if policies.load().get(player_id):
                    policies.remove(player_id)
                    on = False
                else:
                    policies.set_policy(player_id, name=body.get("name"), always_listed=True)
                    on = True
                threading.Thread(target=state.rerender, daemon=True).start()
                self._json(200, {"id": player_id, "always_listed": on})
                return

            if path == "/api/lineup":
                try:
                    summary = writes.prepare(
                        "save_lineup", league_id=str(league_id), my_team_id=str(my_team_id),
                        player={"name": "tu alineacion"}, allow_writes=allow_writes)
                    pending = writes._pending[summary["token"]]
                    pending["args"] = {
                        "team_id": str(my_team_id),
                        "goalkeeper": body.get("goalkeeper"),
                        "defender": body.get("defender") or [],
                        "midfield": body.get("midfield") or [],
                        "striker": body.get("striker") or [],
                        "formation": body.get("formation") or [],
                    }
                    result = writes.confirm(summary["token"], allow_writes=allow_writes)
                except writes.WritesDisabled as exc:
                    self._json(403, {"error": str(exc)})
                except writes.WriteError as exc:
                    self._json(400, {"error": str(exc)})
                else:
                    threading.Thread(target=state.refresh, kwargs={"force": True},
                                     daemon=True).start()
                    self._json(200, {"ok": True, "saved": bool(result),
                                     "formation": body.get("formation")})
                return

            if path == "/api/raid":
                player_id = str(body.get("id") or "")
                max_pay = int(float(body.get("max_pay") or 0))
                if not player_id or max_pay <= 0:
                    self._json(400, {"error": "falta el id o el pago maximo"})
                    return
                entry = policies.set_policy(player_id, name=body.get("name"),
                                            raid=True, max_pay=max_pay)
                log.info("raid scheduled", extra={"player_id": player_id, "max_pay": max_pay})
                threading.Thread(target=state.rerender, daemon=True).start()
                self._json(200, entry)
                return

            if path == "/api/bid/prepare":
                self._prepare(body)
                return
            if path == "/api/bid/confirm":
                self._confirm(body)
                return
            self._send(404, b"not found", "text/plain; charset=utf-8")

        def _player_for(self, player_id: str) -> dict[str, Any]:
            with state.lock:
                players = state.payload.get("players") or []
            for player in players:
                if str(player.get("id")) == str(player_id):
                    listing = player.get("market") or {}
                    return {"name": player.get("name"), "value": player.get("value"),
                            "ideal_bid": player.get("ideal_bid"),
                            "min_bid": listing.get("min_bid"),
                            "clause": player.get("clause"),
                            "bids": listing.get("bids"),
                            "expires": listing.get("expires")}
            return {}

        def _prepare(self, body: dict[str, Any]) -> None:
            try:
                amount = int(float(body.get("amount") or 0))
                summary = writes.prepare(
                    body.get("operation") or "bid",
                    league_id=str(league_id), my_team_id=str(my_team_id),
                    amount=amount,
                    market_id=str(body.get("market_id") or "") or None,
                    bid_id=str(body.get("bid_id") or "") or None,
                    player=self._player_for(body.get("player_id") or ""),
                    allow_writes=allow_writes)
            except writes.WritesDisabled as exc:
                self._json(403, {"error": str(exc)})
            except writes.WriteError as exc:
                self._json(400, {"error": str(exc)})
            except (ValueError, TypeError):
                self._json(400, {"error": "importe no valido"})
            else:
                self._json(200, summary)

        def _confirm(self, body: dict[str, Any]) -> None:
            try:
                result = writes.confirm(str(body.get("token") or ""),
                                        allow_writes=allow_writes,
                                        dry_run=bool(body.get("dry_run")))
            except writes.WritesDisabled as exc:
                self._json(403, {"error": str(exc)})
            except writes.WriteError as exc:
                self._json(400, {"error": str(exc)})
            else:
                threading.Thread(target=state.refresh, kwargs={"force": True},
                                 daemon=True).start()
                self._json(200, result)

    return Handler


def run(builder: Callable[[], tuple[dict, dict | None, dict]], *,
        host: str = "0.0.0.0", port: int = 8000, interval: int = 120,
        allow_writes: bool = True, auto: bool = True, league_id: str | None = None,
        my_team_id: str | None = None) -> int:
    state = State(builder)
    # Scheduled instructions exist precisely to fire while nobody is watching, so
    # they run by default; --no-auto suspends them without going fully read-only.
    state.policy_context = {"league_id": league_id, "my_team_id": my_team_id,
                            "allow_writes": allow_writes and auto}
    state.refresh(force=True)

    def loop() -> None:
        while True:
            time.sleep(interval)
            state.refresh()

    threading.Thread(target=loop, daemon=True).start()
    server = ThreadingHTTPServer((host, port), _handler(
        state, allow_writes=allow_writes, league_id=league_id, my_team_id=my_team_id))
    server.daemon_threads = True
    log.info("serving", extra={"host": host, "port": port, "interval": interval,
                              "writes": allow_writes})
    print(f"Sirviendo en http://{host}:{port}")
    print(f"  refresco cada {interval}s · push por SSE · "
          f"{'operaciones activas' if allow_writes else 'solo lectura'}"
          f"{' · automatico' if (allow_writes and auto) else ''}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0
