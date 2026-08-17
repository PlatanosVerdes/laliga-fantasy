"""LaLiga Fantasy API client (season 26/27 routes).

Public data needs no session; anything league-scoped needs the Bearer token.
Note the route inconsistency kept from the official app: standings and squads
live under `/leagues/{id}/...` while market and buyout live under `/league/{id}/...`.
"""
from __future__ import annotations

from typing import Any

from . import auth, http
from .config import API_BASE, API_HEADERS, CMP

MINUTE = 60
HOUR = 3600


def _headers(authenticated: bool) -> dict[str, str]:
    headers = dict(API_HEADERS)
    if authenticated:
        headers["Authorization"] = f"Bearer {auth.bearer()}"
    return headers


def _get(path: str, *, authenticated: bool = False, ttl: float = 0, tag: str = "api") -> Any:
    sep = "&" if "?" in path else "?"
    url = f"{API_BASE}{path}{sep}x-lang=es"
    return http.get_json(url, headers=_headers(authenticated), ttl=ttl, tag=tag)


def _unwrap(payload: Any) -> list[dict]:
    if isinstance(payload, list):
        return payload
    if isinstance(payload, dict):
        for key in ("elements", "leagues", "teams", "data", "players"):
            value = payload.get(key)
            if isinstance(value, list):
                return value
    return []


# --- public -----------------------------------------------------------------

def all_players(ttl: float = 6 * HOUR) -> list[dict]:
    return _unwrap(_get(f"{CMP}/players", ttl=ttl, tag="players"))


def teams_master(ttl: float = 24 * HOUR) -> list[dict]:
    return _unwrap(_get("/v3/teams-master", ttl=ttl, tag="teams"))


def current_week(ttl: float = 30 * MINUTE) -> dict:
    return _get(f"{CMP}/week/current", ttl=ttl, tag="week")


def calendar(week: int, ttl: float = 6 * HOUR) -> list[dict]:
    return _unwrap(_get(f"{CMP}/calendar?weekNumber={week}", ttl=ttl, tag="calendar"))


def player(player_id: str, ttl: float = 6 * HOUR) -> dict:
    return _get(f"{CMP}/player/{player_id}", ttl=ttl, tag="player")


def player_market_value(player_id: str, ttl: float = 12 * HOUR) -> list[dict]:
    return _unwrap(_get(f"{CMP}/player/{player_id}/market-value", ttl=ttl, tag="mv"))


# --- session-scoped ---------------------------------------------------------

def me(ttl: float = HOUR) -> dict:
    return _get("/v4/user/me", authenticated=True, ttl=ttl, tag="me")


def leagues(ttl: float = HOUR) -> list[dict]:
    return _unwrap(_get(f"{CMP}/leagues", authenticated=True, ttl=ttl, tag="leagues"))


def standings(league_id: str, ttl: float = 30 * MINUTE) -> list[dict]:
    raw = _unwrap(_get(f"{CMP}/leagues/{league_id}/standing", authenticated=True,
                       ttl=ttl, tag="standing"))
    return [_normalize_standing(entry) for entry in raw]


def _normalize_standing(entry: dict) -> dict:
    team = entry.get("team") or {}
    manager = team.get("manager") or {}
    return {
        **entry,
        "teamId": str(entry.get("id") or team.get("id") or ""),
        "teamName": entry.get("name") or team.get("name"),
        "points": entry.get("points") or team.get("teamPoints") or team.get("points") or 0,
        "teamValue": entry.get("teamValue") or team.get("teamValue") or 0,
        "manager": entry.get("manager") if isinstance(entry.get("manager"), str)
                   else manager.get("managerName"),
        "userId": str(entry.get("userId") or manager.get("id") or ""),
    }


def activity(league_id: str, index: int = 0, ttl: float = MINUTE) -> list[dict]:
    return _unwrap(_get(f"{CMP}/leagues/{league_id}/activity/{index}", authenticated=True,
                        ttl=ttl, tag="activity"))


def team_squad(league_id: str, team_id: str, ttl: float = 30 * MINUTE) -> dict:
    return _get(f"{CMP}/leagues/{league_id}/teams/{team_id}", authenticated=True,
                ttl=ttl, tag="squad")


def team_money(team_id: str, ttl: float = MINUTE) -> dict:
    return _get(f"{CMP}/teams/{team_id}/money", authenticated=True, ttl=ttl, tag="money")


def market(league_id: str, ttl: float = MINUTE) -> list[dict]:
    return _unwrap(_get(f"{CMP}/league/{league_id}/market", authenticated=True,
                        ttl=ttl, tag="market"))


def squad_players(squad: dict) -> list[dict]:
    if not squad:
        return []
    for candidate in (squad.get("players"), (squad.get("data") or {}).get("players")):
        if isinstance(candidate, list):
            return candidate
    return []
