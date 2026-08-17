"""Cross-source identity resolution.

LaLiga and futbolfantasy use different team ids (LaLiga 3 = Athletic while
futbolfantasy 3 = Barcelona) and different player spellings, so everything is
matched by normalized team name first and normalized player name second.
"""
from __future__ import annotations

import difflib
import re
import unicodedata
from typing import Any, Iterable

from .config import FF_TEAM_ALIASES

_FF_POSITIONS = {
    "portero": 1,
    "defensa": 2,
    "centrocampista": 3,
    "mediocampista": 3,
    "medio": 3,
    "delantero": 4,
    "entrenador": 5,
}

_NOISE = re.compile(r"\b(fc|cf|ud|cd|rc|rcd|ca|sd|club|de|del|real)\b")


def normalize(text: str | None) -> str:
    if not text:
        return ""
    stripped = unicodedata.normalize("NFKD", text)
    stripped = "".join(c for c in stripped if not unicodedata.combining(c))
    stripped = stripped.lower().replace("ñ", "n")
    stripped = re.sub(r"[^a-z0-9 ]+", " ", stripped)
    return re.sub(r"\s+", " ", stripped).strip()


def normalize_team(name: str | None) -> str:
    base = normalize(name)
    base = FF_TEAM_ALIASES.get(base, base)
    base = _NOISE.sub(" ", base)
    # Drop single letters so "C.A. Osasuna" and "R. Racing Club" reduce like their short forms.
    tokens = [t for t in base.split() if len(t) > 1]
    return " ".join(tokens)


def ff_position_id(position: str | None) -> int | None:
    return _FF_POSITIONS.get(normalize(position))


def surname(name: str) -> str:
    tokens = normalize(name).split()
    return tokens[-1] if tokens else ""


def build_team_index(teams_master: Iterable[dict]) -> dict[str, dict]:
    """normalized team name -> LaLiga team record."""
    index: dict[str, dict] = {}
    for team in teams_master:
        for key in (team.get("name"), team.get("slug"), team.get("shortName")):
            norm = normalize_team(key)
            if norm:
                index.setdefault(norm, team)
    return index


def _name_variants(player: dict) -> list[str]:
    return [v for v in (player.get("nickname"), player.get("name"), player.get("slug")) if v]


_TOKEN_ALIASES = {
    "jr": "junior",
    "fdez": "fernandez",
    "fdz": "fernandez",
    "glez": "gonzalez",
    "gzlez": "gonzalez",
    "hdez": "hernandez",
    "mtnez": "martinez",
    "dguez": "dominguez",
    "rgez": "rodriguez",
    "rodz": "rodriguez",
}


def _is_abbrev(short: str, long: str) -> bool:
    """`fdez` -> `fernandez`: same initial and letters in order."""
    if _TOKEN_ALIASES.get(short) == long:
        return True
    if len(short) < 3 or len(short) >= len(long) or short[0] != long[0]:
        return False
    position = 0
    for char in short:
        position = long.find(char, position)
        if position < 0:
            return False
        position += 1
    return True


def _token_match(short: str, long: str) -> bool:
    return short == long or long.startswith(short) or _is_abbrev(short, long)


def _prefix_subset(mine: list[str], theirs: list[str]) -> bool:
    """Every LaLiga token maps to a distinct futbolfantasy token."""
    if not mine or len(mine) > len(theirs):
        return False
    used: set[int] = set()
    for token in mine:
        index = next((i for i, other in enumerate(theirs)
                      if i not in used and _token_match(token, other)), None)
        if index is None:
            return False
        used.add(index)
    return True


def _nickname_variant(mine: list[str], theirs: list[str]) -> bool:
    """`alex balde` vs `alejandro balde`: same surname, first names share a stem."""
    if len(mine) < 2 or len(theirs) < 2 or mine[-1] != theirs[-1]:
        return False
    a, b = mine[0], theirs[0]
    shared = 0
    for x, y in zip(a, b):
        if x != y:
            break
        shared += 1
    return shared >= 3


def _fuzzy(mine: str, theirs: str) -> bool:
    return difflib.SequenceMatcher(None, mine, theirs).ratio() >= 0.82


def _passes() -> list:
    return [
        lambda pl, ff_: any(v == ff_["norm"] or v == ff_["display"] for v in pl["variants"]),
        lambda pl, ff_: any(set(t) == set(ff_["tokens"]) for t in pl["token_sets"]),
        lambda pl, ff_: any(_prefix_subset(t, ff_["tokens"]) for t in pl["token_sets"]),
        lambda pl, ff_: any(_nickname_variant(t, ff_["tokens"]) for t in pl["token_sets"]),
        lambda pl, ff_: any(_fuzzy(v, ff_["norm"]) for v in pl["variants"]),
    ]


def match_market(players: list[dict], market_rows: list[dict],
                 team_index: dict[str, dict]) -> tuple[dict[str, dict], list[dict]]:
    """Return (laliga_player_id -> futbolfantasy row, unmatched futbolfantasy rows).

    LaLiga only exposes short nicknames ("f de jong", "aitor fdez") while
    futbolfantasy uses full names, so matching runs in passes from strict to
    loose. Each pass only commits pairs that are unambiguous in both directions,
    which keeps "raul" from stealing the slot of "raul moro".
    """
    ff_team_to_laliga: dict[str, str] = {}
    for row in market_rows:
        team = team_index.get(normalize_team(row.get("ff_team")))
        if team:
            ff_team_to_laliga[str(row.get("ff_team_id"))] = str(team["id"])

    prepared_players = []
    for player in players:
        variants = [normalize(v) for v in _name_variants(player)]
        # "R.P. Bigas" also needs to be tried as plain "bigas".
        for variant in list(variants):
            trimmed = " ".join(t for t in variant.split() if len(t) > 1)
            if trimmed and trimmed != variant:
                variants.append(trimmed)
        prepared_players.append({
            "player": player,
            "id": str(player["id"]),
            "team": str(player.get("teamId")),
            "position": int(player.get("positionId") or 0),
            "variants": [v for v in variants if v],
            "token_sets": [v.split() for v in variants if v],
        })

    prepared_rows = []
    for row in market_rows:
        norm = normalize(row.get("ff_name"))
        prepared_rows.append({
            "row": row,
            "norm": norm,
            "display": normalize(row.get("display_name")),
            "tokens": norm.split(),
            "team": ff_team_to_laliga.get(str(row.get("ff_team_id"))),
            "position": ff_position_id(row.get("position")),
        })

    matched: dict[str, dict] = {}
    open_players = {p["id"]: p for p in prepared_players}
    open_rows = list(prepared_rows)

    # Second round ignores the team so mid-window transfers (a player futbolfantasy
    # still lists at his old club) still resolve.
    rounds = [(predicate, same_team) for same_team in (True, False) for predicate in _passes()]
    for predicate, same_team in rounds:
        progress = True
        while progress:
            progress = False
            pairs: list[tuple[dict, dict]] = []
            for entry in open_rows:
                pool = [p for p in open_players.values()
                        if not same_team or not entry["team"] or p["team"] == entry["team"]]
                hits = [p for p in pool if predicate(p, entry)]
                if len(hits) > 1 and entry["position"]:
                    narrowed = [p for p in hits if p["position"] == entry["position"]]
                    hits = narrowed or hits
                if len(hits) == 1:
                    pairs.append((entry, hits[0]))

            claimed: dict[str, int] = {}
            for _, player in pairs:
                claimed[player["id"]] = claimed.get(player["id"], 0) + 1
            for entry, player in pairs:
                if claimed[player["id"]] != 1 or player["id"] not in open_players:
                    continue
                matched[player["id"]] = entry["row"]
                del open_players[player["id"]]
                open_rows = [r for r in open_rows if r is not entry]
                progress = True

    return matched, [entry["row"] for entry in open_rows]


def slugify_ff(name: str) -> str:
    return normalize(name).replace(" ", "-")


def player_label(player: dict[str, Any]) -> str:
    return player.get("nickname") or player.get("name") or str(player.get("id"))
