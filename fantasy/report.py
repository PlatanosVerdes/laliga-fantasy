"""Self-contained HTML report: decisions first, then the league's movements, then detail.

No external assets and no network at render time. Colours follow the reference
data-viz palette; the diverging pair used for value trends was validated in both
modes (worst adjacent CVD ΔE 21.6 light / 19.2 dark) and every coloured cue ships
with its own number or label, so nothing is carried by hue alone.
"""
from __future__ import annotations

import html
import json
import re
from pathlib import Path
from datetime import datetime
from typing import Any, Callable, Sequence

from . import crests, policies
from .config import POSITION_NAMES, POSITIONS, REPORT_FILE


ASSETS = Path(__file__).resolve().parent.parent / "assets"


def _asset(name: str) -> str:
    """Read a page asset from disk.

    The CSS and the JS used to be triple-quoted strings in this module: 1,500 lines of
    another language inside Python, with no editor help and no way for the Go port to
    reuse them. They are files now, read once at import, and both implementations serve
    the same bytes.
    """
    return (ASSETS / name).read_text(encoding="utf-8")


Column = tuple[str, Callable[[dict], Any], str]

DRAWER = _asset("drawer.html")


MODAL = _asset("modal.html")

VERDICTS = {
    "buy":      {"label": "Fichar",         "icon": "▲", "status": "good"},
    "clause":   {"label": "Clausulazo",     "icon": "◆", "status": "good"},
    "protect":  {"label": "Subir clausula", "icon": "!",  "status": "warning"},
    "sell":     {"label": "Vender",         "icon": "▼", "status": "serious"},
    "out":      {"label": "Baja",           "icon": "✕", "status": "critical"},
}
VERDICT_ORDER = ["out", "buy", "clause", "protect", "sell"]

# Position colours, chosen by the user. Each is a step of the reference palette, so
# every one sits in the lightness band and clears the chroma floor on its own. The
# four together do NOT clear the all-pairs CVD gate (yellow vs orange is the weak
# pair), which is why the chip always spells the position out: the hue is a faster
# read of a label that is already there, never the only carrier.
POSITION_COLOURS = {
    1: ("por", "#eb6834", "#d95926"),   # portero  - naranja
    2: ("def", "#4a3aa7", "#9085e9"),   # defensa  - lila
    3: ("med", "#1baf7a", "#199e70"),   # medio    - turquesa
    4: ("del", "#eda100", "#c98500"),   # delantero- amarillo
    5: ("ent", "#898781", "#898781"),   # entrenador
}

# The API reports status in English; the page is in Spanish.
STATUS_LABELS = {
    "injured": "lesionado",
    "doubtful": "duda",
    "sanctioned": "sancionado",
    "suspended": "sancionado",
    "out_of_league": "fuera de la liga",
    "unknown": "sin datos",
}


# --- formatting -------------------------------------------------------------

def _fmt_money(value: Any) -> str:
    if value in (None, ""):
        return "—"
    value = float(value)
    sign = "-" if value < 0 else ""
    value = abs(value)
    if value >= 1e6:
        return f"{sign}{value / 1e6:,.2f}M".replace(",", ".")
    if value >= 999_500:            # 999.999 rounded to "1.000K" instead of "1.00M"
        return f"{sign}{value / 1e6:,.2f}M".replace(",", ".")
    if value >= 1e3:
        return f"{sign}{value / 1e3:,.0f}K"
    return f"{sign}{value:,.0f}"


def _fmt_pct(value: Any) -> str:
    return "—" if value is None else f"{float(value):+.2f}%"


def _fmt_num(value: Any, digits: int = 2) -> str:
    return "—" if value is None else f"{float(value):.{digits}f}"


def _esc(value: Any) -> str:
    return html.escape("" if value is None else str(value))


# --- marks ------------------------------------------------------------------

def _diverging_bar(pct: Any, *, scale: float = 12.0) -> str:
    """Sign and magnitude of the projected value change, with the number beside it.

    Two poles off a neutral midpoint: blue rising, red falling. The label is always
    rendered, so the colour is a second reading of the same fact, never the only one.
    """
    if pct is None:
        return '<span class="bar-cell"><span class="bar-num">—</span></span>'
    value = float(pct)
    # Each arm owns half the track, so the fill can never spill over the label.
    width = min(50.0, abs(value) / scale * 50.0)
    side = "pos" if value >= 0 else "neg"
    label = _fmt_pct(value)
    tip = (f"Proyeccion a 7 dias: {label}"
           f"{' (al tope de la escala)' if abs(value) >= scale else ''}")
    return (
        f'<span class="bar-cell" title="{_esc(tip)}">'
        f'<span class="bar-num {side}">{label}</span>'
        f'<span class="bar-track" role="presentation">'
        f'<span class="bar-fill {side}" style="width:{width:.1f}%"></span>'
        "</span></span>"
    )


def _sparkline(series: Sequence[float] | None, *, width: int = 74, height: int = 20) -> str:
    """Value history. Omitted rather than faked when there is not enough of it."""
    if not series or len(series) < 5:
        return ""
    lo, hi = min(series), max(series)
    span = (hi - lo) or 1.0
    step = width / (len(series) - 1)
    points = " ".join(
        f"{index * step:.1f},{height - 2 - (value - lo) / span * (height - 4):.1f}"
        for index, value in enumerate(series))
    rising = series[-1] >= series[0]
    return (
        f'<svg class="spark" width="{width}" height="{height}" viewBox="0 0 {width} {height}" '
        f'aria-label="Historico de valor: {_fmt_money(series[0])} a {_fmt_money(series[-1])}">'
        f'<polyline points="{points}" fill="none" '
        f'stroke="var(--{"pole-pos" if rising else "pole-neg"})" stroke-width="2" '
        'stroke-linecap="round" stroke-linejoin="round"/></svg>'
    )


def _badge(verdict: str) -> str:
    spec = VERDICTS[verdict]
    return (f'<span class="badge-{spec["status"]}"><span class="badge-icon" aria-hidden="true">'
            f'{spec["icon"]}</span>{spec["label"]}</span>')


def _magnitude_bar(value: Any, *, scale: float = 1.0, digits: int = 2) -> str:
    """Single-hue magnitude bar with its number. Used for points per million.

    One hue, one axis, thin mark anchored at zero — a sequential encoding rather
    than a categorical one, so it needs no CVD pairing.
    """
    if value is None:
        return '<span class="bar-cell"><span class="bar-num">—</span></span>'
    amount = max(0.0, float(value))
    width = min(100.0, amount / scale * 100.0)
    return (
        f'<span class="bar-cell" title="{_esc(f"{amount:.3f} puntos esperados por millon")}">'
        f'<span class="bar-num">{amount:.{digits}f}</span>'
        f'<span class="mag-track"><span class="mag-fill" style="width:{width:.1f}%"></span></span>'
        "</span>")


def _raid_button(row: dict) -> str:
    """Schedule the raid from the row that told you the clause is coming.

    The whole point is arming it *before* the lock lifts, so the button has to live
    in the table that shows the countdown.
    """
    if row.get("is_mine") or not row.get("owner"):
        return "—"
    if row.get("shielded"):
        return '<span class="pill-critical">blindado</span>'
    clause = float(row.get("clause") or 0)
    suggested = int(row.get("max_pay") or (clause * 1.2 if clause else row["value"] * 1.5))
    label = "Reprogramar" if row.get("raid_scheduled") else "Programar"
    return (f'<button class="raid-btn" type="button" '
            f'data-raid="{_esc(row.get("id"))}" data-raid-name="{_esc(row.get("name"))}" '
            f'data-raid-max="{suggested}" '
            f'data-raid-clause="{int(clause)}">{label}</button>')


def _offer_buttons(row: dict) -> str:
    if not row.get("offer_id"):
        return "—"
    common = (f'data-op-market="{_esc(row.get("market_id"))}" '
              f'data-op-offer="{_esc(row.get("offer_id"))}" '
              f'data-op-player="{_esc(row.get("id"))}" '
              f'data-op-name="{_esc(row.get("name"))}" '
              f'data-op-amount="{int(row.get("offer_amount") or 0)}"')
    return (f'<button class="op bid" data-op="accept_offer" {common} type="button">Aceptar'
            f'</button> <button class="op danger" data-op="decline_offer" {common} '
            f'type="button">Rechazar</button>')


def _bid_button(row: dict) -> str:
    """Only rendered where a bid is actually possible; the server still re-validates."""
    listing = row.get("market") or {}
    market_id = listing.get("market_id")
    if not market_id:
        return "—"
    return (f'<button class="bid" type="button" data-market="{_esc(market_id)}" '
            f'data-player="{_esc(row.get("id"))}" data-name="{_esc(row.get("name"))}" '
            f'data-min="{int(listing.get("min_bid") or 0)}" '
            f'data-ideal="{int(row.get("ideal_bid") or 0)}" '
            f'data-value="{int(row.get("value") or 0)}"'
            f'{f" data-bid={listing['my_bid_id']}" if listing.get("my_bid_id") else ""}>'
            f'{"Mi puja" if listing.get("my_bid_id") else "Pujar"}</button>')


def _star(row: dict) -> str:
    """Favourite toggle. Interactive when served; a static file just shows state."""
    on = bool(row.get("starred"))
    return (f'<button class="star{" on" if on else ""}" data-player="{_esc(row.get("id"))}" '
            f'data-name="{_esc(row.get("name"))}" type="button" '
            f'aria-pressed="{"true" if on else "false"}" '
            f'title="{"Quitar de favoritos" if on else "Marcar como favorito"}">'
            f'{"★" if on else "☆"}</button>')


def _ratio_badge(ratio: Any, *, selling: bool = False) -> str:
    """Price against market value, named as well as coloured.

    The same multiple means opposite things depending on which side you are on:
    paying 1.30x is expensive, being *paid* 1.30x is a gift. `selling` flips it.
    """
    if ratio is None:
        return "—"
    value = float(ratio)
    if selling:
        if value >= 1.15:
            status, label = "good", "te pagan de mas"
        elif value >= 1.02:
            status, label = "good", "buen precio"
        elif value >= 0.98:
            status, label = "neutral", "a valor"
        elif value >= 0.9:
            status, label = "warning", "por debajo"
        else:
            status, label = "critical", "te lowballean"
        return (f'<span class="pill-{status}">{value:.2f}x</span>'
                f'<span class="pill-note">{label}</span>')
    if value <= 0.98:
        status, label = "good", "chollo"
    elif value <= 1.06:
        status, label = "neutral", "a valor"
    elif value <= 1.3:
        status, label = "warning", "algo caro"
    else:
        status, label = "critical", "muy caro"
    return (f'<span class="pill-{status}">{value:.2f}x</span>'
            f'<span class="pill-note">{label}</span>')


POWER_STATUS = {"holgado": "good", "normal": "neutral", "justo": "critical"}
RAID_STATUS = {"chollo": "good", "renta": "good", "justo": "warning", "caro": "critical",
               "no te llega": "critical", "sin datos": "neutral", "sin referencia": "neutral"}


def _verdict_badge(row: Any) -> str:
    """Whether paying this clause beats what you already own."""
    if not isinstance(row, dict) or not row.get("verdict"):
        return "—"
    verdict = row["verdict"]
    ratio = row.get("vs_market")
    note = f"{ratio:.1f}x tu plantilla" if ratio else ""
    return (f'<span class="pill-{RAID_STATUS.get(verdict, "neutral")}">{_esc(verdict)}</span>'
            + (f'<span class="pill-note">{_esc(note)}</span>' if note else ""))


def _power_badge(row: Any) -> str:
    """Who can actually buy right now. Named, not just coloured."""
    if not isinstance(row, dict) or not row.get("power"):
        return "—"
    status = POWER_STATUS.get(row["power"], "neutral")
    return (f'<span class="pill-{status}">{_esc(row["power"])}</span>'
            f'<span class="pill-note">{_esc(row.get("power_note"))}</span>')


def _countdown(row: Any) -> str:
    """Live countdown: the server renders a first value and stamps the deadline, so
    the browser keeps ticking it every second without asking again."""
    if not isinstance(row, dict):
        return "—"
    hours = row.get("hours_left")
    deadline = row.get("unlock_at") or row.get("expires")
    if hours is None:
        return "—"
    hours = float(hours)
    stamp = f' data-deadline="{_esc(deadline)}"' if deadline else ""
    if hours <= 0:
        return f'<span class="pill-critical"{stamp}>ya</span>'
    if hours < 24:
        status, text = "critical", f"{hours:.0f}h"
    elif hours < 72:
        status, text = "warning", f"{hours / 24:.1f}d"
    else:
        status, text = "neutral", f"{hours / 24:.0f}d"
    return f'<span class="pill-{status}"{stamp}>{text}</span>'


# --- tables -----------------------------------------------------------------

def _cell(value: Any, kind: str) -> tuple[str, str]:
    """Return (inner HTML, sort key)."""
    if kind == "money":
        return _esc(_fmt_money(value)), str(float(value or 0))
    if kind == "ideal":
        if not value:
            return '<span class="pill-warning">sin margen</span>', "0"
        return _esc(_fmt_money(value)), str(float(value))
    if kind == "pct":
        return _diverging_bar(value), str(float(value or 0))
    if kind == "num":
        return _esc(_fmt_num(value)), str(float(value or 0))
    if kind == "int":
        return ("—" if value is None else f"{int(value)}"), str(float(value or 0))
    if kind == "starts":
        if value is None:
            return "—", "-1"
        share = int(value)
        status = ("good" if share >= 75 else "warning" if share >= 50
                  else "serious" if share >= 30 else "critical")
        return f'<span class="pill-{status}">{share}%</span>', str(share)
    if kind == "pct_plain":
        return _esc(_fmt_pct(value)), str(float(value or 0))
    if kind == "list":
        return _esc(", ".join(value)) if value else "—", ""
    if kind == "verdict":
        return _badge(value), str(VERDICT_ORDER.index(value))
    if kind == "spark":
        return _sparkline(value), str(len(value or []))
    if kind == "player":
        return _player_cell(value), _esc(value.get("name"))
    if kind == "mag":
        return _magnitude_bar(value), str(float(value or 0))
    if kind == "ratio":
        return _ratio_badge(value), str(float(value or 0))
    if kind == "ratio_sell":
        return _ratio_badge(value, selling=True), str(float(value or 0))
    if kind == "verdict_raid":
        return _verdict_badge(value), str(-(value.get("ppm_at_clause") or 0))
    if kind == "status":
        label, status = value
        return (f'<span class="pill-{status or "neutral"}">'
                f'{_esc(str(label).replace("_", " "))}</span>'), str(label)
    if kind == "power":
        order = {"justo": "0", "normal": "1", "holgado": "2"}
        return _power_badge(value), order.get((value or {}).get("power"), "1")
    if kind == "hours":
        return _countdown(value), str(float((value or {}).get("hours_left") or 1e9))
    if kind == "star":
        return _star(value), ("0" if value.get("starred") else "1")
    if kind == "raid":
        return _raid_button(value), str(float((value.get("clause") or 0)))
    if kind == "offer":
        return _offer_buttons(value), str(float(value.get("offer_amount") or 0))
    if kind == "bid":
        return _bid_button(value), str(int((value.get("market") or {}).get("min_bid") or 0))
    return (_esc(value) if value not in (None, "") else "—"), _esc(value or "")


CRESTS: dict[str, str] = {}


# Sections where every row is yours by definition: repeating "mio" there is noise.
ALL_MINE = {"plantilla", "ventas", "ofertas", "misventas", "vencimientos", "riesgo",
            "siempre"}
_current_section = ""


def _player_cell(row: dict) -> str:
    flags = []
    team_id = str(row.get("team_id"))
    badge = (f'<span class="crest crest-{_esc(team_id)}" role="img" '
             f'aria-label="{_esc(row.get("team"))}" title="{_esc(row.get("team"))}"></span>'
             if team_id in CRESTS else "")
    slug = POSITION_COLOURS.get(int(row.get("position_id") or 0), ("ent",))[0]
    if not row.get("available"):
        status = row.get("status")
        flags.append(f'<span class="flag-critical">'
                     f'{_esc(STATUS_LABELS.get(status, status))}</span>')
    elif row.get("status") == "doubtful":
        flags.append('<span class="flag-warning">duda</span>')
    if row.get("prior_based"):
        flags.append('<span class="flag-muted" title="Sin historico: estimado por precio">est.</span>')
    if row.get("is_mine") and _current_section not in ALL_MINE:
        flags.append('<span class="flag-mine">mio</span>')
    return (f'<span class="p-cell">{badge}'
            f'<button class="p-name" type="button" data-detail="{_esc(row.get("id"))}">'
            f'{_esc(row.get("name"))}</button>'
            f'<span class="pos pos-{slug}">{_esc(row.get("position"))}</span>'
            f'<span class="p-meta">{_esc(row.get("team_short") or row.get("team"))}</span>'
            + "".join(flags) + "</span>")


NUMERIC_KINDS = {"money", "num", "int", "pct", "pct_plain", "mag", "ideal", "starts"}


class _in_section:
    """Marks which section a table is being built for, so row flags can adapt."""

    def __init__(self, name: str):
        self.name = name

    def __enter__(self):
        global _current_section
        self.previous = _current_section
        _current_section = self.name
        return self

    def __exit__(self, *exc):
        global _current_section
        _current_section = self.previous
        return False


def _table(columns: Sequence[Column], rows: Sequence[dict], *,
           empty: str = "Sin datos", filterable: bool = False,
           section: str = "") -> str:
    if section:
        with _in_section(section):
            return _table(columns, rows, empty=empty, filterable=filterable)
    if not rows:
        return f'<p class="empty">{_esc(empty)}</p>'
    # A column nobody has data for is noise: drop it rather than ship an empty strip.
    columns = [column for column in columns
               if column[2] != "spark" or any(len(row.get("value_history") or []) >= 5
                                              for row in rows)]
    head = "".join(
        f'<th data-kind="{kind}"'
        f'{" class=\"right\"" if kind in NUMERIC_KINDS else ""}>{_esc(header)}</th>'
        for header, _, kind in columns)
    body = []
    for row in rows:
        cells = []
        for _, accessor, kind in columns:
            inner, sort_key = _cell(accessor(row), kind)
            cls = "num" if kind in NUMERIC_KINDS and kind != "pct" else ""
            if kind == "pct":
                cls = "bar"
            cells.append(f'<td{f" class=\"{cls}\"" if cls else ""} '
                         f'data-sort="{_esc(sort_key)}">{inner}</td>')
        classes = ' class="row-me"' if row.get("is_me") else ""
        attrs = (f' data-position="{_esc(row.get("position"))}"'
                 f' data-price="{float(row.get("entry_cost") or row.get("value") or 0):.0f}"'
                 f' data-name="{_esc((row.get("name") or "").lower())}"') if filterable else ""
        body.append(f"<tr{classes}{attrs}>{''.join(cells)}</tr>")
    return ('<div class="table-wrap"><table class="sortable"><thead><tr>' + head
            + "</tr></thead><tbody>" + "".join(body) + "</tbody></table></div>")


def _player_columns(*, cost_label: str | None = None,
                    extra: Sequence[Column] = ()) -> list[Column]:
    columns: list[Column] = [
        ("★", lambda r: r, "star"),
        ("Jugador", lambda r: r, "player"),
        ("Valor", lambda r: r["value"], "money"),
    ]
    if cost_label:
        columns.append((cost_label, lambda r: r.get("entry_cost"), "money"))
    columns += [
        ("xPts/j", lambda r: r["xpts"], "num"),
        ("Pts/M", lambda r: r["points_value"], "mag"),
        ("Titular", lambda r: r.get("start_probability"), "starts"),
        ("Valor 7d", lambda r: r.get("projected_pct"), "pct"),
        ("Historico", lambda r: r.get("value_history"), "spark"),
        ("Proximo rival", lambda r: (f"{r.get('next_rival')} "
                                     f"({'casa' if r.get('next_home') else 'fuera'})")
                                    if r.get("next_rival") else None, "text"),
        ("Pts 25/26", lambda r: r.get("last_season_points"), "int"),
        ("Score", lambda r: r.get("score"), "num"),
    ]
    columns.extend(extra)
    return columns


def _kpi(label: str, value: str, hint: str = "", *, rank: str = "",
         meter: float | None = None, status: str = "", tab: str = "") -> str:
    """A widget. `meter` (0..1) draws where you sit in the league for that number,
    because a figure like "79.76M" only means something next to the other twelve."""
    parts = [f'<span class="kpi-label">{_esc(label)}</span>',
             f'<span class="kpi-value">{_esc(value)}</span>']
    if rank:
        parts.append(f'<span class="kpi-rank pill-{status or "neutral"}">{_esc(rank)}</span>')
    if meter is not None:
        width = max(2.0, min(100.0, meter * 100))
        parts.append('<span class="kpi-meter" role="presentation">'
                     f'<span class="kpi-meter-fill" style="width:{width:.0f}%"></span></span>')
    if hint:
        parts.append(f'<span class="kpi-hint">{_esc(hint)}</span>')
    if tab:
        # A widget that states a fact should take you to where the fact is explained.
        return (f'<button class="kpi kpi-link" type="button" data-goto="{_esc(tab)}">'
                f'{"".join(parts)}</button>')
    return f'<div class="kpi">{"".join(parts)}</div>'


def _rank_of(value: float, others: list[float]) -> tuple[str, float, str]:
    """Where a number sits among the league's: (label, 0..1 position, status)."""
    ordered = sorted(others, reverse=True)
    position = sum(1 for other in ordered if other > value) + 1
    total = len(ordered)
    if not total:
        return "", 0.0, ""
    share = 1 - (position - 1) / max(1, total - 1)
    status = "good" if position <= max(1, total // 3) else \
             "critical" if position > total - max(1, total // 3) else "neutral"
    return f"{position}\u00ba de {total}", share, status


def _section(title: str, body: str, *, note: str = "", badge: str = "",
             anchor: str = "") -> str:
    # body is already rendered by the time we get here, so the flag suppression is
    # driven by _in_section() around the table build instead.
    badge_html = f'<span class="badge-count">{_esc(badge)}</span>' if badge else ""
    note_html = f'<p class="note">{note}</p>' if note else ""
    ident = f' id="{_esc(anchor)}"' if anchor else ""
    return (f"<section{ident}><h2>{_esc(title)}{badge_html}</h2>{note_html}{body}</section>")


CSS = _asset("report.css")

JS = _asset("report.js")

FILTERS = _asset("filters.html")


# --- sections ---------------------------------------------------------------

def _actions_section(advice: dict[str, Any]) -> str:
    rows: list[dict] = []
    for player in advice["squad"]:
        if not player["available"]:
            status = STATUS_LABELS.get(player["status"], player["status"])
            rows.append({**player, "verdict": "out", "entry_cost": None,
                         "why": f"esta {status}: no puntua"})
    # Being in the market makes a player biddable, not worth bidding on. Only rows
    # whose own numbers back the call get the green badge — otherwise the badge would
    # contradict the reason printed beside it.
    for player in (advice["bids_now"] + advice["asks"]):
        if not player.get("affordable"):
            continue
        ideal = player.get("ideal_bid")
        cost = player["entry_cost"]
        has_ff_verdict = "ideal_bid" in player
        profitable = bool(ideal and cost <= ideal)
        if profitable:
            why = f"puja hasta {_fmt_money(ideal)}"
        elif has_ff_verdict and not ideal:
            continue    # futbolfantasy says there is no margin: not a recommendation
        elif has_ff_verdict:
            continue    # the asking price is above its profitable ceiling
        elif player["score"] > 1.0:
            why = "buen score y entra en tu presupuesto"
        else:
            continue
        if player.get("seller"):
            why += f" · lo vende {player['seller']}"
        if player.get("position_gap"):
            why += f" · te falta un {POSITION_NAMES[player['position_id']].lower()}"
        rows.append({**player, "verdict": "buy", "why": why})
        if len([r for r in rows if r["verdict"] == "buy"]) >= 8:
            break
    for player in advice["raids"][:6]:
        premium = player.get("clause_premium")
        why = f"de {player.get('owner')}"
        if premium:
            why += f", cláusula a {premium:.2f}x su valor"
        rows.append({**player, "verdict": "clause", "why": why})
    for player in advice.get("upcoming_raids", [])[:6]:
        hours = player.get("hours_left") or 0
        why = (f"cláusula de {player.get('owner')} se abre en "
               f"{hours:.0f}h ({_fmt_money(player.get('clause'))})")
        if not player.get("affordable"):
            why += " · no te llega el saldo"
        rows.append({**player, "verdict": "clause", "why": why})
    for player in advice["exposure"][:6]:
        threats = player.get("threats") or 0
        why = (f"{threats} rival{'es' if threats != 1 else ''} pueden pagarla"
               if threats else f"cláusula a solo {player.get('clause_margin', 0):.2f}x su valor")
        if player.get("top_threat"):
            why += f" · el mas rico: {player['top_threat']}"
        rows.append({**player, "verdict": "protect", "entry_cost": player.get("clause"),
                     "why": why})
    for player in advice.get("my_clauses_soon", [])[:6]:
        hours = player.get("hours_left") or 0
        rows.append({**player, "verdict": "protect", "entry_cost": player.get("clause"),
                     "why": f"su cláusula se desbloquea en {hours:.0f}h: quedas expuesto"})
    for player in advice["sells"][:6]:
        if player["available"] and player.get("reasons"):
            rows.append({**player, "verdict": "sell", "entry_cost": None,
                         "why": "; ".join(player["reasons"])})

    rows.sort(key=lambda r: VERDICT_ORDER.index(r["verdict"]))
    columns: list[Column] = [
        ("Que hacer", lambda r: r["verdict"], "verdict"),
        ("★", lambda r: r, "star"),
        ("Jugador", lambda r: r, "player"),
        ("Motivo", lambda r: r.get("why"), "text"),
        ("Coste", lambda r: r.get("entry_cost"), "money"),
        ("Valor", lambda r: r["value"], "money"),
        ("xPts/j", lambda r: r["xpts"], "num"),
        ("Pts/M", lambda r: r["points_value"], "mag"),
        ("Valor 7d", lambda r: r.get("projected_pct"), "pct"),
    ]
    legend = (
        '<div class="legend">'
        + "".join(f'<span><span class="swatch" style="background:var(--{spec["status"]})"></span>'
                  f'{spec["label"]}</span>' for key, spec in VERDICTS.items())
        + '<span><span class="swatch" style="background:var(--pole-pos)"></span>valor subiendo</span>'
        + '<span><span class="swatch" style="background:var(--pole-neg)"></span>valor bajando</span>'
        "</div>")
    buys = [r for r in rows if r["verdict"] == "buy"]
    if not buys:
        legend += ('<p class="callout">Hoy <strong>ningun jugador del mercado sale rentable</strong> '
                   'a lo que piden: ni el mercado libre ni las ventas de rivales estan por debajo '
                   'del techo que calcula futbolfantasy. No pujar tambien es una decision.</p>')
    return _section(
        "Que hacer ahora", legend + _table(columns, rows, empty="Nada urgente"),
        note="Todo lo accionable en una tabla, de lo urgente a lo que puede esperar. "
             "Cada fila lleva el motivo escrito: el color repite el dato, no lo sustituye.",
        badge=f"{len(rows)} decisiones", anchor="acciones")


def _activity_section(events: Sequence[dict], *, threshold: float = 1e6) -> str:
    if not events:
        return _section(
            "Movimientos de la liga",
            '<p class="empty">Sin movimientos todavia. Si la liga ya tiene actividad y esto '
            'sigue vacio, la respuesta del API ha cambiado de forma: <code>probe activity</code> '
            'la vuelca cruda.</p>',
            anchor="movimientos")

    # Lineup changes are the bulk of the log and say nothing about the market.
    moves = [e for e in events if "alinea" not in (e.get("kind") or "")]
    with_amount = [e for e in moves if e.get("amount")]
    big = sorted(with_amount, key=lambda e: -(e.get("amount") or 0))[:12]

    def row(event: dict) -> str:
        player = event.get("player")
        buyer, seller = event.get("buyer"), event.get("seller")
        if player and buyer and seller:
            body = f'<strong>{_esc(player)}</strong>: {_esc(seller)} &rarr; {_esc(buyer)}'
        elif player and buyer:
            body = f'<strong>{_esc(player)}</strong> &rarr; {_esc(buyer)}'
        elif player and seller:
            body = f'<strong>{_esc(player)}</strong>, vendido por {_esc(seller)}'
        elif player:
            body = f'<strong>{_esc(player)}</strong>'
        else:
            body = _esc(buyer or seller
                        or json.dumps(event.get("raw"), ensure_ascii=False)[:110])
        amount = _fmt_money(event["amount"]) if event.get("amount") else "—"
        # The amount alone does not say whether it was a steal or a panic buy; the
        # value on that same day does.
        extra = ""
        if event.get("value_then"):
            premium = event.get("premium") or 1.0
            status = ("critical" if premium >= 1.25 else
                      "warning" if premium >= 1.08 else
                      "good" if premium <= 0.98 else "neutral")
            extra = (f'<span class="feed-then">valia '
                     f'<b>{_esc(_fmt_money(event["value_then"]))}</b></span>'
                     f'<span class="pill-{status}">{premium:.2f}x</span>')
        return (f'<div class="feed-row"><span class="feed-date">'
                f'{_esc(event.get("date", "").replace("T", " ")[:16])}</span>'
                f'<span class="feed-kind">{_esc(event.get("kind"))}</span>'
                f'<span class="feed-body">{body}</span>'
                f'<span class="feed-amount">{_esc(amount)}</span>{extra}</div>')

    blocks = ""
    # Two lists only earn their space once the log is long enough for them to differ.
    if len(moves) > 8 and big:
        blocks += ('<h3 class="kpi-label">Operaciones mas grandes</h3>'
                   f'<div class="feed">{"".join(row(e) for e in big)}</div>'
                   '<h3 class="kpi-label" style="margin-top:20px">Lo ultimo</h3>')
    blocks += f'<div class="feed">{"".join(row(e) for e in moves[:20])}</div>'
    if not moves:
        blocks = ('<p class="empty">Hay actividad en la liga, pero solo cambios de alineacion: '
                  'ninguna compra ni venta todavia.</p>')
    return _section("Movimientos de la liga", blocks,
                    note="Quien ha fichado y vendido, y por cuanto. Las operaciones grandes "
                         "cuentan quien se esta quedando sin caja.",
                    badge=f"{len(moves)} operaciones", anchor="movimientos")


def build(universe: dict[str, Any], advice: dict[str, Any] | None, *,
          context: dict[str, Any] | None = None,
          activity: Sequence[dict] | None = None) -> str:
    context = context or {}
    CRESTS.update(crests.data_uris(universe.get("teams") or []))
    scheduled_raids = {k: v for k, v in policies.load().items() if v.get("raid")}
    for row in universe.get("players") or []:
        entry = scheduled_raids.get(str(row.get("id")))
        row["raid_scheduled"] = bool(entry)
        if entry:
            row["max_pay"] = entry.get("max_pay")
    week = universe["week"]
    players = universe["players"]
    generated = datetime.now().strftime("%d/%m/%Y %H:%M")

    def _when(value: Any) -> str:
        try:
            moment = datetime.fromisoformat(str(value))
        except (TypeError, ValueError):
            return ""
        return f"{WEEKDAYS[moment.weekday()]} {moment.day} {MONTHS[moment.month]} " \
               f"{moment:%H:%M}"

    closes = _when(week.get("closingWeekDate"))
    next_opens = _when(universe.get("next_week_opens"))
    hint = ("cierra " + closes) if closes else ("en juego" if week.get("isLive") else "cerrada")
    kpis = [
        _kpi(f"Jornada {week.get('weekNumber')}",
             "en juego" if week.get("isLive") else "cerrada",
             (f"J{week.get('nextWeek')} desde {next_opens}" if next_opens else ""),
             rank=hint, status="neutral"),
    ]
    if advice:
        squad_value = sum(p["value"] for p in advice["squad"])
        best_eleven = sum(sorted((p["xpts"] for p in advice["squad"]), reverse=True)[:11])
        teams = list((universe.get("league_teams") or {}).values())
        me = (universe.get("league_teams") or {}).get(str(universe.get("my_team_id"))) or {}
        cash_rank, cash_share, cash_status = _rank_of(
            advice["budget"], [t.get("estimated_cash") or 0 for t in teams])
        value_rank, value_share, value_status = _rank_of(
            squad_value, [t.get("squad_value") or 0 for t in teams])
        points_rank, points_share, points_status = _rank_of(
            me.get("points") or 0, [t.get("points") or 0 for t in teams])
        good_offers = [o for o in advice.get("offers") or [] if o["worth_taking"]]
        kpis += [
            _kpi("Mi puesto", f"{me.get('position') or '?'}\u00ba",
                 f"{me.get('points') or 0} puntos", rank=points_rank,
                 meter=points_share, status=points_status, tab="liga"),
            _kpi("Mi saldo", _fmt_money(advice["budget"]),
                 _esc(me.get("power_note") or ""), rank=cash_rank, meter=cash_share,
                 status=cash_status, tab="liga"),
            _kpi("Valor de plantilla", _fmt_money(squad_value),
                 f"{len(advice['squad'])} jugadores", rank=value_rank, meter=value_share,
                 status=value_status, tab="plantilla"),
        ]
        if good_offers:
            kpis.append(_kpi(
                "Ofertas que interesan", str(len(good_offers)),
                ", ".join(o["name"] for o in good_offers[:3]),
                rank="cobra", status="good", tab="decidir"))
        kpis += [
            _kpi("xPts del mejor 11", _fmt_num(best_eleven, 1), "por jornada"),
            _kpi("Pujables ahora", str(len(advice["bids_now"])),
                 f"{len(advice['asks'])} mas en venta por rivales", tab="mercado"),
            _kpi("Cláusulas a tiro", str(len(advice["raids"])),
                 (f"bloqueadas hasta {str(advice.get('clauses_unlock_from'))[:10]}"
                  if not advice["raids"] and advice.get("clauses_locked")
                  else "desbloqueadas y pagables"), tab="clausulas"),
        ]
    else:
        kpis += [
            _kpi("Jugadores", str(len(players)),
                 f"{universe['matched_count']} con datos de futbolfantasy"),
            _kpi("Sesion", "sin liga", "solo datos publicos"),
        ]

    header = (
        '<header><h1>LaLiga Fantasy · panel de decisiones</h1>'
        f'<p>{generated}'
        + (f' · liga <strong>{_esc(context.get("league_name"))}</strong>'
           if context.get("league_name") else "")
        + f' · jornada {_esc(week.get("weekNumber"))}</p>'
        + '<span class="live"><span id="live-dot" class="live-off"></span>'
          '<span id="live-stamp">estatico</span></span>'
        + "</header>"
        f'<div class="kpis">{"".join(kpis)}</div>'
        + ('<div class="tabs" id="tabs" role="tablist"><button class="tab" role="tab" data-tab="decidir" aria-selected="false" type="button">Decidir</button><button class="tab" role="tab" data-tab="mercado" aria-selected="false" type="button">Mercado</button><button class="tab" role="tab" data-tab="clausulas" aria-selected="false" type="button">Cláusulas</button><button class="tab" role="tab" data-tab="plantilla" aria-selected="false" type="button">Plantilla</button><button class="tab" role="tab" data-tab="liga" aria-selected="false" type="button">Liga</button><button class="tab" role="tab" data-tab="ranking" aria-selected="false" type="button">Ranking</button></div>' if advice else "")
    )

    sections: list[str] = []

    if advice:
        sections.append(_actions_section(advice))
    if advice:
        plan = policies.plan(universe["players"])
        if plan:
            entries = policies.load()
            plan_columns: list[Column] = [
                ("Jugador", lambda r: r.get("name"), "text"),
                ("Accion", lambda r: r.get("action").replace("_", " "), "text"),
                ("Importe", lambda r: r.get("amount"), "money"),
                ("Precio minimo", lambda r: (entries.get(r["player_id"]) or {}).get("min_price"), "money"),
                ("Acepto desde", lambda r: (
                    _fmt_money((entries.get(r["player_id"]) or {}).get("accept_above"))
                    if (entries.get(r["player_id"]) or {}).get("accept_above")
                    else "no vendo solo"), "text"),
                ("Motivo", lambda r: r.get("why"), "text"),
                ("Resultado", lambda r: r.get("result") or "pendiente", "text"),
            ]
            pending_actions = [a for a in plan if a["action"] != "ninguna"]
            note = ("Instrucciones permanentes. El interruptor solo mantiene al jugador "
                    "en el mercado. Para que se venda solo hay dos formas, y ninguna es "
                    "automatica por defecto: marcar <strong>vender automaticamente</strong> "
                    "en su ficha, que acepta cualquier oferta que llegue a lo que pides, o "
                    "fijar un importe en «aceptar desde». Sin una de las dos, una oferta "
                    "buena solo <strong>avisa</strong> y decides tu.")
            if pending_actions:
                note += (" <strong>" + str(len(pending_actions)) + " accion(es) en cola</strong>, "
                         "se ejecutan en el proximo refresco (salvo <code>--read-only</code>).")
            sections.append(_section(
                "Siempre en mercado", _table(plan_columns, plan, section="siempre"),
                note=note, badge=f"{len(entries)}", anchor="siempre"))

    if advice:
        raids_scheduled = policies.raid_plan(universe["players"],
                                            cash=advice.get("budget") or 0)
        if raids_scheduled:
            RAID_STATUS = {"pagar_clausula": "good", "esperando": "neutral",
                           "cancelada": "warning", "bloqueada": "critical",
                           "sin_saldo": "warning", "ninguna": "neutral"}
            raid_columns: list[Column] = [
                ("Jugador", lambda r: r.get("name"), "text"),
                ("Dueño", lambda r: r.get("owner"), "text"),
                ("Cláusula", lambda r: r.get("clause"), "money"),
                ("Mi limite", lambda r: r.get("max_pay"), "money"),
                ("Estado", lambda r: (r.get("action"), RAID_STATUS.get(r.get("action"))),
                 "status"),
                ("Motivo", lambda r: r.get("why"), "text"),
            ]
            armed = [r for r in raids_scheduled if r["action"] == "pagar_clausula"]
            note = ("Clausulazos programados: se pagan solos en cuanto la cláusula se "
                    "libere, <strong>y solo si sigue por debajo del limite que fijaste</strong>. "
                    "Si el dueño la sube o blinda al jugador, se cancela en vez de pagar de mas.")
            if armed:
                note += (" <strong>" + str(len(armed)) + " listo(s) para ejecutar ahora.</strong>")
            sections.append(_section(
                "Clausulazos programados", _table(raid_columns, raids_scheduled),
                note=note, badge=f"{len(raids_scheduled)}", anchor="programados"))

    if advice and advice.get("offers"):
        offer_columns: list[Column] = [
            ("Jugador", lambda r: r, "player"),
            ("Valor", lambda r: r["value"], "money"),
            ("Pides", lambda r: r.get("ask"), "money"),
            ("Te ofrecen", lambda r: r.get("offer_amount"), "money"),
            ("Sobre su valor", lambda r: r.get("vs_value"), "ratio_sell"),
            ("Ofertas", lambda r: r.get("offer_count"), "int"),
            ("Caduca", lambda r: str(r.get("offer_expires") or "")[:16].replace("T", " "), "text"),
            ("xPts/j", lambda r: r["xpts"], "num"),
            ("", lambda r: r, "offer"),
        ]
        good = [o for o in advice["offers"] if o["worth_taking"]]
        note = ("Lo que te ofrecen por los jugadores que tienes en venta. "
                "<strong>Sobre su valor</strong> es lo que pagan comparado con lo que vale: "
                "por encima de 1.00x te estan pagando de mas.")
        if good:
            note += (" Ahora mismo interesan: <strong>"
                     + ", ".join(_esc(o["name"]) for o in good) + "</strong>.")
        sections.append(_section(
            "Ofertas que has recibido", _table(offer_columns, advice["offers"], section="ofertas"),
            note=note, badge=f"{len(advice['offers'])}", anchor="ofertas"))

    sections.append(_activity_section(activity or []))

    if advice:
        shape_bits = []
        for position_id, data in advice["shape"].items():
            state = ("<strong>falta</strong>" if data["gap"]
                     else ("sobra" if data["surplus"] else "ok"))
            shape_bits.append(f'{POSITION_NAMES[position_id]} {data["owned"]}/{data["ideal"]} '
                              f'({state})')

        sections.append(PITCH)
        sections.append(_section(
            "Mi plantilla", _table(_player_columns(), advice["squad"], section="plantilla"),
            note=" · ".join(shape_bits), anchor="plantilla"))

        bid_columns = _player_columns(cost_label="Puja minima")
        bid_columns.insert(4, ("Puja max. rentable", lambda r: r.get("ideal_bid"), "ideal"))
        bid_columns.append(("Pujas", lambda r: (r.get("market") or {}).get("bids"), "int"))
        bid_columns.append(("", lambda r: r, "bid"))
        expiry = ((advice["bids_now"][0].get("market") or {}).get("expires")
                  if advice["bids_now"] else None)
        sections.append(_section(
            "Pujar ahora · mercado libre",
            FILTERS + _table(bid_columns, advice["bids_now"], filterable=True,
                             empty="El mercado libre esta vacio ahora mismo"),
            note=("Los jugadores sin dueño que el juego saca hoy: son los unicos que puedes "
                  "pujar en este momento. <strong>Puja maxima rentable</strong> es el techo que "
                  "calcula futbolfantasy; por encima compras caro."
                  + (f" El mercado cierra <strong>{_esc(str(expiry)[:16].replace('T', ' '))}</strong>."
                     if expiry else "")),
            badge=f"{len(advice['bids_now'])}", anchor="fichajes"))

        ask_columns = _player_columns(cost_label="Pide")
        ask_columns.insert(2, ("Vende", lambda r: r.get("seller"), "text"))
        ask_columns.insert(5, ("Sobre valor", lambda r: r.get("ask_ratio"), "ratio"))
        ask_columns.append(("", lambda r: r, "bid"))
        sections.append(_section(
            "En venta por rivales",
            _table(ask_columns, advice["asks"], filterable=True,
                   empty="Nadie ha puesto a nadie en venta"),
            note="Lo que los demas han puesto en el mercado, con lo que piden comparado con el "
                 "valor real. Aqui es donde aparecen los precios de fantasia.",
            badge=f"{len(advice['asks'])}", anchor="enventa"))

        if advice.get("my_listings"):
            mine_columns: list[Column] = [
                ("Jugador", lambda r: r, "player"),
                ("Valor", lambda r: r["value"], "money"),
                ("Pides", lambda r: r.get("entry_cost"), "money"),
                ("Sobre valor", lambda r: r.get("ask_ratio"), "ratio"),
                ("xPts/j", lambda r: r["xpts"], "num"),
                ("Valor 7d", lambda r: r.get("projected_pct"), "pct"),
            ]
            under = [p for p in advice["my_listings"] if p.get("underpriced")]
            note = "Lo que tienes tu en venta ahora mismo."
            if under:
                note += (" <strong>Ojo:</strong> "
                         + ", ".join(_esc(p["name"]) for p in under)
                         + " esta por debajo de su valor de mercado.")
            sections.append(_section("Mis ventas en curso",
                                     _table(mine_columns, advice["my_listings"], section="misventas"),
                                     note=note, anchor="misventas"))

        if advice.get("watchlist"):
            sections.append(_section(
                "Seguimiento · libres sin listar",
                _table(_player_columns(), advice["watchlist"]),
                note="Buenos, sin dueño, pero <strong>no estan en el mercado</strong>: no se "
                     "pueden pujar hoy. Marcalos con la estrella y apareceran arriba en cuanto "
                     "salgan.", anchor="seguimiento"))

        raid_columns = _player_columns(cost_label="Cláusula")
        raid_columns.insert(1, ("Dueño", lambda r: r.get("owner"), "text"))
        raid_columns.insert(4, ("x valor", lambda r: r.get("clause_premium"), "num"))
        raid_columns.append(("Clausulazo", lambda r: r, "raid"))
        sections.append(_section(
            "Cláusulas pagables",
            _table(raid_columns, advice["raids"],
                   empty=(f"Ninguna: las {advice['clauses_locked']} cláusulas de la liga siguen "
                          f"bloqueadas hasta {str(advice.get('clauses_unlock_from'))[:10]}."
                          if advice.get("clauses_locked") else "Ninguna cláusula a tu alcance")),
            note="Jugadores de rivales con la cláusula desbloqueada y dentro de tu poder de compra.",
            badge=f"{len(advice['raids'])}", anchor="clausulas"))

        sell_columns = _player_columns(
            extra=[("Motivos", lambda r: r.get("reasons"), "list")])
        sections.append(_section(
            "Candidatos a vender", _table(sell_columns, advice["sells"], section="ventas"),
            note="Ordenados por presion de venta: score bajo, valor cayendo, poca titularidad "
                 "o exceso en la posicion.", anchor="ventas"))

        exposure_columns: list[Column] = [
            ("Jugador", lambda r: r, "player"),
            ("Valor", lambda r: r["value"], "money"),
            ("Cláusula", lambda r: r.get("clause"), "money"),
            ("x valor", lambda r: r.get("clause_margin"), "num"),
            ("Rivales que pueden", lambda r: r.get("threats"), "int"),
            ("El mas rico", lambda r: r.get("top_threat"), "text"),
            ("xPts/j", lambda r: r["xpts"], "num"),
            ("Score", lambda r: r["score"], "num"),
        ]
        sections.append(_section(
            "Riesgo de cláusula",
            _table(exposure_columns, advice["exposure"], empty="Sin exposicion relevante", section="riesgo"),
            note="Tus jugadores buenos con cláusula baja, contando cuantos rivales tienen caja "
                 "para pagarla ahora mismo.", anchor="riesgo"))

        clause_columns: list[Column] = [
            ("Se abre en", lambda r: r, "hours"),
            ("Fecha", lambda r: str(r.get("unlock_at") or "")[:16].replace("T", " "), "text"),
            ("Jugador", lambda r: r, "player"),
            ("Valor", lambda r: r["value"], "money"),
            ("Cláusula", lambda r: r.get("clause"), "money"),
            ("xPts/j", lambda r: r["xpts"], "num"),
            ("Score", lambda r: r["score"], "num"),
        ]
        mine_clauses = advice.get("my_clauses_soon") or []
        sections.append(_section(
            "Mis cláusulas que vencen",
            _table(clause_columns, mine_clauses, section="vencimientos",
                   empty="Ninguna se desbloquea en los proximos 10 dias."),
            note="Cuando el candado cae, cualquiera con caja suficiente puede pagarla. "
                 "Subir la cláusula antes de esa fecha es la unica defensa.",
            badge=f"{len(mine_clauses)}", anchor="vencimientos"))

        sections.append(_calendar_section(universe, advice))

        rival_clauses = advice.get("upcoming_raids") or []
        rival_clause_columns = list(clause_columns)
        rival_clause_columns.insert(3, ("Dueño", lambda r: r.get("owner"), "text"))
        rival_clause_columns.insert(0, ("¿Renta?", lambda r: r, "verdict_raid"))
        rival_clause_columns.append(("x valor", lambda r: r.get("clause_premium"), "num"))
        rival_clause_columns.append(("Pts/M pagando", lambda r: r.get("ppm_at_clause"), "mag"))
        rival_clause_columns.append(
            ("Techo futbolfantasy", lambda r: r.get("ideal_bid"), "ideal"))
        rival_clause_columns.append(("Clausulazo", lambda r: r, "raid"))
        sections.append(_section(
            "Cláusulas de rivales que se abren",
            _table(rival_clause_columns, rival_clauses,
                   empty="Ninguna cláusula interesante se abre en los proximos 10 dias."),
            note=("El otro lado del mismo reloj: en cuanto se abren, se pueden pagar. "
                  "<strong>¿Renta?</strong> compara los puntos por millon que sacas "
                  f"<em>pagando la cláusula</em> con la mediana de tu plantilla "
                  f"({advice.get('squad_ppm_benchmark', 0):.3f} pts/M): si es peor que lo que "
                  "ya tienes, sale <em>caro</em>. No lo comparo con el techo de "
                  "futbolfantasy porque el juego fija las cláusulas sobre 1.5x el valor, "
                  "asi que por ese criterio ninguna saldria rentable nunca — la columna "
                  "esta ahi para que veas el dato."),
            badge=f"{len(rival_clauses)}", anchor="oportunidades"))

        if advice.get("rivals"):
            rival_columns: list[Column] = [
                ("#", lambda r: r.get("cash_position"), "int"),
                ("Manager", lambda r: r.get("manager") or r.get("name"), "text"),
                ("Poder de compra", lambda r: r, "power"),
                ("Puntos", lambda r: r.get("points"), "int"),
                ("Jugadores", lambda r: r.get("players"), "int"),
                ("Valor plantilla", lambda r: r.get("squad_value"), "money"),
                ("Neto en fichajes", lambda r: r.get("net_flow"), "money"),
                ("Caja estimada", lambda r: r.get("estimated_cash"), "money"),
                ("Suma de cláusulas", lambda r: r.get("clause_total"), "money"),
            ]
            model = advice.get("cash_model") or {}
            note = ("El API solo publica el saldo de tu equipo, y la cifra de la clasificacion es "
                    "solo el valor de la plantilla. Asi que la caja se reconstruye del historial: "
                    "<strong>caja = base + ventas &minus; compras</strong>. ")
            if model.get("anchored"):
                note += (f"La base ({_fmt_money(model['base'])}) esta medida sobre tu propio saldo "
                         "real, asi que absorbe la caja inicial y tus recompensas de una vez. "
                         f"El error que queda es solo la diferencia de recompensas diarias entre "
                         f"managers, como maximo {_fmt_money(model['uncertainty'])}.")
            else:
                note += (f"Sin saldo propio que leer, asumo {_fmt_money(model.get('base'))} de "
                         "caja inicial para todos.")
            sections.append(_section("Poder de compra de la liga",
                                     _table(rival_columns, advice["rivals"]),
                                     note=note, anchor="rivales"))

    top = sorted((p for p in players if p["available"]),
                 key=lambda p: p["score"], reverse=True)[:80]
    sections.append(_section(
        "Ranking global", FILTERS + _table(_player_columns(), top, filterable=True),
        note="Los 80 mejores de LaLiga por score, con dueño o sin el. Filtra por posicion y "
             "precio, o pincha una cabecera para reordenar.",
        badge="top 80", anchor="ranking"))

    value = sorted((p for p in players if p["available"] and p["value"] > 0),
                   key=lambda p: p["points_value"], reverse=True)[:40]
    sections.append(_section(
        "Mejor rentabilidad", _table(_player_columns(), value),
        note="xPts esperados por jornada divididos entre el precio. La metrica que manda cuando "
             "vas justo de saldo.", anchor="rentabilidad"))

    footer = (
        "<footer>Datos: API oficial de LaLiga Fantasy y futbolfantasy.com. "
        "<code>xPts</code> es una estimacion propia: puntos por jornada de la temporada pasada "
        f"y de la actual (peso actual {universe['current_weight']:.0%}), ajustados por "
        "probabilidad de ser titular, dificultad del proximo rival y confianza del dato. "
        "<code>est.</code> marca a quien no tiene historico y se estima por precio. "
        "El barrido de valor a 7 dias es una proyeccion amortiguada, no una promesa. "
        "Herramienta de consulta: no ejecuta ninguna operacion.</footer>"
    )

    crest_css = "".join(f'.crest-{team_id}{{background-image:url({uri})}}'
                        for team_id, uri in sorted(CRESTS.items()))
    return (f'<title>Panel Fantasy</title><style>{CSS}{crest_css}</style>'
            f'<div class="wrap">{header}{"".join(sections)}{footer}</div>'
            + MODAL + DRAWER
            + f"<script>{JS}</script>")





MONTHS = ["", "ene", "feb", "mar", "abr", "may", "jun",
          "jul", "ago", "sep", "oct", "nov", "dic"]
WEEKDAYS = ["lun", "mar", "mie", "jue", "vie", "sab", "dom"]


def _calendar_section(universe: dict[str, Any], advice: dict[str, Any]) -> str:
    """Clause unlocks laid out by day.

    A table sorts; a calendar answers a different question — *when does the league
    open up* — and at the start of a season the answer is dramatic: everything on
    the same day. That is worth seeing as a shape rather than reading as 28 rows.
    """
    clauses = universe.get("clauses") or {}
    entries = (clauses.get("mine") or []) + (clauses.get("rivals") or [])
    if not entries:
        return _section("Calendario de clausulazos",
                        '<p class="empty">Sin cláusulas con fecha conocida.</p>',
                        anchor="calendario")

    spending = advice.get("spending_power") or 0
    by_day: dict[str, list[dict]] = {}
    for row in entries:
        day = str(row.get("unlock_at") or "")[:10]
        if day:
            by_day.setdefault(day, []).append(row)

    cards = []
    for day in sorted(by_day)[:8]:
        rows = sorted(by_day[day], key=lambda r: -r["score"])
        mine = [r for r in rows if r.get("is_mine")]
        targets = [r for r in rows if not r.get("is_mine")
                   and (r.get("clause") or 0) <= spending]
        try:
            date = datetime.fromisoformat(day)
            label = f"{WEEKDAYS[date.weekday()]} {date.day} {MONTHS[date.month]}"
        except ValueError:
            label = day
        chips = "".join(
            f'<button class="cal-chip{" cal-mine" if row.get("is_mine") else ""}'
            f'{" cal-armed" if row.get("raid_scheduled") else ""}" type="button"'
            + ("" if row.get("is_mine") else
               f' data-raid="{_esc(row.get("id"))}"'
               f' data-raid-name="{_esc(row["name"])}"'
               f' data-raid-max="{int(row.get("max_pay") or (row.get("clause") or 0) * 1.2)}"'
               f' data-raid-clause="{int(row.get("clause") or 0)}"')
            + f' data-detail-alt="{_esc(row.get("id"))}"'
            f' title="{"Tuyo: ese dia queda expuesto" if row.get("is_mine") else "Programar clausulazo"}">'
            f'<span class="crest crest-{_esc(row.get("team_id"))}"></span>'
            f'{_esc(row["name"])}'
            f'{"<span class=cal-armed-mark>armado</span>" if row.get("raid_scheduled") else ""}'
            f'<b>{_esc(_fmt_money(row.get("clause")))}</b></button>'
            for row in rows[:14])
        more = (f'<span class="cal-more">+{len(rows) - 14} mas</span>'
                if len(rows) > 14 else "")
        cards.append(
            f'<div class="cal-day"><div class="cal-head"><strong>{_esc(label)}</strong>'
            f'<span class="cal-count">{len(rows)}</span></div>'
            f'<div class="cal-meta">{len(mine)} tuyos · {len(targets)} a tu alcance</div>'
            f'<div class="cal-chips">{chips}{more}</div></div>')

    return _section(
        "Calendario de clausulazos",
        f'<div class="cal">{"".join(cards)}</div>',
        note="Cuando se abre cada cláusula. Los tuyos van marcados: ese dia quedas "
             "expuesto y a la vez puedes atacar. Al arrancar la temporada se abren todas "
             "de golpe, asi que el dia importa mas que la hora.",
        anchor="calendario")


PITCH = _asset("pitch.html")


SECTION_RE = re.compile(r'<section id="([a-z]+)">(.*?)</section>', re.S)


def split_sections(page: str) -> dict[str, str]:
    """Pull each `<section id=...>` out of a built page.

    The server re-renders the whole page (cheap, all in memory) and then serves the
    pieces, so there is exactly one renderer and the live page and the static file
    can never drift apart.
    """
    return {match.group(1): match.group(2) for match in SECTION_RE.finditer(page)}


def write(universe: dict[str, Any], advice: dict[str, Any] | None, *,
          context: dict[str, Any] | None = None, activity: Sequence[dict] | None = None,
          path=None):
    path = path or REPORT_FILE
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(build(universe, advice, context=context, activity=activity),
                    encoding="utf-8")
    return path


def dump_json(universe: dict[str, Any], advice: dict[str, Any] | None, path,
              activity: Sequence[dict] | None = None) -> None:
    payload = {
        "universe": {k: v for k, v in universe.items() if k != "teams"},
        "advice": advice,
        "activity": list(activity or []),
    }
    path.write_text(json.dumps(payload, indent=2, ensure_ascii=False, default=str),
                    encoding="utf-8")
