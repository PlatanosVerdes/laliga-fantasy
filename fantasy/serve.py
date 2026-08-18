"""Live server: JSON API, server-rendered fragments and a push channel.

The page is not regenerated on a schedule the browser knows nothing about, and the
server does not poll blindly either: it sleeps until the next instant that matters
and, when it wakes, first asks the two cheap questions that tell it whether a
rebuild is warranted at all (see `schedule.py`). When the data changes, the version
number moves and every connected browser is told over SSE, so it can swap just the
sections that changed.

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

from . import auth, favourites, http, laliga, policies, report, schedule, writes
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
        self.probe_parts: dict[str, str] = {}
        self.probes = 0
        self.deadline_floor = 0.0
        self.last_effect: dict[str, Any] | None = None
        self.needs_followup = False
        # Set to cut a sleep short. Opening the page has to tighten the cadence now,
        # not at the end of a wait that was planned while nobody was looking.
        self.nudge = threading.Event()
        self.last_full: float | None = None
        self.next_wake: float | None = None
        self.wake_reason = ""
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
                    # An instruction that fired changed the world too, so the page
                    # about to be built is already out of date.
                    self.needs_followup = any(
                        action.get("result") == "hecho" for action in self.policy_actions)
                except Exception as exc:
                    log.error("policies failed", extra={"reason": str(exc)[:200]})

            fingerprint = self._fingerprint(universe, advice)
            changed = force or fingerprint != self.fingerprint
            page = report.build(universe, advice, context=context,
                                activity=universe.get("activity"))
            payload = {
                "generated_at": datetime.now(timezone.utc).isoformat(),
                "week": universe["week"],
                "fixtures": universe.get("fixtures"),
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
            self.last_full = self.generated_at
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

    def probe(self, league_id: str) -> tuple[bool, list[str]]:
        """Two requests that answer whether a rebuild is worth it, and which half moved.

        Both are stored in the cache, so when the answer is yes the rebuild reuses
        these very responses instead of asking again. A change in the activity half
        also drops the squad and cash caches: somebody changing hands makes every
        squad wrong however recently it was read, which is the one case a long TTL
        gets badly wrong.
        """
        events = laliga.activity(league_id, ttl=0, store=True)
        listings = laliga.market(league_id, ttl=0, store=True)
        parts = schedule.probe_parts(events, listings)
        moved = [half for half, digest in parts.items()
                 if digest != self.probe_parts.get(half)]
        self.probe_parts = parts
        self.probes += 1
        if "events" in moved:
            http.invalidate("squad", "money", "standing")
        return bool(moved), moved

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

    def snapshot(self) -> dict[str, Any]:
        """The handful of figures an operation moves, for a before/after comparison.

        Read from the payload already in memory, so taking one costs nothing.
        """
        players = self.payload.get("players") or []
        mine = [p for p in players if p.get("is_mine")]
        return {
            "cash": (self.payload.get("budget") or 0),
            "squad": len(mine),
            "squad_value": round(sum(float(p.get("value") or 0) for p in mine)),
            "listed": sum(1 for m in (self.payload.get("market") or []) if m.get("is_mine")),
            "offers": sum(len(p.get("offers") or []) for p in mine),
            # A match changes neither the transfer log nor the market, but it changes
            # these two, which is the whole reason they are in here.
            "points": round(sum(float(p.get("season_points") or 0) for p in mine)),
            "absences": sum(1 for p in mine if p.get("absence")),
        }

    @staticmethod
    def difference(before: dict[str, Any], after: dict[str, Any]) -> dict[str, Any]:
        """Only what actually moved, so an empty result means the write changed nothing."""
        return {key: {"before": before.get(key), "after": after.get(key),
                      "delta": (after.get(key) or 0) - (before.get(key) or 0)}
                for key in before if before.get(key) != after.get(key)}

    # --- push ---------------------------------------------------------------

    def subscribe(self) -> queue.Queue:
        channel: queue.Queue = queue.Queue(maxsize=8)
        with self.lock:
            first = not self.subscribers
            self.subscribers.add(channel)
        if first:
            self.nudge.set()
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
            "probes": self.probes,
            "requests": dict(http.STATS),
            "next_wake_in": (round(self.next_wake - time.time())
                             if self.next_wake else None),
            "next_wake_why": self.wake_reason or None,
            "subscribers": len(self.subscribers),
            "last_error": self.last_error,
            "last_effect": self.last_effect,
            "session": {
                "present": bool(tokens),
                "seconds_left": auth.seconds_left(tokens) if tokens else None,
                "has_refresh_token": bool(tokens and tokens.get("refresh_token")),
            },
        }


# Figures that only move when something actually happened. Squad value drifts on its
# own every time the market revalues a player, so it is worth reporting after an
# operation but would cry wolf on an ordinary refresh.
DISCRETE = ("cash", "squad", "listed", "offers", "points", "absences")


def settle(state: State, cause: str, *, force: bool = True,
           keys: tuple[str, ...] | None = None) -> dict[str, Any]:
    """Rebuild, then report what moved between before and after.

    Used for two different things, which turn out to be the same thing. A write of
    ours is not a local edit — the cash, the squad, the market and every
    recommendation derived from them change, and `writes.confirm` has already dropped
    the caches it falsified. And a sale we did not make is exactly as consequential:
    when a rival buys the player we had listed, nothing of ours was clicked, so
    bracketing every rebuild is what makes that moment visible too.

    Runs off the request thread: a click should not wait for a dozen requests, and the
    answer arrives over SSE when it is ready.
    """
    before = state.snapshot()
    state.refresh(force=force)
    after = state.snapshot()
    diff = state.difference(before, after)
    if keys:
        diff = {key: value for key, value in diff.items() if key in keys}
    effect = {"operation": cause, "at": datetime.now(timezone.utc).isoformat(),
              "changed": diff}
    if diff:
        state.last_effect = effect
        log.info("world moved", extra={"cause": cause,
                                      "cash_before": before["cash"],
                                      "cash_after": after["cash"],
                                      "changed": list(diff)})
        state.publish({"type": "effect", "version": state.version, **effect})
    return effect


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
                policy_now = policies_now.get(str(player_id)) or {}
                starred = str(player_id) in policies_now
                actions.append({"op": "always", "label": ("Quitar de siempre-en-mercado"
                                                          if starred else "Siempre en mercado"),
                                "kind": "toggle", "on": starred,
                                "min_price": policy_now.get("min_price"),
                                "accept_above": policy_now.get("accept_above"),
                                "value": int(player.get("value") or 0)})
                if listing.get("market_id"):
                    actions.append({"op": "withdraw", "label": "Quitar del mercado",
                                    "kind": "confirm", "market_id": listing["market_id"]})
                else:
                    actions.append({"op": "sell_to_market", "label": "Poner en venta",
                                    "kind": "amount", "suggested": int(player["value"] or 0),
                                    "player_team_id": player.get("player_team_id")})
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
                                "kind": "amount",
                                "player_team_id": player.get("player_team_id"),
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
                # Una oferta directa va contra su anuncio: sin anuncio no hay a que ofertar.
                if listing.get("market_id"):
                    actions.append({"op": "direct_offer",
                                    "label": f"Ofrecer a {player['owner']}", "kind": "amount",
                                    "market_id": listing["market_id"],
                                    "suggested": int(listing.get("min_bid")
                                                     or player["value"] or 0)})
                else:
                    actions.append({"op": "note", "kind": "note",
                                    "label": f"{player['owner']} no lo tiene en venta: solo "
                                             "se le puede pagar la clausula"})
                if listing.get("market_id"):
                    actions.append({"op": "bid", "label": "Pujar por su venta",
                                    "kind": "amount", "market_id": listing["market_id"],
                                    "suggested": int(listing.get("min_bid") or 0),
                                    "min": int(listing.get("min_bid") or 0)})
                if clause and not player.get("clause_locked"):
                    actions.append({"op": "pay_clause",
                                    "label": f"Pagar clausula ({int(clause):,})".replace(",", "."),
                                    "kind": "amount",
                                    "player_team_id": player.get("player_team_id"),
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
                # Amounts present: set them. Otherwise it is the plain on/off toggle.
                if "min_price" in body or "accept_above" in body:
                    amounts, unset = {}, []
                    for key in ("min_price", "accept_above"):
                        if key not in body:
                            continue
                        raw = body.get(key)
                        try:
                            amount = int(float(raw)) if raw not in (None, "") else 0
                        except (TypeError, ValueError):
                            self._json(400, {"error": f"{key} no es un importe"})
                            return
                        if amount > 0:
                            amounts[key] = amount
                        else:
                            unset.append(key)
                    entry = policies.set_policy(player_id, name=body.get("name"),
                                                always_listed=True, unset=tuple(unset),
                                                **amounts)
                    threading.Thread(target=state.rerender, daemon=True).start()
                    self._json(200, {"id": player_id, "always_listed": True,
                                     "min_price": entry.get("min_price"),
                                     "accept_above": entry.get("accept_above")})
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
                    threading.Thread(target=settle, args=(state, "save_lineup"),
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
                    offer_id=str(body.get("offer_id") or "") or None,
                    player_id=str(body.get("player_id") or "") or None,
                    player_team_id=str(body.get("player_team_id") or "") or None,
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
                threading.Thread(target=settle,
                                 args=(state, result.get("operation") or "write"),
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
    # Seed the probe from what the first rebuild already read, so the loop does not
    # begin by asking the API to confirm data it is holding.
    if state.universe:
        state.probe_parts = schedule.probe_parts(
            state.universe.get("activity") or [], state.universe.get("market") or [])

    def loop() -> None:
        """Sleep until the next thing that matters, then do the least that suffices.

        Three kinds of wake-up. A deadline (an auction closing, a scheduled clause
        opening) rebuilds unconditionally, because that is the instant we may have to
        act. The periodic rebuild catches the drift nothing announces: values, points,
        futbolfantasy. Everything else is a probe, and a probe that finds the league
        exactly as it left it costs two requests and stops there.
        """
        while True:
            when, why, kind = schedule.next_wake(
                state.payload, now=time.time(), tick=interval,
                last_full=state.last_full, watched=bool(state.subscribers),
                after=state.deadline_floor, policies=policies.load())
            state.next_wake, state.wake_reason = when, why
            delay = max(schedule.MIN_SLEEP, when - time.time())
            log.debug("sleeping", extra={"seconds": round(delay, 1), "why": why,
                                        "kind": kind})
            state.nudge.clear()
            if state.nudge.wait(delay):
                # Somebody opened the page: replan instead of finishing a sleep that
                # was calculated for an empty room.
                log.debug("sleep cut short", extra={"had_left": round(when - time.time(), 1)})
                continue

            live = schedule.live_matches(state.payload, now=time.time())
            if live or why.startswith(("termina", "cierra")):
                # Points come from the player master, cached for six hours because it
                # barely changes — except while a match is on, when it is the only
                # thing that changes. A match is the one case where the long TTLs have
                # to be pushed aside, and the final points land after the whistle.
                http.invalidate("players", "lineup", "week")

            if kind == schedule.DEADLINE:
                # Remember it as spent, so an expiry the API keeps reporting as
                # imminent cannot be woken for twice.
                state.deadline_floor = when + schedule.LEAD + 1
                log.info("waking on a deadline", extra={"why": why})
                settle(state, "partido" if live else "vencimiento",
                       force=False, keys=DISCRETE)
                if state.needs_followup:
                    state.needs_followup = False
                    settle(state, "policy")
                continue
            if not league_id:
                log.debug("no league to probe, rebuilding", extra={"why": why})
                settle(state, "refresco", force=False, keys=DISCRETE)
                continue
            try:
                moved, halves = state.probe(league_id)
            except Exception as exc:
                # A failed probe must not silence the loop: fall back to rebuilding,
                # which has its own error handling and keeps the last good page up.
                log.warning("probe failed", extra={"error_type": type(exc).__name__,
                                                  "reason": str(exc)[:200]})
                settle(state, "refresco", force=False, keys=DISCRETE)
                continue
            if moved or live or kind == schedule.REBUILD:
                log.info("rebuilding", extra={"why": ("se movio: " + ", ".join(halves))
                                                     if moved else
                                                     (f"{len(live)} partido(s) en juego"
                                                      if live else why)})
                # A transfer in the log is somebody else acting on our squad — a sale
                # we did not make lands here, not in a write path.
                cause = ("partido" if live else
                         "traspaso" if "events" in halves else "mercado")
                settle(state, cause, force=False, keys=DISCRETE)
            else:
                log.debug("nothing moved", extra={"probes": state.probes})

            if state.needs_followup:
                # An instruction fired during that rebuild, so the page it produced
                # describes the world from before its own action.
                state.needs_followup = False
                settle(state, "policy")

    threading.Thread(target=loop, daemon=True).start()
    server = ThreadingHTTPServer((host, port), _handler(
        state, allow_writes=allow_writes, league_id=league_id, my_team_id=my_team_id))
    server.daemon_threads = True
    log.info("serving", extra={"host": host, "port": port, "interval": interval,
                              "ceiling": schedule.CEILING, "writes": allow_writes})
    print(f"Sirviendo en http://{host}:{port}")
    print(f"  sondeo cada {interval}s (x{schedule.IDLE_FACTOR} en calma), "
          f"reconstruccion completa cada {int(schedule.CEILING / 60)} min o "
          f"al vencer algo · push por SSE · "
          f"{'operaciones activas' if allow_writes else 'solo lectura'}"
          f"{' · automatico' if (allow_writes and auto) else ''}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0
