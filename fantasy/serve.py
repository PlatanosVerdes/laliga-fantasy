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

from . import auth, favourites, policies, report, writes
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

        # Standing instructions run off the same data the page just rendered, so
        # what the robot sees is exactly what you see.
        context_for_policies = getattr(self, "policy_context", None)
        if context_for_policies and context_for_policies.get("league_id"):
            try:
                self.policy_actions = policies.enforce(
                    universe["players"],
                    league_id=context_for_policies["league_id"],
                    my_team_id=context_for_policies["my_team_id"],
                    allow_writes=context_for_policies["allow_writes"])
            except Exception as exc:
                log.error("policies failed", extra={"reason": str(exc)[:200]})

        with self.lock:
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
                threading.Thread(target=state.refresh, kwargs={"force": True},
                                 daemon=True).start()
                self._json(200, {"id": player_id, "starred": starred})
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
                            "min_bid": listing.get("min_bid")}
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
        allow_writes: bool = False, league_id: str | None = None,
        my_team_id: str | None = None) -> int:
    state = State(builder)
    state.policy_context = {"league_id": league_id, "my_team_id": my_team_id,
                            "allow_writes": allow_writes}
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
    print(f"  refresco cada {interval}s · push por SSE · escritura "
          f"{'ACTIVADA' if allow_writes else 'desactivada'}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0
