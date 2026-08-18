"""Scoring model and recommendations.

Everything is built on top of a "universe": one row per LaLiga Fantasy player,
merging the official API (price, points, status, ownership, clause) with
futbolfantasy (value momentum, odds of starting, next fixture, ideal bid).
"""
from __future__ import annotations

import math
import statistics
from datetime import datetime, timezone
from typing import Any, Iterable

from . import favourites
from . import futbolfantasy as ff
from . import http, laliga, matching
from .config import IDEAL_PER_POSITION, MIN_PER_POSITION, POSITIONS, WEEKS_IN_SEASON
from .logs import log, timed

# Points-per-week already averages over the weeks a player missed, so the odds of
# starting are applied as a deviation from a typical starter, not as a raw factor.
BASELINE_START_PROBABILITY = 0.85
AVAILABILITY_RANGE = (0.20, 1.10)
NO_TREND_CONFIDENCE = 0.75
SEVERE_STATUS = {"injured", "sanctioned", "suspended", "out_of_league"}

SCORE_WEIGHTS = {
    "points_value": 0.30,
    "xpts": 0.35,
    "projected_pct": 0.20,
    "start_probability": 0.15,
}


# --- helpers ----------------------------------------------------------------

def _zscores(values: list[float], *, winsorize: float = 0.0) -> list[float]:
    """Standard scores. `winsorize` clips the top/bottom quantile first so a
    handful of near-free players cannot flatten the rest of the distribution."""
    clean = sorted(v for v in values if v is not None and math.isfinite(v))
    if len(clean) < 2:
        return [0.0] * len(values)
    low, high = clean[0], clean[-1]
    if winsorize > 0:
        index = max(0, min(len(clean) - 1, int(len(clean) * winsorize)))
        low, high = clean[index], clean[len(clean) - 1 - index]
    clipped = [min(max(v, low), high) if (v is not None and math.isfinite(v)) else None
               for v in values]
    present = [v for v in clipped if v is not None]
    mean = statistics.fmean(present)
    stdev = statistics.pstdev(present) or 1.0
    return [((v - mean) / stdev) if v is not None else 0.0 for v in clipped]


def _rank_percentiles(values: dict[str, float]) -> dict[str, float]:
    """0..1 by rank, so three promoted clubs at zero don't squash the scale."""
    if not values:
        return {}
    order = sorted(values.items(), key=lambda item: item[1])
    span = max(1, len(order) - 1)
    return {key: index / span for index, (key, _) in enumerate(order)}


def _parse_iso(value: str | None) -> datetime | None:
    if not value:
        return None
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None


def money_from_payload(payload: Any) -> int | None:
    """The /money endpoint shape has changed across seasons; probe common keys."""
    if payload is None:
        return None
    if isinstance(payload, (int, float)):
        return int(payload)
    if isinstance(payload, dict):
        for key in ("teamMoney", "money", "balance", "cash", "budget", "availableMoney"):
            value = payload.get(key)
            if isinstance(value, (int, float)):
                return int(value)
        for value in payload.values():
            found = money_from_payload(value) if isinstance(value, dict) else None
            if found is not None:
                return found
    return None


def max_debt_from_payload(payload: Any) -> int | None:
    if isinstance(payload, dict):
        for key in ("maximumDebt", "maxDebt", "maximumAllowedDebt", "debtLimit"):
            value = payload.get(key)
            if isinstance(value, (int, float)):
                return int(value)
    return None


# --- team strength ----------------------------------------------------------

def team_strength(players: Iterable[dict]) -> dict[str, float]:
    """0..1 strength proxy per LaLiga team.

    Squad market value is the only signal that covers promoted clubs: their
    players carry zero last-season fantasy points because those points only exist
    for LaLiga, so a points-based proxy would rate Deportivo at exactly zero.
    """
    points: dict[str, list[float]] = {}
    values: dict[str, list[float]] = {}
    for player in players:
        if int(player.get("positionId") or 0) == 5:
            continue
        team_id = str(player.get("teamId"))
        points.setdefault(team_id, []).append(float(player.get("lastSeasonPoints") or 0))
        values.setdefault(team_id, []).append(float(player.get("marketValue") or 0))

    top = lambda items: sum(sorted(items, reverse=True)[:14])
    points_pct = _rank_percentiles({tid: top(v) for tid, v in points.items()})
    value_pct = _rank_percentiles({tid: top(v) for tid, v in values.items()})
    return {tid: 0.45 * points_pct.get(tid, 0.5) + 0.55 * value_pct.get(tid, 0.5)
            for tid in values}


def fixture_factor(opponent_strength: float | None, home: bool | None) -> float:
    factor = 1.0
    if opponent_strength is not None:
        factor *= 1.0 + 0.12 * (1.0 - 2.0 * opponent_strength)
    if home is not None:
        factor *= 1.04 if home else 0.96
    return factor


def price_prior(players: Iterable[dict], *, buckets: int = 8) -> dict[int, list[tuple[float, float]]]:
    """Points-per-week implied by price, per position, as a log-price curve.

    Built only from players who do have last-season points, then used as the
    baseline for anyone who has none: promoted clubs (their points came from a
    competition this feed does not count) and the star records the game recreated
    this season, which arrive with no history at all (Mbappé, Lamine, Vini...).
    LaLiga prices by expectation, so price is the best prior before kick-off.

    Returns [(log10(value), points_per_week)] sorted by price.
    """
    samples: dict[int, list[tuple[float, float]]] = {}
    for player in players:
        position_id = int(player.get("positionId") or 0)
        if position_id == 5:
            continue
        last_season = float(player.get("lastSeasonPoints") or 0)
        value = float(player.get("marketValue") or 0)
        if last_season <= 0 or value <= 0:
            continue
        samples.setdefault(position_id, []).append((math.log10(value), last_season / WEEKS_IN_SEASON))

    curves: dict[int, list[tuple[float, float]]] = {}
    for position_id, rows in samples.items():
        rows.sort()
        size = max(1, len(rows) // buckets)
        curve = []
        for start in range(0, len(rows), size):
            chunk = rows[start:start + size]
            if len(chunk) < 3 and curve:
                continue
            curve.append((statistics.fmean(x for x, _ in chunk),
                          statistics.median(y for _, y in chunk)))
        curves[position_id] = curve
    return curves


def prior_for(curves: dict[int, list[tuple[float, float]]], position_id: int,
              value: float) -> float:
    """Interpolate the curve, extrapolating past its ends with the edge slope."""
    curve = curves.get(position_id) or []
    if not curve or value <= 0:
        return 0.0
    if len(curve) == 1:
        return curve[0][1]

    x = math.log10(value)
    if x <= curve[0][0]:
        left, right = curve[0], curve[1]
    elif x >= curve[-1][0]:
        left, right = curve[-2], curve[-1]
    else:
        left, right = next((curve[i], curve[i + 1]) for i in range(len(curve) - 1)
                           if curve[i][0] <= x <= curve[i + 1][0])
    span = (right[0] - left[0]) or 1.0
    slope = (right[1] - left[1]) / span
    return max(0.0, left[1] + slope * (x - left[0]))


# --- universe ---------------------------------------------------------------

def build_universe(
    *,
    league_id: str | None = None,
    my_team_id: str | None = None,
    include_coaches: bool = False,
    ff_ttl: float = 2 * 3600,
) -> dict[str, Any]:
    with timed("fetch base data"):
        players = laliga.all_players()
        teams = laliga.teams_master()
        week = laliga.current_week()
        market_rows = ff.market(ttl=ff_ttl)

    # Why a player is out, in words: the API only gives the status code. futbolfantasy
    # writes full names ("Dani Vivian") where LaLiga writes nicknames ("Vivian"), so
    # index by both the whole name and the surname.
    absences: dict[str, dict[str, Any]] = {}
    try:
        for row in ff.absences():
            key = matching.normalize(row["name"])
            absences[key] = row
            absences.setdefault(matching.surname(row["name"]), row)
            if row.get("slug"):
                absences.setdefault(row["slug"].replace("-", " "), row)
    except Exception as exc:
        log.debug("absences unavailable", extra={"error_type": type(exc).__name__})

    def absence_for(player: dict) -> dict[str, Any] | None:
        for candidate in (player.get("name"), matching.player_label(player)):
            if not candidate:
                continue
            found = (absences.get(matching.normalize(candidate))
                     or absences.get(matching.surname(candidate)))
            if found:
                return found
        return None

    team_index = matching.build_team_index(teams)
    team_names = {str(t["id"]): t.get("name") for t in teams}
    team_short = {str(t["id"]): t.get("shortName") for t in teams}
    with timed("match sources", laliga_players=len(players), ff_rows=len(market_rows)):
        matched, unmatched = matching.match_market(players, market_rows, team_index)
    log.info("match rate", extra={"matched": len(matched), "ff_unmatched": len(unmatched),
                                 "coverage": round(len(matched) / max(1, len(market_rows)), 3)})
    strength = team_strength(players)
    curves = price_prior(players)

    fixtures = load_fixtures(week, teams)

    next_week_opens = None
    if week.get("nextWeek"):
        try:
            next_fixtures = laliga.calendar(int(week["nextWeek"]))
            dates = sorted(f.get("date") for f in next_fixtures if f.get("date"))
            next_week_opens = dates[0] if dates else None
        except Exception as exc:
            log.debug("next week calendar unavailable",
                      extra={"error_type": type(exc).__name__})

    completed_weeks = max(0, int(week.get("weekNumber") or 1) - 1)
    current_weight = min(1.0, completed_weeks / 8.0)

    ownership: dict[str, dict[str, Any]] = {}
    league_teams: dict[str, dict[str, Any]] = {}
    activity: list[dict[str, Any]] = []
    market_rows_league: list[dict[str, Any]] = []
    if league_id:
        with timed("load league ownership", league_id=league_id):
            ownership, league_teams = _load_ownership(league_id)
        with timed("load league activity", league_id=league_id):
            activity = load_activity(
                league_id,
                managers={t["user_id"]: t.get("manager") for t in league_teams.values()
                          if t.get("user_id")},
                player_names={str(p["id"]): (p.get("nickname") or p.get("name"))
                              for p in players})
        with timed("load market", league_id=league_id):
            market_rows_league = load_market(league_id, my_team_id)
        with timed("load offers", league_id=league_id):
            offers_by_player = load_offers(
                league_id, [m for m in market_rows_league if m["is_mine"]], ownership)
        enrich_activity_values(activity)
    market_by_player = {m["player_id"]: m for m in market_rows_league if m["player_id"]}
    offers_by_player = locals().get("offers_by_player") or {}
    starred = favourites.ids()

    rows: list[dict[str, Any]] = []
    for player in players:
        position_id = int(player.get("positionId") or 0)
        if position_id == 5 and not include_coaches:
            continue

        pid = str(player["id"])
        trend = matched.get(pid)
        value = float(player.get("marketValue") or 0)
        status = (player.get("playerStatus") or "ok").lower()
        last_season = float(player.get("lastSeasonPoints") or 0)
        season_points = float(player.get("points") or 0)

        prior_based = last_season <= 0
        per_week_last = (prior_for(curves, position_id, value) if prior_based
                         else last_season / WEEKS_IN_SEASON)
        per_week_now = (season_points / completed_weeks) if completed_weeks else 0.0
        base_week = current_weight * per_week_now + (1 - current_weight) * per_week_last

        probability = trend.get("start_probability") if trend else None
        if probability is not None:
            availability = (probability / 100.0) / BASELINE_START_PROBABILITY
            availability = max(AVAILABILITY_RANGE[0], min(AVAILABILITY_RANGE[1], availability))
        else:
            availability = 1.0

        rival_team_id = None
        if trend and trend.get("next_rival"):
            rival = team_index.get(matching.normalize_team(trend["next_rival"]))
            rival_team_id = str(rival["id"]) if rival else None
        difficulty = fixture_factor(strength.get(rival_team_id), trend.get("next_home") if trend else None)

        # Unknown minutes and a price-derived baseline are both guesses; discount them
        # so a fringe player with no data cannot outrank a known starter.
        confidence = 1.0
        if probability is None:
            confidence *= NO_TREND_CONFIDENCE
        if prior_based:
            confidence *= 0.9

        unavailable = status in SEVERE_STATUS
        xpts = 0.0 if unavailable else base_week * availability * difficulty * confidence
        if status == "doubtful":
            xpts *= 0.55

        projected_pct = _projected_pct(trend)
        owner = ownership.get(pid)

        rows.append({
            "id": pid,
            "name": matching.player_label(player),
            "full_name": player.get("name"),
            "position_id": position_id,
            "position": POSITIONS.get(position_id, "?"),
            "team_id": str(player.get("teamId")),
            "team": team_names.get(str(player.get("teamId"))),
            "team_short": team_short.get(str(player.get("teamId"))),
            "value": value,
            "status": status,
            "available": not unavailable,
            "last_season_points": last_season,
            "season_points": season_points,
            "season_avg": float(player.get("averagePoints") or 0),
            "base_week": base_week,
            "prior_based": prior_based,
            "confidence": confidence,
            "start_probability": probability,
            "next_week": trend.get("next_week") if trend else None,
            "next_rival": trend.get("next_rival") if trend else None,
            "next_home": trend.get("next_home") if trend else None,
            "fixture_factor": difficulty,
            "xpts": xpts,
            "points_value": (xpts / (value / 1e6)) if value else 0.0,
            "projected_pct": projected_pct,
            "projected_gain": value * projected_pct / 100.0,
            "trend_label": trend.get("trend_label") if trend else None,
            "streak_days": trend.get("streak_days") if trend else None,
            "streak_dir": trend.get("streak_dir") if trend else None,
            "acceleration": trend.get("acceleration") if trend else None,
            "pct_1d": trend.get("pct_1d") if trend else None,
            "pct_7d": trend.get("pct_7d") if trend else None,
            "pct_30d": trend.get("pct_30d") if trend else None,
            "ff_id": trend.get("ff_id") if trend else None,
            "ff_name": trend.get("ff_name") if trend else None,
            "ff_value": trend.get("value") if trend else None,
            "owner": owner.get("owner") if owner else None,
            "owner_team_id": owner.get("team_id") if owner else None,
            "clause": owner.get("clause") if owner else None,
            "clause_locked_until": owner.get("locked_until") if owner else None,
            "clause_locked": owner.get("locked") if owner else False,
            "shielded": owner.get("shielded") if owner else False,
            "player_team_id": owner.get("player_team_id") if owner else None,
            "is_mine": bool(owner and my_team_id and owner.get("team_id") == str(my_team_id)),
            "absence": absence_for(player),
            "starred": pid in starred,
            "market": market_by_player.get(pid),
            "offers": offers_by_player.get(pid) or [],
        })

    now_utc = datetime.now(timezone.utc)
    for row in rows:
        unlock = _parse_iso(row.get("clause_locked_until"))
        row["clause_hours_left"] = ((unlock - now_utc).total_seconds() / 3600
                                    if unlock else None)

    apply_scores(rows)
    return {
        "week": week,
        "fixtures": fixtures,
        "next_week_opens": next_week_opens,
        "completed_weeks": completed_weeks,
        "current_weight": current_weight,
        "players": rows,
        "teams": teams,
        "team_strength": strength,
        "unmatched_ff": unmatched,
        "matched_count": len(matched),
        "ownership_loaded": bool(ownership),
        "league_teams": league_teams,
        "activity": activity,
        "market": market_rows_league,
        "clauses": clause_calendar(rows),
        "my_team_id": str(my_team_id) if my_team_id else None,
    }


PROJECTION_DAMPING = 0.55
PROJECTION_CAP = 12.0


def _projected_pct(trend: dict | None) -> float:
    """Expected value change over the next 7 days, in percent.

    Recent daily rates are extrapolated but damped: these streaks mean-revert,
    so a raw 7x of yesterday's move would peg almost everyone at the cap.
    """
    if not trend:
        return 0.0
    daily = 0.0
    weights = 0.0
    for window, weight in ((1, 0.5), (3, 0.3), (7, 0.2)):
        pct = trend.get(f"pct_{window}d")
        if pct is None:
            continue
        daily += weight * (pct / window)
        weights += weight
    if not weights:
        return 0.0
    projected = (daily / weights) * 7 * PROJECTION_DAMPING
    return max(-PROJECTION_CAP, min(PROJECTION_CAP, projected))


def apply_scores(rows: list[dict[str, Any]]) -> None:
    winsor = {"points_value": 0.03, "xpts": 0.01}
    columns = {key: _zscores([row.get(key) or 0.0 for row in rows],
                             winsorize=winsor.get(key, 0.0))
               for key in SCORE_WEIGHTS}
    for index, row in enumerate(rows):
        score = sum(SCORE_WEIGHTS[key] * columns[key][index] for key in SCORE_WEIGHTS)
        if not row["available"]:
            score -= 1.5
        elif row["status"] == "doubtful":
            score -= 0.4
        row["score"] = score
    ranked = sorted(rows, key=lambda r: r["score"], reverse=True)
    for rank, row in enumerate(ranked, start=1):
        row["rank"] = rank
    for position_id in set(r["position_id"] for r in rows):
        pool = sorted((r for r in rows if r["position_id"] == position_id),
                      key=lambda r: r["score"], reverse=True)
        for rank, row in enumerate(pool, start=1):
            row["position_rank"] = rank


# --- league ownership -------------------------------------------------------

def _load_ownership(league_id: str) -> tuple[dict[str, dict[str, Any]], dict[str, dict[str, Any]]]:
    """Walk every squad once.

    Returns (player_id -> ownership, team_id -> team summary). The team summary
    carries the cash estimate: the standings figure is total worth (cash plus
    squad), so subtracting the squad's market value leaves the cash. Everyone
    starts a season on the same total, and daily rewards plus trades are what make
    the figures diverge, which is exactly what we want to recover.
    """
    ownership: dict[str, dict[str, Any]] = {}
    teams: dict[str, dict[str, Any]] = {}
    now = datetime.now(timezone.utc)

    for entry in laliga.standings(league_id):
        team_id = entry["teamId"]
        if not team_id:
            continue
        try:
            squad = laliga.team_squad(league_id, team_id)
        except Exception as exc:
            log.warning("squad unreachable", extra={"team_id": team_id,
                                                    "error_type": type(exc).__name__})
            continue

        squad_value = 0.0
        clause_total = 0.0
        count = 0
        for slot in laliga.squad_players(squad):
            master = slot.get("playerMaster") or {}
            pid = str(master.get("id") or "")
            if not pid:
                continue
            unlock = _parse_iso(slot.get("buyoutClauseLockedEndTime"))
            ownership[pid] = {
                "owner": entry.get("manager") or entry.get("teamName") or team_id,
                "team_id": str(team_id),
                "clause": slot.get("buyoutClause"),
                "locked_until": slot.get("buyoutClauseLockedEndTime"),
                "locked": bool(unlock and unlock > now),
                "player_team_id": slot.get("playerTeamId") or slot.get("id"),
                "shielded": bool(slot.get("isShielded")),
            }
            squad_value += float(master.get("marketValue") or 0)
            clause_total += float(slot.get("buyoutClause") or 0)
            count += 1

        # The standings figure is squad value, not total worth: it matches the sum of
        # the squad's market values exactly, and carries no cash. Cash comes from
        # reconstruct_cash instead.
        teams[str(team_id)] = {
            "team_id": str(team_id),
            "user_id": str(entry.get("userId") or ""),
            "name": entry.get("teamName"),
            "manager": entry.get("manager"),
            "points": entry.get("points"),
            "live_points": entry.get("livePoints"),
            "position": entry.get("position"),
            "reported_value": float(entry.get("teamValue") or 0),
            "squad_value": squad_value,
            "clause_total": clause_total,
            "players": count,
            "estimated_cash": 0.0,
            "cash_is_estimate": True,
        }

    return ownership, teams


# --- recommendations --------------------------------------------------------

def squad_shape(players: list[dict]) -> dict[int, dict[str, int]]:
    shape: dict[int, dict[str, int]] = {}
    for position_id, ideal in IDEAL_PER_POSITION.items():
        owned = [p for p in players if p["position_id"] == position_id]
        shape[position_id] = {
            "owned": len(owned),
            "ideal": ideal,
            "minimum": MIN_PER_POSITION[position_id],
            "gap": max(0, MIN_PER_POSITION[position_id] - len(owned)),
            "surplus": max(0, len(owned) - ideal),
        }
    return shape


def recommend(universe: dict[str, Any], *, budget: int, max_debt: int = 0,
              limit: int = 15) -> dict[str, Any]:
    players = universe["players"]
    mine = [p for p in players if p["is_mine"]]
    shape = squad_shape(mine)
    spending_power = budget + max(0, max_debt)

    free_agents = [p for p in players if not p["owner"] and p["available"]]
    rivals = [p for p in players if p["owner"] and not p["is_mine"]]

    # Only what is actually in today's market can be bid on. A free agent who is not
    # listed is a watchlist entry, not a signing: recommending him would be advice
    # you cannot act on.
    def entry(player: dict, cost: float, route: str, **extra: Any) -> dict[str, Any]:
        need = shape.get(player["position_id"], {})
        ideal = player.get("ideal_bid") or 0
        return {**player, "entry_cost": cost, "route": route,
                "position_gap": need.get("gap", 0),
                "affordable": cost <= spending_power,
                "priority": (player["score"]
                             + 0.35 * need.get("gap", 0)
                             + (0.5 if player.get("starred") else 0.0)
                             + (0.3 if ideal and cost <= ideal else 0.0)),
                **extra}

    bids_now, asks, my_listings = [], [], []
    for player in players:
        listing = player.get("market")
        if not listing:
            continue
        cost = listing["min_bid"] or player["value"]
        over = (cost / player["value"]) if player["value"] else None
        if listing["is_mine"]:
            my_listings.append({**player, "entry_cost": cost, "route": "mi venta",
                                "ask_ratio": over,
                                "underpriced": bool(over and over < 1.0)})
        elif listing["kind"] == "libre":
            if player["available"]:
                bids_now.append(entry(player, cost, "mercado libre",
                                      ask_ratio=over, bids=listing.get("bids")))
        else:
            asks.append(entry(player, cost, "venta de rival", ask_ratio=over,
                              seller=listing.get("seller"),
                              overpriced=bool(over and over > 1.15)))

    watchlist = [entry(p, p["value"], "sin listar")
                 for p in free_agents
                 if not p.get("market") and (p.get("starred") or p["score"] > 0.9)]

    locked = [p for p in rivals if p.get("clause") and p["clause_locked"]]
    unlock_dates = sorted(p["clause_locked_until"] for p in locked if p.get("clause_locked_until"))
    raids = []
    for player in rivals:
        clause = player.get("clause")
        if not clause or player["clause_locked"] or clause > spending_power:
            continue
        need = shape.get(player["position_id"], {})
        premium = (clause / player["value"]) if player["value"] else None
        raids.append({**player,
                      "entry_cost": clause,
                      "route": "clausula",
                      "clause_premium": premium,
                      "position_gap": need.get("gap", 0),
                      "priority": player["score"] + 0.35 * need.get("gap", 0)
                                  - 0.5 * max(0.0, (premium or 1.0) - 1.5)})

    sells = []
    for player in mine:
        need = shape.get(player["position_id"], {})
        reasons = []
        if not player["available"]:
            reasons.append(f"baja ({player['status']})")
        if player["projected_pct"] < -1.5:
            reasons.append(f"valor cayendo {player['projected_pct']:.1f}%/7d")
        if player["start_probability"] is not None and player["start_probability"] < 40:
            reasons.append(f"titularidad {player['start_probability']}%")
        if need.get("surplus"):
            reasons.append(f"exceso de {POSITIONS[player['position_id']]}")
        if player["points_value"] < 0.15 and player["value"] > 5e6:
            reasons.append("pocos puntos por millon")
        sells.append({**player,
                      "reasons": reasons,
                      "pressure": -player["score"] + 0.4 * len(reasons)})

    rival_teams = _rival_cash(universe, budget)
    exposure = []
    for player in mine:
        clause, value = player.get("clause"), player["value"]
        if not clause or not value or player["clause_locked"]:
            continue
        able = [t for t in rival_teams if t["estimated_cash"] >= clause]
        margin = clause / value
        # A cheap clause only matters if somebody in the league can actually pay it.
        if not able and margin >= 1.6:
            continue
        risk = max(0.2, player["score"]) * (0.4 * len(able) + max(0.0, 1.6 - margin))
        exposure.append({**player, "clause_margin": margin,
                         "threats": len(able),
                         "top_threat": able[0]["manager"] if able else None,
                         "risk": risk})

    for bucket in (bids_now, asks, watchlist, raids):
        bucket.sort(key=lambda p: p["priority"], reverse=True)
    sells.sort(key=lambda p: p["pressure"], reverse=True)
    exposure.sort(key=lambda p: p["risk"], reverse=True)
    my_listings.sort(key=lambda p: p["entry_cost"], reverse=True)

    offers = []
    for player in mine:
        received = player.get("offers") or []
        if not received:
            continue
        best = received[0]
        amount = float(best.get("money") or 0)
        value = player["value"] or 1
        listing = player.get("market") or {}
        ask = float(listing.get("min_bid") or 0)
        offers.append({**player,
                       "offer_id": str(best.get("id")),
                       "offer_amount": amount,
                       "offer_expires": best.get("expirationDate"),
                       "offer_count": len(received),
                       "market_id": listing.get("market_id"),
                       "ask": ask,
                       "vs_value": amount / value,
                       "vs_ask": (amount / ask) if ask else None,
                       # Worth taking when they pay over the market value, or over what
                       # futbolfantasy thinks the player is worth paying for.
                       "worth_taking": amount >= value or (ask and amount >= ask)})
    offers.sort(key=lambda o: -o["vs_value"])

    # The reference for "is this clause worth paying" is your own squad: does the
    # money buy more points per million than what you already own? Benchmarking
    # against today's market instead brands everything a bargain, because a bad
    # market day drags the median down and says nothing about the player.
    squad_ppm = [p["xpts"] / (p["value"] / 1e6) for p in mine
                 if p["value"] and p["xpts"] > 0]
    benchmark = statistics.median(squad_ppm) if squad_ppm else 0.0

    clauses = universe.get("clauses") or {}
    # A rival's clause is only an opportunity if it opens soon AND you can pay it.
    upcoming_raids = []
    for player in clauses.get("rivals_soon", []):
        clause = player.get("clause") or 0
        if not clause or player["score"] <= 0.4:
            continue
        affordable = clause <= spending_power
        ppm = (player["xpts"] / (clause / 1e6)) if clause else 0.0
        upcoming_raids.append({**player, "entry_cost": clause, "affordable": affordable,
                               "clause_premium": clause / player["value"] if player["value"]
                                                 else None,
                               "ppm_at_clause": ppm,
                               "vs_market": (ppm / benchmark) if benchmark else None,
                               "verdict": _raid_verdict(ppm, benchmark, affordable,
                                                        player["xpts"])})

    # Rank the ones that cleared the gate: a tag is only useful if it separates them.
    passed = sorted((r for r in upcoming_raids if r["verdict"] == ""),
                    key=lambda r: -r["ppm_at_clause"])
    for index, row in enumerate(passed):
        share = index / max(1, len(passed) - 1)
        row["verdict"] = "chollo" if share <= 0.25 else "renta" if share <= 0.6 else "justo"
    # Every clause in a league tends to unlock at the same instant, so the time is
    # usually a tie: break it by who is worth taking, not by squad order.
    RAID_ORDER = {"chollo": 0, "renta": 1, "justo": 2, "caro": 3,
                  "sin datos": 4, "sin referencia": 4, "no te llega": 5}
    upcoming_raids.sort(key=lambda p: (RAID_ORDER.get(p["verdict"], 9),
                                       round(p["hours_left"]), -p["ppm_at_clause"]))

    return {
        "budget": budget,
        "max_debt": max_debt,
        "spending_power": spending_power,
        "squad": sorted(mine, key=lambda p: (p["position_id"], -p["score"])),
        "shape": shape,
        "bids_now": bids_now[:limit],
        "asks": asks[:limit],
        "watchlist": watchlist[:limit],
        "my_listings": my_listings,
        "offers": offers,
        "raids": raids[:limit],
        "sells": sells[:limit],
        "exposure": exposure[:limit],
        "rivals": rival_teams,
        "cash_model": universe.get("cash_model"),
        "free_agent_count": len(free_agents),
        "clauses_locked": len(locked),
        "clauses_unlock_from": unlock_dates[0] if unlock_dates else None,
        "squad_ppm_benchmark": benchmark,
        "my_clauses_soon": clauses.get("mine_soon", [])[:limit],
        "upcoming_raids": upcoming_raids[:limit],
        "starred": [p for p in players if p.get("starred")],
    }


def _raid_verdict(ppm_at_clause: float, benchmark: float, affordable: bool,
                  xpts: float) -> str:
    """Is paying this clause worth it, measured against your own squad.

    Not against futbolfantasy's ceiling: the game sets clauses at roughly 1.5x market
    value, so that comparison brands every raid "expensive" and tells you nothing.
    The question that discriminates is whether these euros buy more points per million
    than the players you already have.
    """
    if not affordable:
        return "no te llega"
    if xpts <= 0:
        return "sin datos"
    if not benchmark:
        return "sin referencia"
    # Absolute gate first: paying more per point than what you already own is bad no
    # matter how it ranks against the other candidates.
    if ppm_at_clause < benchmark:
        return "caro"
    return ""     # ranked afterwards, once the whole candidate set is known


def _rival_cash(universe: dict[str, Any], my_real_budget: int) -> list[dict[str, Any]]:
    """Rivals sorted by spending power, reconstructed from the transfer log."""
    teams = universe.get("league_teams") or {}
    my_team_id = universe.get("my_team_id")
    universe["cash_model"] = reconstruct_cash(
        universe.get("activity") or [], teams,
        my_team_id=my_team_id,
        my_real_cash=my_real_budget if my_real_budget else None)
    prices = sorted(m["min_bid"] for m in (universe.get("market") or []) if m.get("min_bid"))
    median_price = prices[len(prices) // 2] if prices else 5_000_000
    top_price = prices[-1] if prices else 50_000_000

    for team in teams.values():
        cash = team["estimated_cash"]
        if cash >= top_price:
            team["power"] = "holgado"
            team["power_note"] = "puede pagar lo mas caro del mercado"
        elif cash >= median_price * 2:
            team["power"] = "normal"
            team["power_note"] = "le llega a la mayoria del mercado"
        else:
            team["power"] = "justo"
            team["power_note"] = f"no le llega ni al jugador medio ({_short(median_price)})"

    # Your own row belongs in the ranking: the number only means something next to
    # the others, and leaving yourself out makes the table unreadable as a standing.
    everyone = list(teams.values())
    for team in everyone:
        team["is_me"] = str(team["team_id"]) == str(my_team_id)
    everyone.sort(key=lambda t: t["estimated_cash"], reverse=True)
    for position, team in enumerate(everyone, start=1):
        team["cash_position"] = position
    return everyone


def _short(amount: float) -> str:
    return f"{amount / 1e6:.1f}M" if amount >= 1e6 else f"{amount / 1e3:.0f}K"


# activityTypeId, decoded against this league's own history:
#   31 -> user1 bought from the market (the player is still in his squad)
#   33 -> user1 sold to the market (the player left his squad)
#    1 -> transfer between managers; user1 pays, user2 receives
#    9 -> a manager joined the league (no amount, no player)
ACTIVITY_TYPES = {
    1: {"kind": "traspaso", "cash": -1, "counterparty": +1},
    9: {"kind": "se une a la liga", "cash": 0, "counterparty": 0},
    31: {"kind": "compra", "cash": -1, "counterparty": 0},
    33: {"kind": "venta", "cash": +1, "counterparty": 0},
}

# Every manager starts on the same cash; the daily reward is the only drip that
# cannot be reconstructed from the log. Both are only fallbacks: when the session
# can read its own /money we anchor on that instead (see reconstruct_cash).
INITIAL_CASH = 100_000_000
DAILY_REWARD = 100_000


def normalize_activity(events: Iterable[dict], *, managers: dict[str, str] | None = None,
                       player_names: dict[str, str] | None = None) -> list[dict[str, Any]]:
    """Flatten the league log into one shape, resolving ids to names.

    The payload is all ids: `activityTypeId`, `user1Id`, optional `user2Id`,
    `playerMasterId`, `amount`. The raw event is kept so an unknown type can still
    be inspected instead of silently vanishing.
    """
    managers = managers or {}
    player_names = player_names or {}
    out: list[dict[str, Any]] = []

    for event in events:
        if not isinstance(event, dict):
            continue
        type_id = event.get("activityTypeId")
        spec = ACTIVITY_TYPES.get(type_id, {})
        user1 = str(event.get("user1Id") or "")
        user2 = str(event.get("user2Id") or "")
        amount = event.get("amount")
        # cash: -1 means user1 paid, +1 means user1 received.
        pays = spec.get("cash") == -1
        out.append({
            "date": str(event.get("createdAt") or "")[:19],
            "type_id": type_id,
            "kind": spec.get("kind") or f"tipo {type_id}",
            "known": bool(spec),
            "user1": user1,
            "user2": user2 or None,
            "buyer": managers.get(user1) if pays else (managers.get(user2) if user2 else None),
            "seller": (managers.get(user2) if user2 else None) if pays else managers.get(user1),
            "actor": managers.get(user1, user1),
            "player": player_names.get(str(event.get("playerMasterId"))),
            "player_id": str(event.get("playerMasterId")) if event.get("playerMasterId") else None,
            "amount": float(amount) if isinstance(amount, (int, float)) else None,
            "raw": event,
        })
    return out


def load_activity(league_id: str, *, pages: int = 20,
                  managers: dict[str, str] | None = None,
                  player_names: dict[str, str] | None = None) -> list[dict[str, Any]]:
    raw: list[dict] = []
    for index in range(pages):
        try:
            page = laliga.activity(league_id, index)
        except Exception as exc:
            log.warning("activity unreachable", extra={"index": index,
                                                       "error_type": type(exc).__name__})
            break
        if not page:
            break
        raw.extend(page)
    events = normalize_activity(raw, managers=managers, player_names=player_names)
    unknown = sorted({e["type_id"] for e in events if not e["known"]})
    if unknown:
        log.warning("unknown activity types", extra={"types": unknown})
    log.info("activity loaded", extra={"events": len(events)})
    return events


def load_market(league_id: str, my_team_id: str | None = None) -> list[dict[str, Any]]:
    """Today's market, normalized.

    Two kinds share the endpoint: `marketPlayerLeague` is the daily pool of
    unowned players the game puts up (biddable right now, `salePrice` is the
    floor), and `marketPlayerTeam` is a manager listing one of his own, where
    `salePrice` is his asking price and can be far above market value.
    """
    try:
        entries = laliga.market(league_id)
    except Exception as exc:
        log.warning("market unreachable", extra={"error_type": type(exc).__name__})
        return []

    out: list[dict[str, Any]] = []
    for entry in entries:
        master = entry.get("playerMaster") or {}
        seller = entry.get("sellerTeam") or {}
        seller_manager = (seller.get("manager") or {}).get("managerName")
        seller_team_id = str(seller.get("id") or "")
        out.append({
            "market_id": str(entry.get("id") or ""),
            "player_id": str(master.get("id") or ""),
            "kind": "libre" if entry.get("discr") == "marketPlayerLeague" else "venta",
            "min_bid": float(entry.get("salePrice") or 0),
            "market_value": float(master.get("marketValue") or 0),
            "expires": entry.get("expirationDate"),
            "bids": entry.get("numberOfBids"),
            "offers": entry.get("numberOfOffers"),
            "seller": seller_manager,
            "seller_team_id": seller_team_id or None,
            "is_mine": bool(my_team_id and seller_team_id == str(my_team_id)),
            "direct_offer": entry.get("directOffer"),
            # There is no endpoint that lists your own bids (GET on .../bid is 405,
            # POST-only), so the id has to come from whatever the market entry itself
            # exposes once a bid exists. Looked up generically rather than guessed.
            "my_bid_id": _find_bid_id(entry),
        })
    log.info("market loaded", extra={"entries": len(out),
                                    "libres": sum(1 for e in out if e["kind"] == "libre")})
    return out


def _find_bid_id(entry: dict[str, Any]) -> str | None:
    for key in ("bidId", "myBidId", "userBidId"):
        if entry.get(key):
            return str(entry[key])
    for key in ("bids", "myBids", "userBids", "bid"):
        value = entry.get(key)
        if isinstance(value, dict) and value.get("id"):
            return str(value["id"])
        if isinstance(value, list):
            for item in value:
                if isinstance(item, dict) and item.get("id"):
                    return str(item["id"])
    return None


def load_offers(league_id: str, listings: list[dict[str, Any]],
                ownership: dict[str, dict[str, Any]]) -> dict[str, list[dict[str, Any]]]:
    """Offers received for the players you have listed.

    Keyed by playerTeamId, which only the squad walk knows, so ownership has to
    supply it. One request per listing, and only for your own listings.
    """
    offers: dict[str, list[dict[str, Any]]] = {}
    for listing in listings:
        player_id = listing.get("player_id")
        slot = ownership.get(str(player_id)) or {}
        player_team_id = slot.get("player_team_id")
        if not player_team_id:
            continue
        try:
            received = laliga.player_offers(league_id, str(player_team_id))
        except Exception as exc:
            log.debug("offers unreachable", extra={"player_id": player_id,
                                                  "error_type": type(exc).__name__})
            continue
        pending = [o for o in received if (o.get("status") or "pending") == "pending"]
        if pending:
            offers[str(player_id)] = sorted(pending, key=lambda o: -(o.get("money") or 0))
    log.info("offers loaded", extra={"listings": len(listings), "with_offers": len(offers)})
    return offers


# matchState observed from the calendar: 1 not started, 7 finished with a score. The
# code for a match under way has not been seen, so liveness is derived from the clock
# instead of a guessed constant — see schedule.live_matches.
MATCH_PENDING, MATCH_FINISHED = 1, 7


def load_fixtures(week: dict[str, Any],
                  teams: list[dict[str, Any]] | None = None) -> list[dict[str, Any]]:
    """This week's matches, with kick-off and state.

    A match is the other thing that changes the world without touching the transfer
    log or the market: points accrue, lineups lock, someone limps off. The refresh
    loop cannot see any of that from the two cheap endpoints, so it needs the
    fixture list to know when to look.
    """
    try:
        rows = laliga.calendar(int(week.get("weekNumber") or 1))
    except Exception as exc:
        log.debug("calendar unavailable", extra={"error_type": type(exc).__name__})
        return []

    names = {str(team.get("id")): team.get("shortName") or team.get("name")
             for team in (teams or [])}
    fixtures = []
    for row in rows:
        local, visitor = str(row.get("localId") or ""), str(row.get("visitorId") or "")
        fixtures.append({
            "id": str(row.get("id") or ""),
            "kickoff": row.get("matchDate") or row.get("date"),
            "state": row.get("matchState"),
            "local_id": local, "visitor_id": visitor,
            "local": names.get(local) or local, "visitor": names.get(visitor) or visitor,
            "local_score": row.get("localScore"), "visitor_score": row.get("visitorScore"),
        })
    log.debug("fixtures loaded", extra={"week": week.get("weekNumber"),
                                       "matches": len(fixtures),
                                       "pending": sum(1 for f in fixtures
                                                      if f["state"] == MATCH_PENDING)})
    return fixtures


def clause_calendar(players: Iterable[dict], *, horizon_hours: int = 24 * 10) -> dict[str, Any]:
    """When each locked clause opens up.

    Two sides of the same date: your own players become raidable the moment their
    lock lifts, and a rival's player becomes raidable *by you*. Both are actionable
    only if you know the date is coming, which is why it needs its own section.
    """
    now = datetime.now(timezone.utc)
    mine: list[dict[str, Any]] = []
    rivals: list[dict[str, Any]] = []

    for player in players:
        unlock = _parse_iso(player.get("clause_locked_until"))
        if not player.get("owner") or not unlock:
            continue
        hours = (unlock - now).total_seconds() / 3600
        row = {**player, "unlock_at": unlock.isoformat(), "hours_left": hours,
               "unlocked": hours <= 0}
        (mine if player.get("is_mine") else rivals).append(row)

    # Soonest first, and only what is worth acting on: your good players and the
    # rivals' players you could actually afford are the only rows that matter.
    by_hour_then_worth = lambda r: (round(r["hours_left"]), -r["score"])
    mine.sort(key=by_hour_then_worth)
    rivals.sort(key=by_hour_then_worth)
    return {
        "mine": mine,
        "rivals": rivals,
        "mine_soon": [r for r in mine if r["hours_left"] <= horizon_hours],
        "rivals_soon": [r for r in rivals if r["hours_left"] <= horizon_hours],
        "next_unlock": mine[0]["unlock_at"] if mine else None,
    }


def enrich_activity_values(events: list[dict[str, Any]], *, limit: int = 18) -> None:
    """Add what each player was worth the day he changed hands.

    The amount alone does not say whether it was a bargain or a panic buy. The
    public market-value history is a daily series per player, so the value on the
    event's own date turns "paid 92M" into "paid 92M for someone worth 78M".
    """
    biggest = sorted((e for e in events if e.get("amount") and e.get("player_id")),
                     key=lambda e: -(e["amount"] or 0))[:limit]
    for event in biggest:
        try:
            history = laliga.player_market_value(event["player_id"])
        except Exception as exc:
            log.debug("value history unavailable",
                      extra={"player_id": event["player_id"],
                             "error_type": type(exc).__name__})
            continue
        day = (event.get("date") or "")[:10]
        if not history or not day:
            continue
        # The series is daily; take the last point at or before the trade.
        on_or_before = [point for point in history
                        if str(point.get("date", ""))[:10] <= day]
        point = (on_or_before or history)[-1 if on_or_before else 0]
        value = float(point.get("marketValue") or 0)
        if not value:
            continue
        event["value_then"] = value
        event["premium"] = event["amount"] / value
        event["premium_abs"] = event["amount"] - value
    log.info("activity values enriched", extra={"enriched": sum(1 for e in events
                                                               if e.get("value_then"))})


def reconstruct_cash(events: Iterable[dict], teams: dict[str, dict[str, Any]], *,
                     my_team_id: str | None = None,
                     my_real_cash: int | None = None) -> dict[str, Any]:
    """Derive every manager's cash from the transfer log.

    The API exposes /money for your own team only, and the standings figure turns
    out to be squad value alone, so cash has to be reconstructed:

        cash = starting cash + rewards claimed + sales - purchases

    Purchases and sales all live in the log. Rewards are the one term it does not
    record, so instead of guessing we anchor the whole league on the one cash
    figure we can read — your own: `base = my_cash - my_net` folds your starting
    cash and your claimed rewards into a single measured constant, and every rival
    gets that same base plus their own net. The residual error is therefore only
    the difference in rewards claimed, bounded by 100k a day.
    """
    net: dict[str, float] = {}
    for event in events:
        amount = event.get("amount")
        if not amount:
            continue
        spec = ACTIVITY_TYPES.get(event.get("type_id"), {})
        if spec.get("cash"):
            net[event["user1"]] = net.get(event["user1"], 0.0) + spec["cash"] * amount
        if spec.get("counterparty") and event.get("user2"):
            net[event["user2"]] = net.get(event["user2"], 0.0) + spec["counterparty"] * amount

    by_user = {str(team.get("user_id")): team for team in teams.values() if team.get("user_id")}
    for user_id, team in by_user.items():
        team["net_flow"] = net.get(user_id, 0.0)

    base = float(INITIAL_CASH)
    anchored = False
    mine = teams.get(str(my_team_id)) if my_team_id else None
    if mine and my_real_cash is not None:
        base = my_real_cash - mine.get("net_flow", 0.0)
        anchored = True

    for team in teams.values():
        team["estimated_cash"] = max(0.0, base + team.get("net_flow", 0.0))
        team["cash_is_estimate"] = True
    if mine and my_real_cash is not None:
        mine["estimated_cash"] = float(my_real_cash)
        mine["cash_is_estimate"] = False

    model = {
        "base": base,
        "anchored": anchored,
        "implied_rewards": (base - INITIAL_CASH) if anchored else None,
        "uncertainty": DAILY_REWARD * 10,
        "events_with_cash": sum(1 for e in events if e.get("amount")
                                and ACTIVITY_TYPES.get(e.get("type_id"), {}).get("cash")),
    }
    log.info("cash reconstructed", extra={"base": round(base), "anchored": anchored,
                                         "teams": len(teams)})
    return model


def deep_enrich(rows: list[dict[str, Any]], *, limit: int = 20) -> int:
    """Replace the price-derived baseline with real last-season output.

    The API ships no history for promoted clubs or for the star records it
    recreated this season, so for a shortlist we read futbolfantasy's player page
    and recompute the baseline from the matchdays he actually played. One request
    per player and the pages are heavy, hence opt-in and shortlist-only.
    """
    fixed = 0
    for row in rows[:limit]:
        if not row.get("prior_based") or not row.get("ff_id"):
            continue
        slug = matching.slugify_ff(row.get("ff_name") or row["name"])
        try:
            page = ff.player_page(slug)
        except Exception as exc:
            log.debug("deep enrich skipped", extra={"player": row["name"], "slug": slug,
                                                    "error_type": type(exc).__name__})
            continue
        if not page.get("games_played"):
            continue
        base_week = page["total_points"] / WEEKS_IN_SEASON
        scale = (base_week / row["base_week"]) if row["base_week"] else 1.0
        row["base_week"] = base_week
        row["last_season_points"] = page["total_points"]
        row["last_season_games"] = page["games_played"]
        row["prior_based"] = False
        row["xpts"] *= scale
        row["points_value"] = (row["xpts"] / (row["value"] / 1e6)) if row["value"] else 0.0
        row["source"] = "futbolfantasy"
        fixed += 1
    log.info("deep enrich", extra={"fixed": fixed, "considered": min(limit, len(rows))})
    return fixed


def enrich_buckets(advice: dict[str, Any], *, limit: int = 15) -> None:
    """Enrich every shortlist in one pass, each player fetched at most once.

    The buckets overlap heavily (a market player is in `bids_now` and possibly in
    the watchlist and the raid list too), so enriching them one by one meant tens of
    duplicated requests per refresh — enough for futbolfantasy to answer 429.
    """
    buckets = ("bids_now", "asks", "watchlist", "raids", "upcoming_raids")
    rows_by_player: dict[str, list[dict]] = {}
    for name in buckets:
        for row in (advice.get(name) or [])[:limit]:
            if row.get("ff_id"):
                rows_by_player.setdefault(str(row["ff_id"]), []).append(row)

    fetched = 0
    for ff_id, rows in rows_by_player.items():
        try:
            detail = ff.player_detail(ff_id)
        except http.RateLimited as exc:
            log.warning("futbolfantasy rate limited: dejo de enriquecer este ciclo",
                        extra={"fetched": fetched, "pending": len(rows_by_player) - fetched,
                               "retry_after": exc.retry_after})
            break
        except Exception:
            continue
        fetched += 1
        for row in rows:
            _apply_detail(row, detail)
    log.info("detail enrichment", extra={"unique_players": len(rows_by_player),
                                        "fetched": fetched})


def _apply_detail(row: dict[str, Any], detail: dict[str, Any]) -> None:
    row["ideal_bid"] = detail.get("ideal_bid")
    row["max_value"] = detail.get("max_value")
    row["min_value"] = detail.get("min_value")
    row["max_date"] = detail.get("max_date")
    row["injury_marks"] = detail.get("injury_marks")
    entry = row.get("entry_cost") or row.get("value")
    ideal = detail.get("ideal_bid") or 0
    row["bid_headroom"] = (ideal - entry) if ideal else None
    row["profitable"] = bool(ideal and entry and ideal >= entry)
    history = detail.get("history") or detail.get("prev_season_history") or []
    row["value_history"] = [point["value"] for point in history][-60:]
