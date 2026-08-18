"""When to wake up, and whether anything actually moved.

A fixed interval is the wrong shape for this game. Nothing happens for hours, and
then several things happen at an exact second: a clause unlocks, an auction closes,
an offer expires. Polling often enough to catch the second wastes the hours, and
polling calmly enough for the hours arrives late to the second.

So the loop asks two questions instead of one. *Has anything moved?* is answered by
the activity log and the market listing — two requests — and if the answer is no,
the expensive rebuild is skipped. *When does something next matter?* is answered
here, from deadlines already present in the data, so the loop sleeps until then
instead of counting to 120 with its eyes shut.
"""
from __future__ import annotations

import hashlib
import json
from datetime import datetime, timezone
from typing import Any

# Wake this much before a deadline: enough for one request to land on time, not so
# much that we act on a clause which is still locked.
LEAD = 2.0
MIN_SLEEP = 3.0
# Even with nothing moving and no deadline in sight, rebuild this often: values,
# points and futbolfantasy all drift without generating an event.
CEILING = 900.0
# How close a deadline has to be for the league to count as busy. Inside this
# window the probe runs at the base tick; outside it, it backs off — but never while
# somebody has the page open, because for them the page is supposed to be live.
BUSY_WINDOW = 600.0
IDLE_FACTOR = 4
# How long a match is worth treating as under way. Kick-off plus stoppage, halftime and
# the minutes it takes for the last points to settle.
MATCH_WINDOW = 130 * 60
# While the ball is rolling, points move and nothing announces it. Two minutes rather
# than one because a live rebuild is not cheap — the player master has to be re-read —
# and points do not change meaningfully faster than that.
LIVE_TICK = 120.0

PROBE, REBUILD, DEADLINE = "probe", "rebuild", "deadline"
# matchState 7 is a finished match; see analysis.load_fixtures.
FINISHED = 7


def live_matches(payload: dict[str, Any], *, now: float,
                 mine_only: bool = False) -> list[dict[str, Any]]:
    """Matches whose ball is presumably rolling, judged by the clock.

    The API's code for a live match is not known — 1 is pending and 7 is finished, and
    that is all that has been observed — so this asks whether we are inside the window
    that starts at kick-off, and trusts the finished state when it is set. Over-polling
    a postponed match costs a request; missing a live one costs the points.

    `mine_only` keeps the matches our own players are in. Somebody else's fixture moves
    the standings, ours moves our score, and only the second deserves the fast cadence.
    """
    teams = set()
    if mine_only:
        teams = {str(p.get("team_id")) for p in (payload.get("players") or [])
                 if p.get("is_mine") and p.get("team_id")}
    live = []
    for fixture in payload.get("fixtures") or []:
        kickoff = _parse(fixture.get("kickoff"))
        if not kickoff or fixture.get("state") == FINISHED:
            continue
        if not kickoff <= now <= kickoff + MATCH_WINDOW:
            continue
        if mine_only and not ({str(fixture.get("local_id")),
                               str(fixture.get("visitor_id"))} & teams):
            continue
        live.append(fixture)
    return live


def _parse(value: Any) -> float | None:
    """Epoch seconds from whatever shape the API used for this particular date."""
    if value in (None, ""):
        return None
    if isinstance(value, (int, float)):
        return float(value) / 1000 if value > 1e11 else float(value)   # ms or s
    text = str(value).strip().replace("Z", "+00:00")
    try:
        stamp = datetime.fromisoformat(text)
    except ValueError:
        return None
    if stamp.tzinfo is None:
        stamp = stamp.replace(tzinfo=timezone.utc)
    return stamp.timestamp()


def deadlines(payload: dict[str, Any], *, now: float, after: float = 0.0,
              policies: dict[str, Any] | None = None) -> list[tuple[float, str]]:
    """Every future instant that changes what we would do, soonest first.

    Only moments that need an action or a fresh read belong here. A clause that
    unlocks is a deadline when a raid is scheduled on it; otherwise it is a date on
    the calendar, and a calendar can be redrawn at leisure.

    `after` discards deadlines already acted on. Without it, an expiry the API keeps
    reporting a second into the future would be woken for again and again, and a
    tight loop of rebuilds is a worse failure than a late one.
    """
    policies = policies if policies is not None else (payload.get("policies") or {})
    players = payload.get("players") or []
    names = {str(p.get("id")): p.get("name") for p in players}
    found: list[tuple[float, str]] = []

    for listing in payload.get("market") or []:
        when = _parse(listing.get("expires"))
        if when and when > now:
            who = names.get(str(listing.get("player_id"))) or "un jugador"
            found.append((when, f"cierra la subasta de {who}"))

    for fixture in payload.get("fixtures") or []:
        kickoff = _parse(fixture.get("kickoff"))
        if not kickoff:
            continue
        pair = f"{fixture.get('local')} - {fixture.get('visitor')}"
        if kickoff > now:
            found.append((kickoff, f"empieza {pair}"))
        elif fixture.get("state") != FINISHED and kickoff + MATCH_WINDOW > now:
            # The final points land after the whistle, not on it.
            found.append((kickoff + MATCH_WINDOW, f"termina {pair}"))

    closing = _parse((payload.get("week") or {}).get("closingWeekDate"))
    if closing and closing > now:
        found.append((closing, f"cierra la jornada {(payload.get('week') or {}).get('weekNumber')}"))

    for player in players:
        policy = policies.get(str(player.get("id"))) or {}
        if policy.get("raid"):
            when = _parse(player.get("clause_locked_until"))
            if when and when > now:
                found.append((when, f"se libera la clausula de {player.get('name')}"))
        for offer in player.get("offers") or []:
            when = _parse(offer.get("expirationDate") or offer.get("expires"))
            if when and when > now:
                found.append((when, f"caduca una oferta por {player.get('name')}"))

    return sorted(entry for entry in found if entry[0] > after)


def next_wake(payload: dict[str, Any], *, now: float, tick: float,
              last_full: float | None = None, watched: bool = False,
              after: float = 0.0,
              policies: dict[str, Any] | None = None) -> tuple[float, str, str]:
    """(when, why, kind). The why is logged and shown, so it has to read plainly."""
    upcoming = deadlines(payload, now=now, after=after, policies=policies)
    busy = watched or bool(upcoming and upcoming[0][0] - now <= BUSY_WINDOW)

    if live_matches(payload, now=now, mine_only=True):
        # Our own players on the pitch: points move and no endpoint announces it.
        tick, busy = min(tick, LIVE_TICK), True
    elif live_matches(payload, now=now):
        busy = True     # somebody else's match: the standings move, our score does not

    when = now + (tick if busy else tick * IDLE_FACTOR)
    why, kind = "a ver si se ha movido algo", PROBE

    rebuild_at = (last_full if last_full is not None else now) + CEILING
    if rebuild_at < when:
        when, why, kind = rebuild_at, "reconstruccion completa periodica", REBUILD

    if upcoming and upcoming[0][0] - LEAD < when:
        when, why, kind = upcoming[0][0] - LEAD, upcoming[0][1], DEADLINE

    return max(when, now + MIN_SLEEP), why, kind


def _amount(value: Any) -> str:
    """Same number, same string, whichever side of the parser it came from."""
    try:
        return str(int(float(value)))
    except (TypeError, ValueError):
        return ""


def probe_parts(activity: list[dict[str, Any]],
                market: list[dict[str, Any]]) -> dict[str, str]:
    """The probe split in two, because the two halves mean different things.

    A new activity event means somebody bought, sold or was transferred, so every
    squad we hold is stale however recently it was read. A change in the market half
    alone is a new listing or a rival bid: nothing has changed hands.
    """
    events = []
    for event in (activity or [])[:8]:
        source = (event or {}).get("raw") or event or {}
        events.append(str(source.get("id")))
    listings = sorted(
        f"{entry.get('id') or entry.get('market_id')}:"
        f"{_amount(entry.get('salePrice') if entry.get('salePrice') is not None else entry.get('min_bid'))}:"
        f"{entry.get('numberOfBids') if entry.get('numberOfBids') is not None else entry.get('bids')}:"
        f"{entry.get('numberOfOffers') if entry.get('numberOfOffers') is not None else entry.get('offers')}"
        for entry in (market or []))
    return {"events": hashlib.sha1(json.dumps(events).encode()).hexdigest(),
            "market": hashlib.sha1(json.dumps(listings).encode()).hexdigest()}
