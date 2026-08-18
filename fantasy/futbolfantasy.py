"""futbolfantasy.com scrapers.

Three sources, in order of value:
  * /analytics/laliga-fantasy/mercado  — one request, every player, value deltas
    over 1/2/3/7/14/30 days, trend, acceleration, next fixture and the odds of
    starting it.
  * /analytics/laliga-fantasy/mercado/detalle/{id} — per player: full value
    history, season max/min and the site's "puja maxima rentable".
  * /jugadores/{slug} — per matchday role, points and injury markers.
"""
from __future__ import annotations

import html as html_mod
import re
import threading
import time
from typing import Any

from . import http
from .config import FF_DETAIL_URL, FF_HEADERS, FF_MARKET_URL, FF_PLAYER_URL

HOUR = 3600


# futbolfantasy is somebody's site, not an API we are entitled to hammer: keep a
# floor between requests that actually leave this process (cache hits don't count).
MIN_INTERVAL = 0.4
_last_request = 0.0
_throttle = threading.Lock()


def _fetch(url: str, *, ttl: float, tag: str) -> str:
    global _last_request
    cached = http.cached(url, tag, ttl)
    if cached is not None:
        return cached
    with _throttle:
        wait = MIN_INTERVAL - (time.monotonic() - _last_request)
        if wait > 0:
            time.sleep(wait)
        _last_request = time.monotonic()
    return http.fetch(url, headers=FF_HEADERS, ttl=ttl, tag=tag, timeout=60)


def _num(text: str | None) -> float | None:
    """Parse both raw JS numbers from data-* attributes ("-0.0739") and rendered
    Spanish text ("1.715.638", "8,1"). A comma is the tell for the latter."""
    if text is None:
        return None
    text = text.strip()
    if "," in text:
        text = text.replace(".", "").replace(",", ".")
    try:
        return float(text)
    except ValueError:
        return None


def parse_team_map(html: str) -> dict[str, str]:
    block = re.search(r'<select[^>]*name="equipo"[^>]*>(.*?)</select>', html, re.S)
    if not block:
        return {}
    pairs = re.findall(r'<option[^>]*value="(\d+)"[^>]*>([^<]+)</option>', block.group(1))
    return {tid: html_mod.unescape(name).strip() for tid, name in pairs if tid != "0"}


_ATTR_KEYS = (
    "id", "nombre", "posicion", "equipo", "valor", "valor1", "valor2", "valor3",
    "valor7", "valor14", "valor30", "tendencia", "aceleracion",
    "diferencia1", "diferencia2", "diferencia3", "diferencia7", "diferencia14",
    "diferencia30", "diferencia-pct1", "diferencia-pct2", "diferencia-pct3",
    "diferencia-pct7", "diferencia-pct14", "diferencia-pct30",
)


def parse_market(html: str) -> list[dict[str, Any]]:
    team_map = parse_team_map(html)
    rows: list[dict[str, Any]] = []

    for chunk in html.split('class="elemento_jugador')[1:]:
        attrs: dict[str, str] = {}
        for key in _ATTR_KEYS:
            match = re.search(rf'data-{re.escape(key)}="([^"]*)"', chunk)
            if match:
                attrs[key] = match.group(1)
        if "id" not in attrs or "nombre" not in attrs:
            continue

        display = re.search(r'class="player-name"><span[^>]*>([^<]+)<', chunk)
        fixture = re.search(r'title="Jornada (\d+)[^"]*?Pr[^"]*?rival: ([^("]+)\(([^)]+)\)"', chunk)
        probability = re.search(r'class="prob-\d+"[^>]*>(\d+)%<', chunk)
        trend_icon = re.search(r'<i class="fas fa-[^"]*"\s+data-tooltip="([^"]+)"', chunk)
        streak = re.search(r'fa-caret-(up|down)[^>]*></i>\s*(\d+)<span', chunk)

        row: dict[str, Any] = {
            "ff_id": attrs["id"],
            "ff_name": html_mod.unescape(attrs["nombre"]),
            "display_name": html_mod.unescape(display.group(1)).strip() if display else None,
            "position": attrs.get("posicion"),
            "ff_team_id": attrs.get("equipo"),
            "ff_team": team_map.get(attrs.get("equipo", ""), None),
            "value": _num(attrs.get("valor")),
            "trend_score": _num(attrs.get("tendencia")),
            "acceleration": _num(attrs.get("aceleracion")),
            "next_week": int(fixture.group(1)) if fixture else None,
            "next_rival": html_mod.unescape(fixture.group(2)).strip() if fixture else None,
            "next_home": (fixture.group(3).strip().lower().startswith("cas")) if fixture else None,
            "start_probability": int(probability.group(1)) if probability else None,
            "trend_label": html_mod.unescape(trend_icon.group(1)) if trend_icon else None,
            "streak_days": int(streak.group(2)) if streak else None,
            "streak_dir": streak.group(1) if streak else None,
        }
        for window in (1, 2, 3, 7, 14, 30):
            row[f"value_{window}d_ago"] = _num(attrs.get(f"valor{window}"))
            row[f"delta_{window}d"] = _num(attrs.get(f"diferencia{window}"))
            row[f"pct_{window}d"] = _num(attrs.get(f"diferencia-pct{window}"))
        rows.append(row)

    return rows


def market(ttl: float = 2 * HOUR) -> list[dict[str, Any]]:
    return parse_market(_fetch(FF_MARKET_URL, ttl=ttl, tag="ff_market"))


def parse_detail(html: str) -> dict[str, Any]:
    def js_number(name: str) -> float | None:
        match = re.search(rf'\b{name}\s*=\s*(-?\d+(?:\.\d+)?)', html)
        return float(match.group(1)) if match else None

    def js_string(name: str) -> str | None:
        match = re.search(rf'\b{name}\s*=\s*"([^"]*)"', html)
        return match.group(1) if match else None

    def js_series(name: str) -> list[dict[str, Any]]:
        match = re.search(rf'\b{name}\s*=\s*(\[.*?\]);', html, re.S)
        if not match:
            return []
        pairs = re.findall(r'\{"date":"([^"]+)","value":(\d+)\}', match.group(1))
        return [{"date": d.replace("\\/", "/"), "value": int(v)} for d, v in pairs]

    ideal_bid = None
    call = re.search(r'parsePujaIdeal\(\s*(\d+)\s*\)', html)
    if call:
        ideal_bid = int(call.group(1))

    injuries = sorted({m for m in re.findall(r"xMin:\s*'([^']+)'", html)})

    history = js_series("player_chartjs") or js_series("player_chartjs_prev")
    return {
        "ideal_bid": ideal_bid or 0,
        "max_value": js_number("max_valor"),
        "min_value": js_number("min_valor"),
        "max_date": js_string("max_date"),
        "min_date": js_string("min_date"),
        "history": history,
        "prev_season_history": js_series("player_chartjs_prev"),
        "injury_marks": injuries,
    }


def player_detail(ff_id: str, ttl: float = 24 * HOUR) -> dict[str, Any]:
    url = FF_DETAIL_URL.format(ff_id=ff_id)
    return parse_detail(_fetch(url, ttl=ttl, tag="ff_detail"))


def _cell(chunk: str, css: str) -> str | None:
    match = re.search(rf'<td class="[^"]*\b{re.escape(css)}\b[^"]*">(.*?)</td>', chunk, re.S)
    if not match:
        return None
    text = re.sub(r"<[^>]+>", " ", match.group(1))
    return re.sub(r"\s+", " ", html_mod.unescape(text)).strip() or None


GAME_KEY = "laliga-fantasy"


def _game_points(row: str, game: str = GAME_KEY) -> float | None:
    """Points for one game inside the row's points cell.

    The cell carries one span per fantasy game (19 of them) and each span's class
    is a quality band plus the game key, e.g. `... very-high laliga-fantasy`. Only
    the span whose class contains the game key is that game's score; the visible
    `relevo` column is a different metric entirely.
    """
    cell = re.search(r'<td class="data points[^"]*">(.*?)</td>', row, re.S)
    if not cell:
        return None
    for match in re.finditer(r'<span class="([^"]*)">\s*([-\d.,]*)\s*</span>', cell.group(1), re.S):
        if game in match.group(1).split():
            return _num(match.group(2))
    return None


def parse_player_page(html: str) -> dict[str, Any]:
    name = re.search(r"<h1[^>]*>(.*?)</h1>", html, re.S)
    matches: list[dict[str, Any]] = []

    for chunk in re.split(r'<tr class="(?=plegado plegable)', html)[1:]:
        role = re.search(r'data-posicion-laliga-fantasy="([^"]*)"', chunk)
        if not role:
            continue
        # The main row ends at the first </tr>; what follows is its stats breakdown.
        row = chunk.split("</tr>")[0]
        week = re.search(r'jorn-td">\s*(\d+)', row)
        teams = re.findall(r'<img class="img"[^>]*alt="([^"]+)"', row)
        score = re.search(r'class="score (won|lost|draw)">([\d]+-[\d]+)<', row)
        minute_out = re.search(r'title="Salida"[^>]*>\s*(\d+)', row)
        matches.append({
            "week": int(week.group(1)) if week else None,
            "role": role.group(1),
            "played": role.group(1) != "NoConvocado",
            "home": 'data-local="1"' in chunk,
            "teams": teams[:2],
            "result": score.group(2) if score else None,
            "outcome": score.group(1) if score else None,
            "minute_out": int(minute_out.group(1)) if minute_out else None,
            "injured": "lesionado" in row.lower(),
            "points": _game_points(row),
            "sofascore": _num(_cell(row, "sofascore")),
        })

    played = [m for m in matches if m["played"] and m["points"] is not None]
    return {
        "name": re.sub(r"\s+", " ", re.sub(r"<[^>]+>", "", name.group(1))).strip() if name else None,
        "matches": matches,
        "games_played": len(played),
        "total_points": sum(m["points"] for m in played),
        "avg_points": (sum(m["points"] for m in played) / len(played)) if played else None,
    }


def player_page(slug: str, ttl: float = 12 * HOUR) -> dict[str, Any]:
    return parse_player_page(_fetch(FF_PLAYER_URL.format(slug=slug), ttl=ttl, tag="ff_player"))
