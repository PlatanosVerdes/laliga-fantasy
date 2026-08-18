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
from datetime import datetime
from typing import Any, Callable, Sequence

from . import crests, policies
from .config import POSITION_NAMES, POSITIONS, REPORT_FILE

Column = tuple[str, Callable[[dict], Any], str]

DRAWER = '''<div class="drawer" id="drawer" hidden role="dialog" aria-modal="true">
  <div class="drawer-panel">
    <button class="drawer-close" type="button" aria-label="Cerrar">&times;</button>
    <div class="drawer-body"><p class="empty">Cargando…</p></div>
  </div>
</div>'''


MODAL = '''<div class="modal" id="bid-modal" hidden role="dialog" aria-modal="true"
     aria-label="Confirmar operacion">
  <div class="modal-card">
    <h3><span class="bid-action">Pujar por</span> <span class="bid-who"></span></h3>
    <div id="bid-amount-step">
      <div class="bid-field">
        <label for="bid-amount">Importe de la puja</label>
        <input class="bid-amount" id="bid-amount" type="text" inputmode="numeric" autocomplete="off" spellcheck="false">
      </div>
      <p class="bid-refs">
        <span>Puja minima <b class="bid-min"></b></span>
        <span>Techo rentable <b class="bid-ideal"></b></span>
        <span>Valor <b class="bid-value"></b></span>
        <span class="bid-rivals-wrap">Pujas vigentes <b class="bid-rivals"></b></span>
      </p>
      <p class="bid-warn" hidden></p>
    </div>
    <div id="bid-summary-step" hidden>
      <div class="bid-summary"></div>
    </div>
    <p class="bid-error"></p>
    <div class="modal-actions">
      <button class="bid-drop" type="button" hidden>Retirar mi puja</button>
      <button class="bid-cancel" type="button">Cancelar</button>
      <button class="bid-next primary" type="button">Continuar</button>
      <button class="bid-confirm" type="button" hidden>Aceptar</button>
    </div>
    <p class="modal-note">Se comprueba contra tu saldo antes de ejecutar.</p>
  </div>
</div>'''

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


CSS = """
:root{
  --plane:#f9f9f7; --surface:#fcfcfb; --line:#e1e0d9; --baseline:#c3c2b7;
  --ink:#0b0b0b; --ink-2:#52514e; --muted:#898781;
  --pole-pos:#2a78d6; --pole-neg:#e34948; --mid:#f0efec;
  --good:#0ca30c; --warning:#fab219; --serious:#ec835a; --critical:#d03b3b;
  --accent:#2a78d6;
  /* blue ramp, ordinal-safe end first (light: no lighter than step 250) */
  --seq-2:#86b6ef; --seq-4:#2a78d6; --seq-6:#184f95;
  --pos-por:#eb6834; --pos-def:#4a3aa7; --pos-med:#1baf7a; --pos-del:#eda100;
  --pitch-green:#1f6b3f;
}
@media (prefers-color-scheme: dark){
  :root:not([data-theme="light"]){
    --plane:#0d0d0d; --surface:#1a1a19; --line:#2c2c2a; --baseline:#383835;
    --ink:#ffffff; --ink-2:#c3c2b7; --muted:#898781;
    --pole-pos:#3987e5; --pole-neg:#e66767; --mid:#383835;
    --accent:#3987e5;
    --seq-2:#184f95; --seq-4:#3987e5; --seq-6:#9ec5f4;
    --pos-por:#d95926; --pos-def:#9085e9; --pos-med:#199e70; --pos-del:#c98500;
    --pitch-green:#17492d;
  }
}
:root[data-theme="dark"]{
  --plane:#0d0d0d; --surface:#1a1a19; --line:#2c2c2a; --baseline:#383835;
  --ink:#ffffff; --ink-2:#c3c2b7; --muted:#898781;
  --pole-pos:#3987e5; --pole-neg:#e66767; --mid:#383835;
  --accent:#3987e5;
  --seq-2:#184f95; --seq-4:#3987e5; --seq-6:#9ec5f4;
  --pos-por:#d95926; --pos-def:#9085e9; --pos-med:#199e70; --pos-del:#c98500;
  --pitch-green:#17492d;
}
*{box-sizing:border-box}
body{margin:0;background:var(--plane);color:var(--ink);
  font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
  -webkit-font-smoothing:antialiased}
.wrap{max-width:1500px;margin:0 auto;padding:32px 20px 80px}
header h1{font-size:27px;margin:0 0 4px;letter-spacing:-.015em}
header p{margin:0;color:var(--ink-2);font-size:13px}
.tabs{display:flex;flex-wrap:wrap;gap:4px;margin:22px 0 0;border-bottom:1px solid var(--line);
  padding-bottom:0}
.tab{font:inherit;font-size:13px;font-weight:600;color:var(--muted);background:none;
  border:none;border-bottom:2px solid transparent;padding:9px 14px;cursor:pointer;
  margin-bottom:-1px}
.tab:hover{color:var(--ink)}
.tab.on{color:var(--ink);border-bottom-color:var(--accent)}
.kpi-rank{align-self:flex-start;margin-top:2px}
.kpi-rank.pill-neutral{background:none;color:var(--muted);padding-left:0;padding-right:0}
.kpi-meter{display:block;height:4px;background:var(--mid);border-radius:2px;margin-top:7px;
  overflow:hidden}
.kpi-meter-fill{display:block;height:4px;background:var(--seq-4);border-radius:2px}
.kpis{display:grid;grid-template-columns:repeat(auto-fit,minmax(165px,1fr));gap:12px;margin:20px 0 0}
.kpi{background:var(--surface);border:1px solid var(--line);border-radius:10px;padding:12px 14px;
  display:flex;flex-direction:column;gap:1px;text-align:left;font:inherit;color:inherit}
button.kpi-link{cursor:pointer}
button.kpi-link:hover{border-color:var(--accent)}
button.kpi-link::after{content:"›";position:absolute;top:10px;right:12px;color:var(--muted);
  font-size:15px}
button.kpi-link{position:relative}
.kpi-label{font-size:11px;text-transform:uppercase;letter-spacing:.07em;color:var(--muted)}
.kpi-value{font-size:21px;font-weight:600;font-variant-numeric:tabular-nums;letter-spacing:-.01em}
.kpi-hint{font-size:11px;color:var(--ink-2)}
section{margin:40px 0}
h2{font-size:17px;margin:0 0 6px;display:flex;align-items:center;gap:9px;letter-spacing:-.01em}
.badge-count{font-size:11px;font-weight:500;color:var(--ink-2);background:var(--mid);
  border-radius:99px;padding:2px 9px}
.note{color:var(--ink-2);font-size:13px;margin:0 0 14px;max-width:82ch}
.note strong{color:var(--ink);font-weight:600}
.filters{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin:0 0 12px}
.filters label{font-size:12px;color:var(--muted);display:flex;align-items:center;gap:5px}
.filters select,.filters input{font:inherit;font-size:13px;color:var(--ink);
  background:var(--surface);border:1px solid var(--line);border-radius:7px;padding:5px 8px}
.filters input[type=search]{min-width:180px}
.filters button{font:inherit;font-size:12px;color:var(--ink-2);background:var(--surface);
  border:1px solid var(--line);border-radius:7px;padding:5px 10px;cursor:pointer}
.filters button:hover{color:var(--ink)}
.table-wrap{overflow-x:auto;background:var(--surface);border:1px solid var(--line);
  border-radius:10px}
table{border-collapse:separate;border-spacing:0;width:100%;font-size:13px}
th,td{padding:9px 11px;text-align:left;white-space:nowrap;border-bottom:1px solid var(--line)}
tbody tr:last-child td{border-bottom:none}
th{position:sticky;top:0;z-index:1;background:var(--surface);cursor:pointer;font-size:11px;
  font-weight:600;text-transform:uppercase;letter-spacing:.05em;color:var(--muted);
  user-select:none;border-bottom:1px solid var(--baseline)}
th:hover{color:var(--ink)}
th.sorted-asc::after{content:" ↑";color:var(--accent)}
th.sorted-desc::after{content:" ↓";color:var(--accent)}
td.num{text-align:right;font-variant-numeric:tabular-nums}
tbody tr:hover{background:color-mix(in srgb,var(--accent) 7%,transparent)}
tbody tr.row-me{background:color-mix(in srgb,var(--accent) 10%,transparent);
  box-shadow:inset 3px 0 0 var(--accent)}
tbody tr.row-me td:nth-child(2){font-weight:700}
.p-cell{display:inline-flex;align-items:center;gap:7px}
.crest{width:17px;height:17px;flex:0 0 auto;display:inline-block;
  background-repeat:no-repeat;background-position:center;background-size:contain}
button.p-name{font:inherit;font-weight:600;color:var(--ink);background:none;border:none;
  padding:0;cursor:pointer;text-align:left;border-bottom:1px dotted var(--baseline)}
button.p-name:hover{color:var(--accent);border-bottom-color:var(--accent)}
.p-meta{color:var(--muted);font-size:11px}
.pos{font-size:10px;font-weight:700;letter-spacing:.05em;padding:2px 6px;border-radius:5px;
  text-transform:uppercase}
.pos-por{color:var(--pos-por);background:color-mix(in srgb,var(--pos-por) 15%,transparent)}
.pos-def{color:var(--pos-def);background:color-mix(in srgb,var(--pos-def) 15%,transparent)}
.pos-med{color:var(--pos-med);background:color-mix(in srgb,var(--pos-med) 16%,transparent)}
.pos-del{color:var(--pos-del);background:color-mix(in srgb,var(--pos-del) 18%,transparent)}
.pos-ent{color:var(--muted);background:var(--mid)}
.flag-critical,.flag-warning,.flag-muted{font-size:10px;margin-left:6px;padding:1px 5px;
  border-radius:4px;text-transform:uppercase;letter-spacing:.04em;font-weight:600}
.flag-critical{color:#fff;background:var(--critical)}
.flag-warning{color:#3a2c00;background:var(--warning)}
.flag-muted{color:var(--muted);border:1px solid var(--line)}
[class^="badge-"]:not(.badge-count){display:inline-flex;align-items:center;gap:5px;font-size:11px;
  font-weight:700;text-transform:uppercase;letter-spacing:.05em;padding:3px 8px;border-radius:6px}
.badge-icon{font-size:10px;line-height:1}
.badge-good{color:var(--good);background:color-mix(in srgb,var(--good) 14%,transparent);
  box-shadow:inset 0 0 0 1px color-mix(in srgb,var(--good) 40%,transparent)}
.badge-warning{color:var(--warning);background:color-mix(in srgb,var(--warning) 16%,transparent);
  box-shadow:inset 0 0 0 1px color-mix(in srgb,var(--warning) 45%,transparent)}
.badge-serious{color:var(--serious);background:color-mix(in srgb,var(--serious) 16%,transparent);
  box-shadow:inset 0 0 0 1px color-mix(in srgb,var(--serious) 45%,transparent)}
.badge-critical{color:var(--critical);background:color-mix(in srgb,var(--critical) 14%,transparent);
  box-shadow:inset 0 0 0 1px color-mix(in srgb,var(--critical) 42%,transparent)}
/* Sequential single hue for magnitude (points per million). The step nearest the
   surface still clears 2:1, per the ordinal rule. */
.mag-track{position:relative;width:64px;height:8px;background:var(--mid);border-radius:2px;
  flex:0 0 auto;display:block;overflow:hidden}
.mag-fill{display:block;height:8px;background:var(--seq-4);border-radius:0 2px 2px 0}
[class^="pill-"]{display:inline-block;font-size:11px;font-weight:700;padding:2px 7px;
  border-radius:5px;font-variant-numeric:tabular-nums}
.pill-good{color:var(--good);background:color-mix(in srgb,var(--good) 15%,transparent)}
.pill-warning{color:var(--warning);background:color-mix(in srgb,var(--warning) 18%,transparent)}
.pill-critical{color:var(--critical);background:color-mix(in srgb,var(--critical) 15%,transparent)}
.pill-neutral{color:var(--ink-2);background:var(--mid)}
.pill-serious{color:var(--serious);background:color-mix(in srgb,var(--serious) 16%,transparent)}
.pill-note{font-size:10px;color:var(--muted);margin-left:6px;text-transform:uppercase;
  letter-spacing:.04em}
.star{background:none;border:none;cursor:pointer;font-size:15px;line-height:1;padding:0 2px;
  color:var(--muted)}
.star:hover{color:var(--warning)}
.star.on{color:var(--warning)}
.flag-mine{font-size:10px;margin-left:6px;padding:1px 5px;border-radius:4px;font-weight:600;
  text-transform:uppercase;letter-spacing:.04em;color:var(--seq-6);
  background:color-mix(in srgb,var(--accent) 14%,transparent)}
td.bar{min-width:150px}
.bar-cell{display:flex;align-items:center;gap:9px;justify-content:flex-end}
.bar-track{position:relative;width:78px;height:8px;background:var(--mid);border-radius:2px;
  flex:0 0 auto;display:block}
.bar-track::after{content:"";position:absolute;left:50%;top:-2px;width:1px;height:12px;
  background:var(--baseline)}
.bar-fill{position:absolute;top:0;height:8px;display:block}
.bar-fill.pos{left:50%;background:var(--pole-pos);border-radius:0 2px 2px 0}
.bar-fill.neg{right:50%;background:var(--pole-neg);border-radius:2px 0 0 2px}
.bar-num{font-variant-numeric:tabular-nums;font-size:12px;min-width:58px;text-align:right;
  color:var(--ink-2)}
th.right{text-align:right}
.spark{display:block;vertical-align:middle}
.reasons{white-space:normal;max-width:320px;color:var(--ink-2);font-size:12px}
.feed{background:var(--surface);border:1px solid var(--line);border-radius:10px;
  overflow:hidden;max-width:1000px}
.feed-row{display:flex;gap:12px;align-items:baseline;padding:10px 14px;
  border-bottom:1px solid var(--line);font-size:13px}
.feed-row:last-child{border-bottom:none}
.feed-date{color:var(--muted);font-size:11px;font-variant-numeric:tabular-nums;
  flex:0 0 96px;white-space:nowrap}
.feed-kind{flex:0 0 118px;color:var(--ink-2);font-size:11px;text-transform:uppercase;
  letter-spacing:.05em}
.feed-body{flex:1 1 auto;min-width:0}
.feed-body strong{font-weight:600}
.feed-amount{font-variant-numeric:tabular-nums;font-weight:700;flex:0 0 auto;text-align:right;
  min-width:78px}
.feed-then{font-size:11px;color:var(--muted);flex:0 0 auto;white-space:nowrap}
.feed-then b{color:var(--ink-2);font-variant-numeric:tabular-nums}
button.bid{font:inherit;font-size:11px;font-weight:700;text-transform:uppercase;
  letter-spacing:.05em;color:#fff;background:var(--accent);border:none;border-radius:6px;
  padding:4px 10px;cursor:pointer}
button.bid:hover{filter:brightness(1.08)}
button.raid-btn{font:inherit;font-size:11px;font-weight:700;text-transform:uppercase;
  letter-spacing:.05em;color:var(--warning);background:transparent;border-radius:6px;
  border:1px solid color-mix(in srgb,var(--warning) 50%,transparent);padding:3px 9px;
  cursor:pointer}
button.raid-btn:hover{background:color-mix(in srgb,var(--warning) 14%,transparent)}
button.ghost{font:inherit;font-size:11px;font-weight:700;text-transform:uppercase;
  letter-spacing:.05em;color:var(--ink-2);background:transparent;border:1px solid var(--line);
  border-radius:6px;padding:3px 9px;cursor:pointer;margin-left:5px}
button.ghost:hover{color:var(--ink);border-color:var(--baseline)}
button.danger{font:inherit;font-size:11px;font-weight:700;text-transform:uppercase;
  letter-spacing:.05em;color:var(--critical);background:transparent;
  border:1px solid color-mix(in srgb,var(--critical) 50%,transparent);border-radius:6px;
  padding:3px 9px;cursor:pointer;margin-left:5px}
button.danger:hover{background:color-mix(in srgb,var(--critical) 12%,transparent)}
.live{display:inline-flex;align-items:center;gap:6px;font-size:11px;color:var(--muted);
  margin-left:2px}
#live-dot{width:8px;height:8px;border-radius:50%;display:inline-block}
#live-dot.live-on{background:var(--good);box-shadow:0 0 0 3px color-mix(in srgb,var(--good) 22%,transparent)}
#live-dot.live-off{background:var(--muted)}
.drawer{position:fixed;inset:0;background:rgba(0,0,0,.5);z-index:60;display:flex;
  justify-content:flex-end}
.drawer[hidden]{display:none}
.drawer-panel{background:var(--surface);border-left:1px solid var(--line);width:min(520px,100%);
  height:100%;overflow-y:auto;padding:24px 26px;position:relative;
  box-shadow:-18px 0 50px rgba(0,0,0,.3)}
.drawer-close{position:absolute;top:14px;right:16px;background:none;border:none;
  font-size:26px;line-height:1;color:var(--muted);cursor:pointer}
.drawer-close:hover{color:var(--ink)}
.drawer h3{margin:0 0 2px;font-size:20px;letter-spacing:-.01em}
.drawer .sub{color:var(--muted);font-size:12px;margin:0 0 18px;display:flex;align-items:center;
  gap:7px;flex-wrap:wrap}
.drawer-stats{display:grid;grid-template-columns:1fr 1fr;gap:9px 16px;margin:0 0 18px;
  font-size:13px}
.drawer-stats dt{color:var(--muted);font-size:11px;text-transform:uppercase;
  letter-spacing:.05em}
.drawer-stats dd{margin:0 0 6px;font-weight:600;font-variant-numeric:tabular-nums}
.drawer-stats dd.tit-hi{color:var(--good)}
.drawer-stats dd.tit-mid{color:var(--warning)}
.drawer-stats dd.tit-lo{color:var(--serious)}
.drawer-stats dd.tit-out{color:var(--critical)}
.drawer-chart{margin:0 0 20px}
.drawer-actions{display:flex;flex-direction:column;gap:8px}
.drawer-actions button{font:inherit;font-size:13px;font-weight:600;padding:10px 13px;
  border-radius:8px;cursor:pointer;text-align:left;border:1px solid var(--line);
  background:var(--plane);color:var(--ink)}
.drawer-actions button:hover{border-color:var(--baseline)}
.drawer-actions button.primary{background:var(--accent);color:#fff;border-color:transparent}
.drawer-actions button.danger-full{color:var(--critical);
  border-color:color-mix(in srgb,var(--critical) 45%,transparent)}
.drawer-actions button.on{background:color-mix(in srgb,var(--warning) 16%,transparent);
  border-color:color-mix(in srgb,var(--warning) 45%,transparent)}
.drawer-actions button[disabled]{opacity:.45;cursor:not-allowed}
.drawer-note{font-size:11px;color:var(--muted);margin:16px 0 0}
.effect{position:fixed;right:18px;bottom:18px;z-index:70;background:var(--surface);
  border:1px solid var(--line);border-radius:11px;padding:14px 16px 13px;
  box-shadow:0 14px 40px rgba(0,0,0,.28);max-width:340px;
  opacity:0;transform:translateY(10px);transition:opacity .28s,transform .28s}
.effect.in{opacity:1;transform:none}
.effect h4{margin:0 0 9px;font-size:12px;text-transform:uppercase;letter-spacing:.06em;
  color:var(--muted);font-weight:600}
.effect table{border-collapse:collapse;font-size:12.5px}
.effect th{text-align:left;font-weight:500;color:var(--ink-2);padding:2px 12px 2px 0;
  position:static;background:none;font-size:12.5px;text-transform:none;letter-spacing:0;
  cursor:default}
.effect td{padding:2px 0;font-variant-numeric:tabular-nums;text-align:right}
.effect td.arrow{padding:0 7px;color:var(--muted)}
.effect td.delta{padding-left:11px;font-weight:600}
.effect td.delta.up{color:var(--good)}
.effect td.delta.down{color:var(--critical)}
.effect-close{position:absolute;top:8px;right:10px;background:none;border:none;
  color:var(--muted);font-size:17px;line-height:1;cursor:pointer;padding:2px}
.effect-close:hover{color:var(--ink)}
.always-panel{border:1px solid var(--line);border-radius:9px;padding:12px 13px 13px;
  background:var(--plane);display:flex;flex-direction:column;gap:10px}
.always-panel h4{margin:0;font-size:11px;text-transform:uppercase;letter-spacing:.06em;
  color:var(--muted);font-weight:600}
.always-check{display:flex;gap:9px;align-items:flex-start;font-size:12px;line-height:1.4;
  cursor:pointer;color:var(--ink-2)}
.always-check input{margin:1px 0 0;width:15px;height:15px;flex:0 0 auto;accent-color:var(--accent)}
.always-check b{color:var(--ink)}
.always-check i{color:var(--muted);font-style:normal;font-size:11px}
.always-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px}
.always-grid label{display:flex;flex-direction:column;gap:4px;font-size:11px;color:var(--muted)}
.always-grid input{font:inherit;font-size:15px;font-variant-numeric:tabular-nums;
  padding:7px 9px;border:1px solid var(--line);border-radius:7px;background:var(--surface);
  color:var(--ink);width:100%;box-sizing:border-box}
.always-grid input::placeholder{color:var(--muted);font-size:12px}
.always-foot{display:flex;align-items:center;gap:10px;justify-content:space-between}
.always-foot p{margin:0;font-size:11px;color:var(--muted);line-height:1.35}
.always-foot p b{color:var(--warning)}
.always-save{font:inherit;font-size:12px;font-weight:600;padding:7px 13px;border-radius:7px;
  border:1px solid transparent;background:var(--accent);color:#fff;cursor:pointer;flex:0 0 auto}
.always-save[disabled]{opacity:.5;cursor:not-allowed}
.always-saved{color:var(--good);font-weight:600}
.modal{position:fixed;inset:0;background:rgba(0,0,0,.55);display:flex;align-items:center;
  justify-content:center;z-index:50;padding:20px}
.modal[hidden]{display:none}
.modal-card{background:var(--surface);border:1px solid var(--line);border-radius:12px;
  padding:22px;max-width:420px;width:100%;box-shadow:0 18px 50px rgba(0,0,0,.3)}
.modal-card h3{margin:0 0 4px;font-size:17px}
.modal-card .bid-who{font-weight:700}
.bid-field{display:flex;flex-direction:column;gap:5px;margin:16px 0 6px}
.bid-field label{font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:var(--muted)}
.bid-field input{font:inherit;font-size:19px;font-variant-numeric:tabular-nums;
  padding:9px 11px;border:1px solid var(--line);border-radius:8px;background:var(--plane);
  color:var(--ink)}
.bid-refs{display:flex;gap:14px;font-size:12px;color:var(--ink-2);margin:2px 0 0;flex-wrap:wrap}
.bid-refs b{color:var(--ink);font-variant-numeric:tabular-nums}
.bid-refs b.rivals-on{color:var(--warning)}
.bid-warn,.bid-warn-line{font-size:12px;color:var(--warning);margin:10px 0 0}
.bid-error{font-size:12px;color:var(--critical);margin:10px 0 0;min-height:1em}
.bid-dl{display:grid;grid-template-columns:auto 1fr;gap:6px 14px;margin:4px 0 0;font-size:13px}
.bid-dl dt{color:var(--muted)}
.bid-dl dd{margin:0;text-align:right;font-variant-numeric:tabular-nums}
.bid-ok{color:var(--good);font-weight:600}
.modal-actions{display:flex;gap:9px;justify-content:flex-end;margin-top:20px}
.modal-actions button{font:inherit;font-size:13px;border-radius:8px;padding:8px 15px;
  cursor:pointer;border:1px solid var(--line);background:var(--plane);color:var(--ink)}
.modal-actions .bid-next,.modal-actions .bid-confirm{background:var(--accent);color:#fff;
  border-color:transparent;font-weight:600}
.modal-actions .bid-drop{color:var(--critical);border-color:color-mix(in srgb,var(--critical) 45%,transparent);margin-right:auto}
.modal-note{font-size:11px;color:var(--muted);margin:14px 0 0}
.cal{display:flex;gap:12px;overflow-x:auto;padding-bottom:6px}
.cal-day{flex:0 0 300px;background:var(--surface);border:1px solid var(--line);
  border-radius:10px;padding:12px 13px}
.cal-head{display:flex;align-items:baseline;justify-content:space-between;gap:8px}
.cal-head strong{font-size:14px;text-transform:capitalize}
.cal-count{font-size:11px;font-weight:700;color:var(--ink-2);background:var(--mid);
  border-radius:99px;padding:2px 8px}
.cal-meta{font-size:11px;color:var(--muted);margin:3px 0 10px}
.cal-chips{display:flex;flex-direction:column;gap:4px}
.cal-chip{display:flex;align-items:center;gap:6px;font-size:12px;padding:4px 7px;
  border-radius:6px;background:var(--plane);width:100%;border:1px solid transparent;
  font:inherit;font-size:12px;color:var(--ink);cursor:pointer;text-align:left}
.cal-chip:hover{border-color:var(--warning)}
.cal-chip[data-raid]:hover{background:color-mix(in srgb,var(--warning) 10%,transparent)}
.cal-armed{border-color:color-mix(in srgb,var(--warning) 60%,transparent)}
.cal-armed-mark{font-size:9px;text-transform:uppercase;letter-spacing:.06em;
  color:var(--warning);font-weight:700}
.cal-chip b{margin-left:auto;font-variant-numeric:tabular-nums;font-size:11px;
  color:var(--ink-2)}
.cal-mine{background:color-mix(in srgb,var(--warning) 15%,transparent);
  box-shadow:inset 0 0 0 1px color-mix(in srgb,var(--warning) 40%,transparent)}
.cal-more{font-size:11px;color:var(--muted);padding:2px 6px}

.pitch-bar{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin:0 0 14px}
.pitch-bar label{font-size:12px;color:var(--muted);display:flex;align-items:center;gap:6px}
.pitch-bar select{font:inherit;font-size:13px;color:var(--ink);background:var(--surface);
  border:1px solid var(--line);border-radius:7px;padding:6px 9px}
.pitch-bar button{font:inherit;font-size:12px;font-weight:600;border-radius:7px;padding:7px 13px;
  cursor:pointer;border:1px solid var(--line);background:var(--surface);color:var(--ink)}
.pitch-bar button.primary{background:var(--accent);color:#fff;border-color:transparent}
.pitch-bar button[disabled]{opacity:.45;cursor:not-allowed}
#pitch-status{margin-left:auto}
.pitch-wrap{display:grid;grid-template-columns:1fr 260px;gap:16px;align-items:start}
@media (max-width:900px){.pitch-wrap{grid-template-columns:1fr}}
.pitch{position:relative;border-radius:14px;padding:22px 16px;display:flex;
  flex-direction:column;justify-content:space-between;gap:14px;min-height:560px;
  background:
    repeating-linear-gradient(180deg,
      color-mix(in srgb,var(--pitch-green) 96%,#000) 0 46px,
      color-mix(in srgb,var(--pitch-green) 88%,#000) 46px 92px);
  box-shadow:inset 0 0 0 2px color-mix(in srgb,#fff 22%,transparent)}
.pitch::before{content:"";position:absolute;left:8%;right:8%;top:50%;height:2px;
  background:color-mix(in srgb,#fff 22%,transparent)}
.pitch::after{content:"";position:absolute;left:50%;top:50%;width:88px;height:88px;
  transform:translate(-50%,-50%);border:2px solid color-mix(in srgb,#fff 22%,transparent);
  border-radius:50%}
.pitch-line{display:flex;justify-content:space-evenly;gap:8px;position:relative;z-index:1}
.slot{width:112px;min-height:118px;border-radius:10px;display:flex;flex-direction:column;
  align-items:center;gap:3px;padding:7px 5px;transition:transform .14s ease,box-shadow .14s ease;
  background:color-mix(in srgb,#000 34%,transparent);cursor:grab;
  backdrop-filter:blur(2px)}
.slot:hover{transform:translateY(-3px);box-shadow:0 8px 22px rgba(0,0,0,.35)}
.slot:hover .slot-name{text-decoration:underline;text-decoration-style:dotted;
  text-underline-offset:2px}
.slot.dragging{opacity:.35;cursor:grabbing}
.slot.drop-target{box-shadow:inset 0 0 0 2px var(--warning)}
.slot.empty{background:color-mix(in srgb,#000 18%,transparent);
  box-shadow:inset 0 0 0 1px color-mix(in srgb,#fff 24%,transparent);cursor:default;
  align-items:center;justify-content:center;color:color-mix(in srgb,#fff 55%,transparent);
  font-size:11px;text-transform:uppercase;letter-spacing:.06em}
.slot .crest{width:20px;height:20px}
.slot-name{font-size:12px;font-weight:700;color:#fff;text-align:center;line-height:1.15;
  text-shadow:0 1px 2px rgba(0,0,0,.55)}
.slot-meta{display:flex;gap:5px;align-items:center;justify-content:center;font-size:10px;
  flex-wrap:wrap;white-space:nowrap;color:color-mix(in srgb,#fff 78%,transparent)}
.slot-weeks{display:flex;gap:3px;flex-wrap:wrap;justify-content:center}
.wk{font-size:10px;font-weight:700;border-radius:4px;padding:1px 4px;color:#fff;
  font-variant-numeric:tabular-nums}
.wk-hi{background:#0ca30c}.wk-mid{background:#2a78d6}.wk-lo{background:#8a5a00}
.wk-neg{background:#d03b3b}.wk-none{background:rgba(255,255,255,.18)}
/* Sin relleno: el relleno queda reservado a los puntos de cada jornada, para que un
   80% no se lea como "80 puntos". */
.tit{font-size:10px;font-weight:800;font-variant-numeric:tabular-nums}
.tit-hi{color:#8ee6a8}
.tit-mid{color:#ffd479}
.tit-lo{color:#ffb98f}
.tit-out{color:#ff9c92}
/* La J solo cuando hay jornadas de verdad: "J sin jornadas" no se lee bien. */
.slot-weeks .wk-label{font-size:9px;font-weight:700;align-self:center;margin-right:1px;
  color:color-mix(in srgb,#fff 55%,transparent)}
.slot-trend{font-size:10px;font-weight:700;font-variant-numeric:tabular-nums}
.slot-trend.up{color:#8ee6a8}.slot-trend.down{color:#ffb1a8}
.slot{position:relative}
.ring-red{box-shadow:inset 0 0 0 2px var(--critical)}
.ring-amber{box-shadow:inset 0 0 0 2px var(--warning)}
.slot.ring-red:hover,.slot.ring-amber:hover{box-shadow:inset 0 0 0 2px currentColor,
  0 8px 22px rgba(0,0,0,.35)}
.slot.ring-red:hover{color:var(--critical)}
.slot.ring-amber:hover{color:var(--warning)}
.badge-status{position:absolute;top:5px;right:5px;width:16px;height:20px;border-radius:3px;
  display:flex;align-items:center;justify-content:center;z-index:2;font-weight:900;
  font-size:13px;line-height:1;box-shadow:0 1px 3px rgba(0,0,0,.5)}
/* Tarjeta roja: la insignia misma, con proporcion de tarjeta. */
.st-sancionado{background:#d03b3b}
/* Botiquin: cruz roja sobre blanco, dos barras solidas. */
.st-lesionado{background:#fff}
.st-lesionado .kit{position:relative;display:block;width:11px;height:11px}
.st-lesionado .kit::before,.st-lesionado .kit::after{content:"";position:absolute;
  background:#d03b3b;border-radius:1px}
.st-lesionado .kit::before{left:4px;top:0;width:3px;height:11px}
.st-lesionado .kit::after{top:4px;left:0;height:3px;width:11px}
.st-duda{background:#fab219;color:#3a2c00}
/* Tooltip propio: el nativo tarda casi un segundo en salir. */
.tip{position:absolute;bottom:calc(100% + 7px);left:50%;transform:translateX(-50%) scale(.96);
  text-transform:none;letter-spacing:normal;font-weight:400;
  background:#101114;color:#fff;border:1px solid rgba(255,255,255,.14);border-radius:8px;
  padding:7px 10px;font-size:11px;line-height:1.35;width:190px;text-align:left;
  opacity:0;pointer-events:none;transition:opacity .09s ease,transform .09s ease;z-index:20;
  box-shadow:0 8px 24px rgba(0,0,0,.45)}
.tip strong{display:block;font-size:11px;margin-bottom:2px}
.tip em{color:#c3c2b7;font-style:normal;font-size:10px}
.slot:hover .tip,.bench-item:hover .tip{opacity:1;transform:translateX(-50%) scale(1)}
.bench{background:var(--surface);border:1px solid var(--line);border-radius:12px;padding:14px}
.bench h3{margin:0 0 10px;font-size:13px;text-transform:uppercase;letter-spacing:.06em;
  color:var(--muted)}
.bench-list{display:flex;flex-direction:column;gap:7px;min-height:120px}
.bench.drop-target{border-color:var(--warning)}
.bench-item{display:flex;align-items:center;gap:8px;padding:7px 9px;border-radius:8px;
  background:var(--plane);font-size:12px;cursor:grab;position:relative}
.bench-item:hover{background:color-mix(in srgb,var(--accent) 9%,transparent)}
.bench-item.dragging{opacity:.35}
.bench-item .crest{width:16px;height:16px}
.bench-name{font-weight:600}
.bench-empty{color:var(--muted);font-size:12px;font-style:italic}
.callout{background:color-mix(in srgb,var(--warning) 12%,transparent);
  border-left:3px solid var(--warning);border-radius:0 8px 8px 0;padding:11px 14px;
  font-size:13px;margin:0 0 14px;color:var(--ink)}
.empty{color:var(--muted);font-size:13px;font-style:italic;background:var(--surface);
  border:1px solid var(--line);border-radius:10px;padding:14px}
.legend{display:flex;flex-wrap:wrap;gap:14px;margin:0 0 14px;font-size:12px;color:var(--ink-2)}
.legend span{display:inline-flex;align-items:center;gap:6px}
.swatch{width:11px;height:11px;border-radius:3px;display:inline-block}
footer{margin-top:64px;color:var(--ink-2);font-size:12px;border-top:1px solid var(--line);
  padding-top:18px;max-width:90ch}
footer code{background:var(--mid);padding:1px 5px;border-radius:4px;font-size:11px}
"""

JS = r"""
// ---- estado de filtros, para que sobreviva a un recambio de seccion --------
const filterState = {pos:'all', price:'', text:''};

function wireTables(root=document){
  root.querySelectorAll('table.sortable').forEach(table=>{
    if(table.dataset.wired) return;
    table.dataset.wired='1';
    table.querySelectorAll('th').forEach((th,index)=>{
      th.addEventListener('click',()=>{
        const body=table.tBodies[0], rows=[...body.rows];
        const numeric=['money','pct','num','int','pct_plain','spark','verdict','mag','ideal','hours','ratio'].includes(th.dataset.kind);
        const desc=!th.classList.contains('sorted-desc');
        table.querySelectorAll('th').forEach(h=>h.classList.remove('sorted-asc','sorted-desc'));
        th.classList.add(desc?'sorted-desc':'sorted-asc');
        rows.sort((a,b)=>{
          const x=a.cells[index].dataset.sort, y=b.cells[index].dataset.sort;
          const cmp=numeric?(parseFloat(x||0)-parseFloat(y||0)):String(x).localeCompare(String(y),'es');
          return desc?-cmp:cmp;
        });
        rows.forEach(r=>body.appendChild(r));
      });
    });
  });
}

function applyFilters(){
  const maxPrice=parseFloat(filterState.price)||Infinity;
  const needle=filterState.text.trim().toLowerCase();
  document.querySelectorAll('.filters').forEach(bar=>{
    const scope=bar.closest('section');
    let shown=0,total=0;
    scope.querySelectorAll('tr[data-position]').forEach(row=>{
      total++;
      const ok=(filterState.pos==='all'||row.dataset.position===filterState.pos)
        && parseFloat(row.dataset.price)<=maxPrice
        && (!needle||row.dataset.name.includes(needle));
      row.hidden=!ok; if(ok) shown++;
    });
    const counter=bar.querySelector('.f-count');
    if(counter) counter.textContent=shown+' de '+total+' filas';
  });
}

function wireFilters(root=document){
  root.querySelectorAll('.filters').forEach(bar=>{
    if(bar.dataset.wired) return;
    bar.dataset.wired='1';
    const pos=bar.querySelector('.f-pos'), price=bar.querySelector('.f-price'),
          text=bar.querySelector('.f-text'), reset=bar.querySelector('.f-reset');
    pos.value=filterState.pos; price.value=filterState.price; text.value=filterState.text;
    const sync=()=>{ filterState.pos=pos.value; filterState.price=price.value;
                     filterState.text=text.value; applyFilters(); };
    [pos,price,text].forEach(el=>el.addEventListener('input',sync));
    reset.addEventListener('click',()=>{ filterState.pos='all'; filterState.price='';
      filterState.text=''; pos.value='all'; price.value=''; text.value=''; applyFilters(); });
  });
  applyFilters();
}

// ---- favoritos -------------------------------------------------------------
function wireStars(root=document){
  root.querySelectorAll('button.star').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click', async ()=>{
      const on=button.classList.contains('on');
      const paint=(state,el)=>{ el.classList.toggle('on',state);
        el.textContent=state?'★':'☆';
        el.setAttribute('aria-pressed',state?'true':'false'); };
      paint(!on,button);
      try{
        const res=await fetch('/api/favourite',{method:'POST',
          headers:{'Content-Type':'application/json'},
          body:JSON.stringify({id:button.dataset.player,name:button.dataset.name})});
        if(!res.ok) throw new Error(res.status);
        const data=await res.json();
        document.querySelectorAll(`button.star[data-player="${button.dataset.player}"]`)
          .forEach(el=>paint(!!data.starred,el));
      }catch(e){
        paint(on,button);
        button.title='Solo se puede cambiar en la version servida (fantasy serve)';
      }
    });
  });
}

// ---- cuentas atras en vivo -------------------------------------------------
function tick(){
  const now=Date.now();
  document.querySelectorAll('[data-deadline]').forEach(el=>{
    const left=new Date(el.dataset.deadline).getTime()-now;
    if(isNaN(left)) return;
    if(left<=0){ el.textContent='ya'; el.className='pill-critical'; return; }
    const h=Math.floor(left/3600000), m=Math.floor(left%3600000/60000),
          s=Math.floor(left%60000/1000);
    el.textContent = h>=24 ? Math.floor(h/24)+'d '+(h%24)+'h'
                   : h>0   ? h+'h '+String(m).padStart(2,'0')+'m'
                           : m+'m '+String(s).padStart(2,'0')+'s';
    el.className = h<1 ? 'pill-critical' : h<24 ? 'pill-warning' : 'pill-neutral';
  });
}
setInterval(tick,1000);

// ---- puja con doble confirmacion ------------------------------------------
const modal=document.getElementById('bid-modal');
let pending=null;

const fmt=(n)=> n==null ? '—' :
  (Math.abs(n)>=1e6 ? (n/1e6).toFixed(2)+'M' : Math.abs(n)>=1e3 ? (n/1e3).toFixed(0)+'K' : String(n));
// El importe se escribe con puntos de millar para que no haya que contar ceros.
const group=(n)=> (n==null||isNaN(n)) ? '' : Number(n).toLocaleString('es-ES');
const digits=(s)=> parseInt(String(s).replace(/[^0-9]/g,''),10);
const exact=(n)=> n==null ? '—' : Number(n).toLocaleString('es-ES')+' €';

function closeModal(){ modal.hidden=true; pending=null; }

// Un unico sitio decide que paso se ve: antes los botones compartian clase con los
// bloques y querySelector solo alcanzaba al primero, asi que el contenido avanzaba
// y los botones se quedaban en el paso uno.
function showStep(step,{confirmLabel='Aceptar'}={}){
  modal.querySelector('#bid-amount-step').hidden = step!==1;
  modal.querySelector('#bid-summary-step').hidden = step!==2;
  modal.querySelector('.bid-next').hidden = step!==1;
  const confirm=modal.querySelector('.bid-confirm');
  confirm.hidden = step!==2;
  confirm.textContent = confirmLabel;
  confirm.disabled = false;
}

function wireBids(root=document){
  root.querySelectorAll('button.bid').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>openBid(button.dataset));
  });
}

function openBid(data){
  pending={market_id:data.market, player_id:data.player, name:data.name,
           min_bid:+data.min, ideal:+data.ideal||0, value:+data.value};
  modal.hidden=false;
  modal.querySelector('.bid-action').textContent='Pujar por';
  modal.querySelector('.bid-who').textContent=data.name;
  const suggested = pending.ideal && pending.ideal>=pending.min_bid ? pending.ideal : pending.min_bid;
  const input=modal.querySelector('.bid-amount');
  input.value=group(suggested);
  modal.querySelector('.bid-min').textContent=exact(pending.min_bid);
  modal.querySelector('.bid-ideal').textContent=pending.ideal?exact(pending.ideal):'sin margen';
  modal.querySelector('.bid-value').textContent=exact(pending.value);
  showRivals(+data.bids||0, data.expires);
  const drop=modal.querySelector('.bid-drop');
  pending.bid_id=data.bid||null;
  drop.hidden=!pending.bid_id;
  showStep(1);
  modal.querySelector('.bid-error').textContent='';
  checkAmount();
  input.focus();
}

function showRivals(count, expires){
  const wrap=modal.querySelector('.bid-rivals-wrap');
  const node=modal.querySelector('.bid-rivals');
  if(!wrap) return;
  const isBid = pending && (pending.operation==='bid' || !pending.operation);
  wrap.hidden = !isBid;
  if(!isBid) return;
  node.textContent = count ? String(count) : 'ninguna';
  node.className = 'bid-rivals'+(count?' rivals-on':'');
}

function checkAmount(){
  const input=modal.querySelector('.bid-amount');
  const warn=modal.querySelector('.bid-warn');
  const amount=digits(input.value);
  const caret=input.selectionStart, before=input.value.length;
  input.value=group(amount);
  if(document.activeElement===input){
    const shift=input.value.length-before;
    input.setSelectionRange(Math.max(0,caret+shift), Math.max(0,caret+shift));
  }
  let text='';
  if(!amount) text='Escribe un importe.';
  else if(amount<pending.min_bid) text='Por debajo de la puja minima ('+exact(pending.min_bid)+').';
  if(pending.ideal && amount>pending.ideal) text='Por encima del techo rentable de futbolfantasy.';
  else if(!pending.ideal) text='futbolfantasy no le ve rentabilidad a este precio.';
  warn.textContent=text;
  warn.hidden=!text;
}

if(modal){
  modal.querySelector('.bid-amount').addEventListener('input',checkAmount);
  modal.querySelector('.bid-cancel').addEventListener('click',closeModal);
  modal.addEventListener('click',(e)=>{ if(e.target===modal) closeModal(); });
  document.addEventListener('keydown',(e)=>{ if(e.key==='Escape'&&!modal.hidden) closeModal(); });

  // paso 1: pedir al servidor que valide y devuelva el resumen + token
  modal.querySelector('.bid-next').addEventListener('click', async ()=>{
    const amount=digits(modal.querySelector('.bid-amount').value);
    modal.querySelector('.bid-error').textContent='';
    try{
      const res=await fetch('/api/bid/prepare',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({operation:pending.operation||'bid', amount,
                             market_id:pending.market_id, player_id:pending.player_id,
                             player_team_id:pending.player_team_id,
                             offer_id:pending.offer_id})});
      const data=await res.json();
      if(!res.ok) throw new Error(data.error||res.status);
      pending.token=data.token;
      const op=pending.operation||'bid';
      const movesCash=['bid','direct_offer','pay_clause','accept_offer'].includes(op);
      modal.querySelector('.bid-summary').innerHTML =
        `<dl class="bid-dl">
           <dt>Jugador</dt><dd>${data.player_name||pending.name}</dd>
           <dt>${AMOUNT_LABEL[op]||'Importe'}</dt>
             <dd><strong>${exact(data.amount)}</strong></dd>
           <dt>Saldo ahora</dt><dd>${exact(data.cash_before)}</dd>
           ${movesCash?`<dt>${op==='accept_offer'?'Saldo despues':'Saldo si sale'}</dt>
             <dd><strong>${exact(data.cash_after)}</strong></dd>`:''}
         </dl>` +
        (data.warnings||[]).map(w=>`<p class="bid-warn-line">⚠ ${w}</p>`).join('');
      showStep(2,{confirmLabel:CONFIRM_LABEL[pending.operation]||'Aceptar'});
    }catch(err){
      modal.querySelector('.bid-error').textContent=err.message;
    }
  });

  // retirar una puja ya puesta, con el mismo doble paso
  modal.querySelector('.bid-drop').addEventListener('click', async ()=>{
    modal.querySelector('.bid-error').textContent='';
    try{
      const res=await fetch('/api/bid/prepare',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({operation:'cancel_bid',market_id:pending.market_id,
                             bid_id:pending.bid_id,player_id:pending.player_id})});
      const data=await res.json();
      if(!res.ok) throw new Error(data.error||res.status);
      pending.token=data.token;
      modal.querySelector('.bid-summary').innerHTML =
        `<p>Vas a <strong>retirar tu puja</strong> por ${pending.name}.</p>`;
      modal.querySelector('.bid-drop').hidden=true;
      showStep(2);
    }catch(err){ modal.querySelector('.bid-error').textContent=err.message; }
  });

  // paso 2: confirmar de verdad
  modal.querySelector('.bid-confirm').addEventListener('click', async ()=>{
    const button=modal.querySelector('.bid-confirm');
    button.disabled=true; button.textContent='Enviando…';
    try{
      const res=await fetch('/api/bid/confirm',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({token:pending.token})});
      const data=await res.json();
      if(!res.ok) throw new Error(data.error||res.status);
      modal.querySelector('.bid-summary').innerHTML =
        `<p class="bid-ok">${DONE_LABEL[pending&&pending.operation]||'Hecho'}${
          data.dry_run?' (simulacro)':''}.</p>`;
      modal.querySelector('.bid-confirm').hidden=true;
      modal.querySelector('.bid-cancel').textContent='Cerrar';
    }catch(err){
      modal.querySelector('.bid-error').textContent=err.message;
    }finally{
      button.disabled=false; button.textContent='Aceptar';
    }
  });
}

// ---- operaciones genericas (aceptar/rechazar oferta, retirar) --------------
// Cada operacion se llama por su nombre en el boton final y en el resumen: "Pujas" y
// "Saldo si ganas" no significan nada cuando lo que haces es vender.
const CONFIRM_LABEL={};   // el boton dice simplemente Aceptar
const DONE_LABEL={bid:'Puja enviada',sell_to_market:'Puesto en venta',
  accept_offer:'Oferta aceptada',decline_offer:'Oferta rechazada',
  withdraw:'Retirado del mercado',direct_offer:'Oferta enviada',
  pay_clause:'Clausula pagada',raise_clause:'Clausula subida',
  cancel_bid:'Puja retirada'};
const AMOUNT_LABEL={bid:'Pujas',sell_to_market:'Precio de venta',
  accept_offer:'Cobras',direct_offer:'Ofreces',pay_clause:'Pagas',
  raise_clause:'Subes la clausula'};

const OP_LABELS={accept_offer:'Aceptar oferta por',decline_offer:'Rechazar oferta por',
                 withdraw:'Retirar del mercado a',sell_to_market:'Poner en venta a'};

function wireOps(root=document){
  root.querySelectorAll('button.op').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click', async ()=>{
      const d=button.dataset;
      pending={operation:d.op, market_id:d.opMarket, offer_id:d.opOffer,
               player_id:d.opPlayer, name:d.opName, amount:+d.opAmount||null};
      modal.hidden=false;
      modal.querySelector('.bid-who').textContent=d.opName;
      modal.querySelector('.bid-action').textContent=OP_LABELS[d.op]||'Confirmar';
      modal.querySelector('.bid-drop').hidden=true;
      modal.querySelector('.bid-error').textContent='';
      modal.querySelector('.bid-summary').innerHTML='<p>Comprobando…</p>';
      showStep(2,{confirmLabel:CONFIRM_LABEL[d.op]||'Aceptar'});
      try{
        const res=await fetch('/api/bid/prepare',{method:'POST',
          headers:{'Content-Type':'application/json'},
          body:JSON.stringify({operation:d.op,market_id:d.opMarket,offer_id:d.opOffer,
                               player_id:d.opPlayer,amount:+d.opAmount||undefined})});
        const data=await res.json();
        if(!res.ok) throw new Error(data.error||res.status);
        pending.token=data.token;
        modal.querySelector('.bid-summary').innerHTML=
          `<dl class="bid-dl">
             <dt>Operacion</dt><dd>${data.label}</dd>
             <dt>Jugador</dt><dd>${data.player_name||d.opName}</dd>
             ${data.amount?`<dt>Importe</dt><dd><strong>${exact(data.amount)}</strong></dd>`:''}
             <dt>Saldo</dt><dd>${exact(data.cash_before)}</dd>
           </dl>` +
          (data.warnings||[]).map(w=>`<p class="bid-warn-line">⚠ ${w}</p>`).join('');
      }catch(err){
        modal.querySelector('.bid-summary').innerHTML='';
        modal.querySelector('.bid-error').textContent=err.message;
        modal.querySelector('.bid-confirm').hidden=true;
      }
    });
  });
}



// ---- alineacion: campo, arrastrar y guardar --------------------------------
const LINE_ORDER=['striker','midfield','defender','goalkeeper'];   // arriba -> abajo
const LINE_LABEL={goalkeeper:'POR',defender:'DEF',midfield:'MED',striker:'DEL'};
const LINE_POS={goalkeeper:1,defender:2,midfield:3,striker:4};
let pitchState=null, pitchDirty=false, dragged=null;


// Iconos de estado: tarjeta roja, botiquin, incognita. El motivo lo pone
// futbolfantasy (la API solo da el codigo de estado) y sale al instante con un
// tooltip propio, porque el title nativo tarda casi un segundo.
// Glifos de texto, no SVG: a 12px un trazo fino no se ve, y una cruz de dos rects
// o un caracter siempre salen.
const ICON_CARD='';              // la propia insignia ES la tarjeta
const ICON_KIT='<i class="kit"></i>';
const ICON_DOUBT='?';

function statusOf(player){
  const st=player.status||'ok';
  const a=player.absence||{};
  if(st==='suspended'||st==='sanctioned'||a.kind==='sancionado')
    return {cls:'st-sancionado', icon:ICON_CARD, label:'Sancionado'};
  if(st==='injured'||a.kind==='lesionado')
    return {cls:'st-lesionado', icon:ICON_KIT, label:'Lesionado'};
  if(st==='doubtful'||a.kind==='duda')
    return {cls:'st-duda', icon:ICON_DOUBT, label:'Duda'};
  return null;
}

function statusRing(player){
  const s=statusOf(player);
  return s ? ' ring-'+(s.cls==='st-sancionado'?'red':'amber') : '';
}

function statusBadge(player){
  const s=statusOf(player);
  if(!s) return '';
  const a=player.absence||{};
  const bits=[`<strong>${s.label}</strong>`];
  if(a.reason) bits.push(a.reason);
  if(a.since) bits.push(`<em>${a.since}</em>`);
  if(a.until) bits.push(`<em>${a.until}</em>`);
  if(!a.reason) bits.push('<em>sin detalle en futbolfantasy</em>');
  return `<span class="badge-status ${s.cls}">${s.icon}`
    +`<span class="tip">${bits.join('<br>')}</span></span>`;
}

// Titularidad: el numero que decide si los xPts se van a materializar.
function titClass(p){
  return p>=75?'tit-hi':p>=50?'tit-mid':p>=30?'tit-lo':'tit-out';
}

function weekChip(w){
  const p=w.points;
  const cls = p==null?'wk-none': p<0?'wk-neg': p>=8?'wk-hi': p>=4?'wk-mid':'wk-lo';
  return `<span class="wk ${cls}" title="Jornada ${w.week}">${p==null?'–':p}</span>`;
}

function shirtHtml(player,line,index){
  if(!player) return `<div class="slot empty" data-line="${line}" data-index="${index}">`
    +`${LINE_LABEL[line]}<br>libre</div>`;
  const trend=player.projected_pct||0;
  const played=(player.weeks||[]).slice(-5);
  const weeks=played.length
    ? '<span class="wk-label">J</span>'+played.map(weekChip).join('')
    : '<span class="wk wk-none">sin jornadas</span>';
  return `<div class="slot${statusRing(player)}" draggable="true" data-line="${line}"
    data-index="${index}" data-player="${player.id}" data-pt="${player.player_team_id}"
    title="${player.name} · ${player.next_rival?('vs '+player.next_rival):''}">
    ${statusBadge(player)}
    <span class="crest crest-${player.team_id}"></span>
    <span class="slot-name">${player.name}</span>
    <span class="slot-weeks">${weeks}</span>
    <span class="slot-meta">
      <span>${(player.xpts||0).toFixed(1)} xPts</span>
      ${player.start_probability!=null?`<span class="tit ${titClass(player.start_probability)}"
        >${player.start_probability}%</span>`:''}
      <span class="slot-trend ${trend>=0?'up':'down'}">${trend>=0?'▲':'▼'}${Math.abs(trend).toFixed(1)}%</span>
    </span>
  </div>`;
}

function benchHtml(player){
  const trend=player.projected_pct||0;
  return `<div class="bench-item${statusRing(player)}" draggable="true" data-player="${player.id}"
    data-pt="${player.player_team_id}" data-from="bench" title="${player.name}">
    ${statusBadge(player)}
    <span class="crest crest-${player.team_id}"></span>
    <span class="pos pos-${(LINE_LABEL[Object.keys(LINE_POS).find(k=>LINE_POS[k]===player.position_id)]||'ENT').toLowerCase()}">${
      {1:'POR',2:'DEF',3:'MED',4:'DEL'}[player.position_id]||'ENT'}</span>
    <span class="bench-name">${player.name}</span>
    <span class="slot-trend ${trend>=0?'up':'down'}" style="margin-left:auto">${
      (player.xpts||0).toFixed(1)}</span>
  </div>`;
}

function renderPitch(){
  if(!pitchState) return;
  const pitch=document.getElementById('pitch');
  const benchList=document.getElementById('bench-list');
  if(!pitch) return;
  pitch.innerHTML=LINE_ORDER.map(line=>{
    const slots=pitchState.lines[line]||[];
    return `<div class="pitch-line" data-line="${line}">`
      + slots.map((p,i)=>shirtHtml(p,line,i)).join('') + '</div>';
  }).join('');
  benchList.innerHTML=(pitchState.bench||[]).map(benchHtml).join('')
    || '<p class="bench-empty">Sin reservas</p>';
  document.getElementById('pitch-formation').textContent=
    (pitchState.formation||[]).join('-');
  const save=document.getElementById('pitch-save');
  save.disabled=!pitchDirty||!pitchState.writes_enabled;
  document.getElementById('pitch-status').textContent = pitchDirty
    ? 'cambios sin guardar' : (pitchState.writes_enabled?'':'servidor en solo lectura');
  wireDrag();
}

let justDragged=false;

function wireDrag(){
  document.querySelectorAll('.slot[draggable], .bench-item[draggable]').forEach(node=>{
    // Arrastrar y clicar empiezan igual, asi que un drop no puede abrir la ficha.
    node.addEventListener('click',()=>{
      if(justDragged) return;
      if(node.dataset.player) openDetail(node.dataset.player);
    });
    node.addEventListener('dragstart',e=>{
      dragged={id:node.dataset.player, pt:node.dataset.pt,
               from:node.dataset.from||'pitch',
               line:node.dataset.line, index:+node.dataset.index};
      node.classList.add('dragging');
      e.dataTransfer.effectAllowed='move';
      e.dataTransfer.setData('text/plain',node.dataset.player);
    });
    node.addEventListener('dragend',()=>{
      node.classList.remove('dragging'); dragged=null;
      justDragged=true; setTimeout(()=>{ justDragged=false; },250);
      document.querySelectorAll('.drop-target').forEach(n=>n.classList.remove('drop-target')); });
  });
  const targets=[...document.querySelectorAll('.slot'), document.getElementById('bench')];
  targets.forEach(node=>{
    if(!node) return;
    node.addEventListener('dragover',e=>{ e.preventDefault(); node.classList.add('drop-target'); });
    node.addEventListener('dragleave',()=>node.classList.remove('drop-target'));
    node.addEventListener('drop',e=>{
      e.preventDefault(); node.classList.remove('drop-target');
      if(!dragged) return;
      if(node.id==='bench') dropOnBench();
      else dropOnSlot(node.dataset.line, +node.dataset.index);
    });
  });
}

function takeFrom(source){
  if(source.from==='bench'){
    const i=pitchState.bench.findIndex(p=>p&&p.id===source.id);
    return i<0?null:pitchState.bench.splice(i,1)[0];
  }
  const arr=pitchState.lines[source.line];
  const player=arr[source.index]; arr[source.index]=null;
  return player;
}

function dropOnSlot(line,index){
  const moving=dragged;
  if(moving.from==='pitch'&&moving.line===line&&moving.index===index) return;
  const target=pitchState.lines[line][index]||null;
  const player=takeFrom(moving);
  if(!player) return;
  // Una linea solo acepta su propia posicion; el portero es intransferible.
  if(player.position_id!==LINE_POS[line]){
    // devolver y avisar
    if(moving.from==='bench') pitchState.bench.push(player);
    else pitchState.lines[moving.line][moving.index]=player;
    flashPitch(`${player.name} es ${{1:'portero',2:'defensa',3:'medio',4:'delantero'}[player.position_id]}`
      +`, no puede jugar de ${{goalkeeper:'portero',defender:'defensa',midfield:'medio',striker:'delantero'}[line]}.`);
    return;
  }
  pitchState.lines[line][index]=player;
  if(target){
    if(moving.from==='bench') pitchState.bench.push(target);
    else pitchState.lines[moving.line][moving.index]=target;   // intercambio
  }
  pitchDirty=true; renderPitch();
}

function dropOnBench(){
  if(dragged.from==='bench') return;
  const player=takeFrom(dragged);
  if(player) pitchState.bench.push(player);
  pitchDirty=true; renderPitch();
}

function flashPitch(message){
  const status=document.getElementById('pitch-status');
  status.textContent=message;
  status.style.color='var(--warning)';
  setTimeout(()=>{ status.style.color=''; renderPitch(); },2600);
}

function applyFormation(text){
  const [d,m,s]=text.split(',').map(Number);
  const want={goalkeeper:1,defender:d,midfield:m,striker:s};
  const spare=[];
  LINE_ORDER.forEach(line=>{
    const arr=pitchState.lines[line]||[];
    while(arr.length>want[line]){ const p=arr.pop(); if(p) spare.push(p); }
    while(arr.length<want[line]) arr.push(null);
    pitchState.lines[line]=arr;
  });
  // rellenar huecos con reservas de esa posicion, el resto al banquillo
  LINE_ORDER.forEach(line=>{
    pitchState.lines[line]=pitchState.lines[line].map(slot=>{
      if(slot) return slot;
      const pool=spare.concat(pitchState.bench);
      const i=pool.findIndex(p=>p&&p.position_id===LINE_POS[line]);
      if(i<0) return null;
      const chosen=pool[i];
      const inSpare=spare.indexOf(chosen);
      if(inSpare>=0) spare.splice(inSpare,1);
      else pitchState.bench.splice(pitchState.bench.indexOf(chosen),1);
      return chosen;
    });
  });
  pitchState.bench=pitchState.bench.concat(spare);
  pitchState.formation=[d,m,s];
  pitchDirty=true; renderPitch();
}

async function loadPitch(){
  const pitch=document.getElementById('pitch');
  if(!pitch) return;
  try{
    const res=await fetch('/api/lineup');
    if(!res.ok) throw new Error(res.status);
    pitchState=await res.json();
  }catch(e){
    pitch.innerHTML='<p class="slot empty" style="width:auto">Solo disponible en la '
      +'version servida</p>';
    return;
  }
  pitchDirty=false;
  const select=document.getElementById('pitch-formation-select');
  const all=[...(pitchState.formations.free||[]),...(pitchState.formations.premium||[])];
  const current=(pitchState.formation||[]).join(',');
  select.innerHTML=all.map(f=>{
    const premium=(pitchState.formations.premium||[]).includes(f);
    return `<option value="${f}"${f===current?' selected':''}>${f.replace(/,/g,'-')}`
      +`${premium?' (premium)':''}</option>`;
  }).join('');
  if(!select.dataset.wired){
    select.dataset.wired='1';
    select.addEventListener('change',()=>applyFormation(select.value));
    document.getElementById('pitch-reset').addEventListener('click',loadPitch);
    document.getElementById('pitch-save').addEventListener('click',savePitch);
  }
  renderPitch();
}

async function savePitch(){
  const missing=LINE_ORDER.some(l=>(pitchState.lines[l]||[]).some(p=>!p));
  if(missing){ flashPitch('Hay huecos sin cubrir: completa el once antes de guardar.');
    return; }
  const ids=l=>pitchState.lines[l].map(p=>p.player_team_id);
  const button=document.getElementById('pitch-save');
  button.disabled=true; button.textContent='Guardando…';
  try{
    const res=await fetch('/api/lineup',{method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({goalkeeper:ids('goalkeeper')[0], defender:ids('defender'),
                           midfield:ids('midfield'), striker:ids('striker'),
                           formation:pitchState.formation})});
    const data=await res.json();
    if(!res.ok) throw new Error(data.error||res.status);
    pitchDirty=false;
    // Los ids no cambian al guardar, asi que basta con repintar lo que ya tenemos:
    // recargar del API es una vuelta entera para el mismo resultado.
    pitchState.formation=data.formation||pitchState.formation;
    renderPitch();
    document.getElementById('pitch-status').textContent=
      'guardada ' + new Date().toLocaleTimeString('es-ES');
  }catch(err){ flashPitch('No se ha guardado: '+err.message); }
  finally{ button.textContent='Guardar alineación'; }
}

// ---- cajon de jugador: un nombre, todas sus acciones ----------------------
const drawer=document.getElementById('drawer');

function closeDrawer(){ if(drawer) drawer.hidden=true; }

function sparkSvg(history){
  const points=history.map(h=>h.value).filter(v=>v!=null);
  if(points.length<3) return '';
  const w=440,h=90,lo=Math.min(...points),hi=Math.max(...points),span=(hi-lo)||1;
  const step=w/(points.length-1);
  const path=points.map((v,i)=>`${(i*step).toFixed(1)},${(h-4-(v-lo)/span*(h-12)).toFixed(1)}`).join(' ');
  const rising=points[points.length-1]>=points[0];
  return `<svg class="drawer-chart" width="100%" height="${h}" viewBox="0 0 ${w} ${h}"
    preserveAspectRatio="none" aria-label="Historico de valor">
    <polyline points="${path}" fill="none" stroke="var(--${rising?'pole-pos':'pole-neg'})"
      stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>
    <p class="drawer-note">Valor diario, ultimos ${points.length} dias ·
      min ${fmt(lo)} · max ${fmt(hi)}</p>`;
}

async function openDetail(playerId){
  if(!drawer) return;
  drawer.hidden=false;
  const body=drawer.querySelector('.drawer-body');
  body.innerHTML='<p class="empty">Cargando…</p>';
  let data;
  try{
    const res=await fetch('/api/player/'+playerId);
    if(!res.ok) throw new Error(res.status);
    data=await res.json();
  }catch(e){
    body.innerHTML='<p class="empty">Solo disponible en la version servida '
      +'(<code>fantasy serve</code>).</p>';
    return;
  }
  const p=data.player, l=data.listing||{};
  const rival=p.next_rival?`${p.next_rival} (${p.next_home?'casa':'fuera'})`:'—';
  const owner=p.is_mine?'tu':(p.owner||'libre');
  body.innerHTML=`
    <h3>${p.name}</h3>
    <p class="sub"><span class="pos pos-${(p.position||'').toLowerCase().slice(0,3)}">${p.position}</span>
      ${p.team||''} · ${owner}${p.starred?' · ★':''}</p>
    <dl class="drawer-stats">
      <div><dt>Valor de mercado</dt><dd>${exact(p.value)}</dd></div>
      <div><dt>xPts por jornada</dt><dd>${(p.xpts||0).toFixed(2)}</dd></div>
      <div><dt>Puntos por millon</dt><dd>${(p.points_value||0).toFixed(3)}</dd></div>
      <div><dt>Score</dt><dd>${(p.score||0)>=0?'+':''}${(p.score||0).toFixed(2)}
        <span style="color:var(--muted);font-weight:400">· #${p.rank||'?'}</span></dd></div>
      <div><dt>Puntos 25/26</dt><dd>${p.last_season_points||0}</dd></div>
      <div><dt>Puntos temporada</dt><dd>${p.season_points||0}</dd></div>
      <div><dt>Titularidad</dt><dd class="${p.start_probability!=null?titClass(p.start_probability):''}"
        >${p.start_probability!=null?p.start_probability+'%':'—'}</dd></div>
      <div><dt>Proximo rival</dt><dd>${rival}</dd></div>
      <div><dt>Valor 7d</dt><dd style="color:var(--${(p.projected_pct||0)>=0?'pole-pos':'pole-neg'})"
        >${(p.projected_pct||0)>=0?'+':''}${(p.projected_pct||0).toFixed(2)}%</dd></div>
      <div><dt>Techo rentable</dt><dd${p.ideal_bid?'':' style="color:var(--warning)"'}
        >${p.ideal_bid?exact(p.ideal_bid):'sin margen'}</dd></div>
      <div><dt>Clausula</dt><dd>${p.clause?exact(p.clause):'—'}${p.clause_locked?' 🔒':''}</dd></div>
      ${l.market_id?`<div><dt>En mercado</dt><dd>${exact(l.min_bid)}</dd></div>`:''}
      ${l.kind==='libre'?`<div><dt>Pujas vigentes</dt><dd${l.bids?' style="color:var(--warning)"':''}>${l.bids||'ninguna'}</dd></div>`:''}
      ${l.expires?`<div><dt>Cierra</dt><dd>${String(l.expires).slice(11,16)}</dd></div>`:''}
      ${p.status&&p.status!=='ok'?`<div><dt>Estado</dt><dd style="color:var(--${
        p.status==='suspended'||p.status==='sanctioned'?'critical':'warning'})">${
        {injured:'lesionado',doubtful:'duda',suspended:'sancionado',
         sanctioned:'sancionado'}[p.status]||p.status}</dd></div>`:''}
      ${p.absence&&p.absence.reason?`<div style="grid-column:1/-1"><dt>Motivo</dt><dd
        style="text-align:left;font-weight:400">${p.absence.reason}${
        p.absence.since?' · '+p.absence.since:''}${
        p.absence.until?' · '+p.absence.until:''}</dd></div>`:''}
    </dl>
    ${sparkSvg(data.history||[])}
    <div class="drawer-actions">${(data.actions||[]).map(actionButton).join('')}</div>
    ${data.writes_enabled?'':'<p class="drawer-note">Servidor en modo solo lectura: '
      +'las operaciones estan desactivadas.</p>'}`;
  body.querySelectorAll('button[data-action]').forEach(button=>
    button.addEventListener('click',()=>runAction(JSON.parse(button.dataset.action),p)));
  wireAlways(body,p);
}

// El pie del panel dice en palabras que va a pasar: cambiar de "no vende solo" a
// "vendo desde X" es justo lo que hay que ver confirmado.
function note(panel,data){
  const line=panel.querySelector('.always-foot p');
  line.innerHTML = data.accept_above
    ? '<b>Vendo desde ese importe</b>, sin preguntar. El importe manda sobre el '
      +'interruptor de arriba.'
    : (data.auto_sell
        ? '<b>Vendo si llegan a tu precio de venta.</b> Si prefieres otro numero, '
          +'ponlo en «aceptar desde».'
        : 'No vende solo: si llega una oferta buena <b>te aviso</b> y decides tu.');
}

function wireAlways(scope,player){
  const panel=scope.querySelector('.always-panel');
  if(!panel) return;
  const min=panel.querySelector('.always-min');
  const accept=panel.querySelector('.always-accept');
  const save=panel.querySelector('.always-save');
  const auto=panel.querySelector('.always-auto');
  auto.addEventListener('change',async()=>{
    auto.disabled=true;
    try{
      const res=await fetch('/api/always',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({id:player.id,name:player.name,auto_sell:auto.checked})});
      if(!res.ok) throw new Error(res.status);
      const data=await res.json();
      auto.checked=!!data.auto_sell;
      note(panel,data);
    }catch(e){ auto.checked=!auto.checked; }
    finally{ auto.disabled=false; }
  });
  [min,accept].forEach(input=>input.addEventListener('input',()=>{
    const n=digits(input.value);
    input.value=isNaN(n)?'':group(n);
    save.disabled=false; save.textContent='Guardar';
  }));
  save.addEventListener('click',async()=>{
    save.disabled=true; save.textContent='Guardando…';
    try{
      const res=await fetch('/api/always',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({id:player.id,name:player.name,
                             min_price:digits(min.value)||0,
                             accept_above:digits(accept.value)||0})});
      if(!res.ok) throw new Error(res.status);
      const data=await res.json();
      min.value=data.min_price?group(data.min_price):'';
      accept.value=data.accept_above?group(data.accept_above):'';
      note(panel,{...data,auto_sell:auto.checked});
      save.textContent='Guardado'; save.classList.add('always-saved');
      setTimeout(()=>{save.classList.remove('always-saved');
                      save.textContent='Guardar'; save.disabled=false;},1600);
    }catch(e){
      save.textContent='No se ha guardado'; save.disabled=false;
    }
  });
}

function alwaysPanel(a){
  // Vacio = no vender solo. Es la unica forma de decirlo, asi que el placeholder
  // lo dice con palabras en vez de dejar un hueco que parece "sin limite".
  const min=a.min_price?group(a.min_price):'';
  const acc=a.accept_above?group(a.accept_above):'';
  const ask=a.asking||a.min_price||a.value||0;
  return `<div class="always-panel">
    <h4>Siempre en mercado</h4>
    <label class="always-check"><input type="checkbox" class="always-auto"
      ${a.auto_sell?'checked':''}>
      <span><b>Vender automaticamente</b> si alguien llega a lo que pides
      ${ask?`(${exact(ask)})`:''}<br>
      <i>para jugadores que te dan igual: si la oferta es minimamente buena, fuera</i></span>
    </label>
    <div class="always-grid">
      <label>Precio de listado
        <input class="always-min" type="text" inputmode="numeric" autocomplete="off"
               value="${min}" placeholder="${a.value?group(a.value):'valor de mercado'}"></label>
      <label>Aceptar desde
        <input class="always-accept" type="text" inputmode="numeric" autocomplete="off"
               value="${acc}" placeholder="no vendo solo"></label>
    </div>
    <div class="always-foot">
      <p>${acc?'<b>Vendo desde ese importe</b>, sin preguntar. El importe manda sobre el '
             +'interruptor de arriba.'
            :(a.auto_sell?'<b>Vendo si llegan a tu precio de venta.</b> Si prefieres otro '
                          +'numero, ponlo en «aceptar desde».'
                        :'No vende solo: si llega una oferta buena <b>te aviso</b> y decides tu.')}</p>
      <button class="always-save">Guardar</button>
    </div></div>`;
}

function actionButton(a){
  if(a.kind==='note') return `<p class="drawer-note">${a.label}</p>`;
  const cls=a.op==='decline_offer'||a.op==='withdraw' ? 'danger-full'
          : (a.op==='always'||a.op==='raid') ? (a.on?'on':'') : 'primary';
  const off=a.blocked?' disabled':'';
  const button=`<button class="${cls}" data-action='${JSON.stringify(a).replace(/'/g,"&#39;")}'${off}>`
    +`${a.label}${a.blocked?' — no te llega':''}</button>`;
  return a.op==='always'&&a.on ? button+alwaysPanel(a) : button;
}

async function runAction(a,player){
  if(a.op==='note') return;
  if(a.op==='raid'){
    const current=group(a.suggested);
    const answer=prompt('Clausulazo programado para '+player.name+'.\n\n'
      +'Se pagara en cuanto se libere la clausula, y SOLO si entonces sigue por debajo '
      +'del importe que pongas aqui. Si el dueño la sube o le pone blindaje, se cancela.\n\n'
      +'Pago maximo (€):', current);
    if(answer===null) return;
    const max_pay=digits(answer);
    if(!max_pay) return;
    const res=await fetch('/api/raid',{method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({id:player.id,name:player.name,max_pay})});
    if(res.ok) openDetail(player.id);
    return;
  }
  if(a.op==='always'){
    // Pintar antes de preguntar: el servidor confirma en milisegundos, pero volver a
    // cargar la ficha entera hacia que el boton pareciera muerto.
    const button=[...document.querySelectorAll('.drawer-actions button')]
      .find(b=>b.textContent.includes('mercado'));
    const turningOn=!a.on;
    if(button){
      button.classList.toggle('on',turningOn);
      button.textContent=turningOn?'Quitar de siempre-en-mercado':'Siempre en mercado';
      button.disabled=true;
    }
    try{
      const res=await fetch('/api/always',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({id:player.id,name:player.name})});
      if(!res.ok) throw new Error(res.status);
      const data=await res.json();
      if(button){
        button.classList.toggle('on',!!data.always_listed);
        button.textContent=data.always_listed?'Quitar de siempre-en-mercado'
                                             :'Siempre en mercado';
      }
      a.on=!!data.always_listed;
      const existing=document.querySelector('.always-panel');
      if(a.on&&!existing&&button){
        button.insertAdjacentHTML('afterend',alwaysPanel(a));
        wireAlways(button.parentElement,player);
      }else if(!a.on&&existing){ existing.remove(); }
    }catch(e){
      if(button){ button.classList.toggle('on',a.on);
        button.textContent=a.on?'Quitar de siempre-en-mercado':'Siempre en mercado'; }
    }finally{ if(button) button.disabled=false; }
    return;
  }
  closeDrawer();
  if(a.kind==='amount'){
    pending={operation:a.op, market_id:a.market_id, player_id:a.player_id||player.id,
             player_team_id:a.player_team_id||player.player_team_id,
             offer_id:a.offer_id, name:player.name, min_bid:a.min||0,
             ideal:player.ideal_bid||0, value:player.value};
    modal.hidden=false;
    modal.querySelector('.bid-action').textContent=a.label+' —';
    modal.querySelector('.bid-who').textContent=player.name;
    modal.querySelector('.bid-amount').value=group(a.suggested||a.min||0);
    modal.querySelector('.bid-min').textContent=a.min?exact(a.min):'sin minimo';
    modal.querySelector('.bid-ideal').textContent=player.ideal_bid?exact(player.ideal_bid):'sin margen';
    modal.querySelector('.bid-value').textContent=exact(player.value);
    showRivals(+a.bids||0, a.expires);
    modal.querySelector('.bid-drop').hidden=true;
    showStep(1);
    modal.querySelector('.bid-error').textContent='';
    checkAmount();
  }else{
    const button=document.createElement('button');
    button.className='op'; button.dataset.op=a.op;
    button.dataset.opMarket=a.market_id||''; button.dataset.opOffer=a.offer_id||'';
    button.dataset.opPlayer=a.player_id||player.id; button.dataset.opName=player.name;
    button.dataset.opAmount=a.amount||'';
    document.body.appendChild(button); wireOps(document.body); button.click();
    button.remove();
  }
}

async function scheduleRaid(dataset){
  const suggested=group(+dataset.raidMax||0);
  const clause=+dataset.raidClause||0;
  const answer=prompt('Clausulazo programado para '+dataset.raidName+'.\n\n'
    +'Se pagara SOLO en el momento en que la clausula se libere, y solo si entonces '
    +'sigue por debajo del importe que pongas aqui.\n'
    +(clause?('Clausula ahora: '+clause.toLocaleString('es-ES')+' €\n'):'')
    +'Si el dueño la sube por encima de tu limite, o blinda al jugador, se cancela '
    +'sola y no se paga nada.\n\n'
    +'Pago maximo (€):', suggested);
  if(answer===null) return;
  const max_pay=digits(answer);
  if(!max_pay) return;
  const res=await fetch('/api/raid',{method:'POST',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify({id:dataset.raid,name:dataset.raidName,max_pay})});
  if(!res.ok){ alert('No se ha podido programar.'); return; }
  alert(dataset.raidName+': programado con limite '+max_pay.toLocaleString('es-ES')+' €.\n'
    +'Se ejecutara solo si el servidor corre con --auto.');
  swap();
}

function wireRaids(root=document){
  root.querySelectorAll('button.raid-btn, .cal-chip[data-raid]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>scheduleRaid(button.dataset));
  });
  // Tus propios chips del calendario no se clausulan: abren su ficha.
  root.querySelectorAll('.cal-chip:not([data-raid])').forEach(chip=>{
    if(chip.dataset.wired) return;
    chip.dataset.wired='1';
    chip.addEventListener('click',()=>openDetail(chip.dataset.detailAlt));
  });
  root.querySelectorAll('button[data-goto]').forEach(card=>{
    if(card.dataset.wired) return;
    card.dataset.wired='1';
    card.addEventListener('click',()=>{
      const target=resolveTarget(card.dataset.goto);
      if(target) showTab(target.tab,{section:target.section});
      else showTab(card.dataset.goto);
    });
  });
}

function wireDetails(root=document){
  root.querySelectorAll('button[data-detail]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>openDetail(button.dataset.detail));
  });
}

if(drawer){
  drawer.querySelector('.drawer-close').addEventListener('click',closeDrawer);
  drawer.addEventListener('click',(e)=>{ if(e.target===drawer) closeDrawer(); });
  document.addEventListener('keydown',(e)=>{ if(e.key==='Escape') closeDrawer(); });
}

// ---- pestañas: una vista a la vez ------------------------------------------
const TABS=[
  {id:'decidir', label:'Decidir', sections:['acciones','ofertas']},
  {id:'mercado', label:'Mercado', sections:['fichajes','enventa','misventas','siempre','seguimiento']},
  {id:'clausulas', label:'Cláusulas', sections:['programados','calendario','vencimientos','oportunidades','riesgo','clausulas']},
  {id:'plantilla', label:'Plantilla', sections:['once','plantilla','ventas']},
  {id:'liga', label:'Liga', sections:['rivales','movimientos']},
  {id:'ranking', label:'Ranking', sections:['ranking','rentabilidad']},
];

// Un hash puede ser una pestaña (#mercado) o una seccion (#oportunidades): lo
// segundo es lo que hay en los enlaces, asi que hay que resolverlo a su pestaña.
function resolveTarget(hash){
  const id=(hash||'').replace(/^#/,'');
  if(!id) return null;
  if(TABS.some(t=>t.id===id)) return {tab:id, section:null};
  const owner=TABS.find(t=>t.sections.includes(id));
  return owner ? {tab:owner.id, section:id} : null;
}

function showTab(id,{section=null,updateHash=true}={}){
  const tab=TABS.find(t=>t.id===id)||TABS[0];
  document.querySelectorAll('section[id]').forEach(s=>{
    s.hidden=!tab.sections.includes(s.id);
  });
  document.querySelectorAll('.tab').forEach(b=>{
    const on=b.dataset.tab===tab.id;
    b.classList.toggle('on',on);
    b.setAttribute('aria-selected',on?'true':'false');
  });
  try{ localStorage.setItem('fantasy-tab',tab.id); }catch(e){}
  applyFilters();
  if(updateHash){
    // replaceState, no assignment: no queremos una entrada de historial por clic ni
    // disparar hashchange sobre nosotros mismos.
    history.replaceState(null,'','#'+(section||tab.id));
  }
  if(tab.sections.includes('once') && !pitchState) loadPitch();
  if(section){
    const node=document.getElementById(section);
    if(node) node.scrollIntoView({behavior:'smooth',block:'start'});
  }
}

function wireTabs(){
  const bar=document.getElementById('tabs');
  if(!bar||bar.dataset.wired) return;
  bar.dataset.wired='1';
  bar.querySelectorAll('.tab').forEach(b=>
    b.addEventListener('click',()=>showTab(b.dataset.tab)));
  window.addEventListener('hashchange',()=>{
    const target=resolveTarget(location.hash);
    if(target) showTab(target.tab,{section:target.section,updateHash:false});
  });
  let saved=null;
  try{ saved=localStorage.getItem('fantasy-tab'); }catch(e){}
  const target=resolveTarget(location.hash);
  if(target) showTab(target.tab,{section:target.section,updateHash:false});
  else showTab(saved||'decidir');
}

// ---- push: recambiar solo lo que cambia -----------------------------------
let currentVersion=null;

// Secciones cuyo contenido lo pinta el navegador, no el servidor: el fragmento que
// llega es una carcasa vacia, asi que recambiarla borraria lo que hay dentro (y los
// cambios de alineacion sin guardar).
const CLIENT_OWNED=new Set(['once']);

async function swap(){
  const res=await fetch('/api/fragments');
  if(!res.ok) return;
  const data=await res.json();
  if(data.version===currentVersion) return;
  currentVersion=data.version;
  Object.entries(data.sections).forEach(([id,inner])=>{
    if(CLIENT_OWNED.has(id)) return;
    const node=document.getElementById(id);
    if(node && node.innerHTML!==inner) node.innerHTML=inner;
  });
  wireTables(); wireFilters(); wireStars(); wireBids(); wireOps(); wireDetails(); wireRaids(); tick();
  showTab(document.querySelector('.tab.on')?.dataset.tab||'decidir',
          {updateHash:false});
  const stamp=document.getElementById('live-stamp');
  if(stamp) stamp.textContent='actualizado '+new Date().toLocaleTimeString('es-ES');
}

const EFFECT_LABELS={cash:'Saldo',squad:'Jugadores',squad_value:'Valor de la plantilla',
                     listed:'En el mercado',offers:'Ofertas recibidas',
                     points:'Puntos de la plantilla',absences:'Bajas'};
const OPERATION_LABELS={sell_to_market:'Puesto en venta',accept_offer:'Oferta aceptada',
  decline_offer:'Oferta rechazada',withdraw:'Retirado del mercado',bid:'Puja enviada',
  modify_bid:'Puja modificada',cancel_bid:'Puja cancelada',direct_offer:'Oferta directa',
  pay_clause:'Clausulazo pagado',raise_clause:'Clausula subida',
  save_lineup:'Alineacion guardada',policy:'Instruccion ejecutada',
  traspaso:'Se ha movido la liga',mercado:'Cambios en el mercado',
  partido:'Partido en juego',
  vencimiento:'Ha vencido algo',refresco:'Actualizado'};

function showEffect(message){
  // Lo que una operacion mueve de verdad: el antes y el despues, no un "hecho".
  const rows=Object.entries(message.changed||{}).map(([key,change])=>{
    const money=key==='cash'||key==='squad_value';
    const fmt=(n)=> money?exact(n||0):String(n??0);
    const worse=key==='absences';
    const sign=change.delta>0?(worse?'down':'up'):(change.delta<0?(worse?'up':'down'):'');
    return `<tr><th>${EFFECT_LABELS[key]||key}</th><td>${fmt(change.before)}</td>`
      +`<td class="arrow">→</td><td>${fmt(change.after)}</td>`
      +`<td class="delta ${sign}">${change.delta>0?'+':''}${fmt(change.delta)}</td></tr>`;
  }).join('');
  if(!rows) return;
  const box=document.createElement('div');
  box.className='effect';
  box.innerHTML=`<button class="effect-close" aria-label="Cerrar">×</button>`
    +`<h4>${OPERATION_LABELS[message.operation]||message.operation}</h4>`
    +`<table>${rows}</table>`;
  document.body.appendChild(box);
  box.querySelector('.effect-close').addEventListener('click',()=>box.remove());
  requestAnimationFrame(()=>box.classList.add('in'));
  setTimeout(()=>{box.classList.remove('in');setTimeout(()=>box.remove(),400);},12000);
}

function connect(){
  const dot=document.getElementById('live-dot');
  const source=new EventSource('/api/events');
  source.onopen=()=>{ if(dot){ dot.className='live-on'; dot.title='En vivo'; } };
  source.onmessage=(event)=>{
    const message=JSON.parse(event.data);
    if(message.type==='effect'){
      showEffect(message);
      if(message.version!==currentVersion) swap();
      return;
    }
    if(message.type==='state'||message.type==='hello'){
      if(message.version!==currentVersion) swap();
    }
  };
  source.onerror=()=>{
    if(dot){ dot.className='live-off'; dot.title='Sin conexion: reintentando'; }
    source.close(); setTimeout(connect,5000);
  };
}

wireTables(); wireFilters(); wireStars(); wireBids(); wireOps(); wireDetails(); wireRaids();
wireTabs(); tick();
if(window.EventSource && location.protocol.startsWith('http')) connect();

// ---- legacy (fichero estatico) ----
"""

FILTERS = """
<div class="filters">
  <label>Posicion
    <select class="f-pos">
      <option value="all">todas</option>
      <option value="POR">portero</option>
      <option value="DEF">defensa</option>
      <option value="MED">medio</option>
      <option value="DEL">delantero</option>
    </select>
  </label>
  <label>Precio maximo (&euro;)
    <input class="f-price" type="number" step="1000000" min="0" placeholder="sin limite">
  </label>
  <label>Buscar
    <input class="f-text" type="search" placeholder="nombre">
  </label>
  <button class="f-reset" type="button">Limpiar</button>
  <span class="f-count kpi-label"></span>
</div>
"""


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

    # Only link to sections this run actually renders: without a session most of
    # them do not exist, and a chip that jumps nowhere is worse than no chip.
    links = [("movimientos", "Movimientos"), ("ranking", "Ranking"),
             ("rentabilidad", "Rentabilidad")]
    if advice:
        links = [("acciones", "Que hacer"), ("fichajes", "Pujar ahora"),
                 ("ofertas", "Ofertas"), ("enventa", "En venta"), ("calendario", "Calendario"), ("vencimientos", "Mis cláusulas"),
                 ("oportunidades", "Cláusulas rivales"), ("movimientos", "Movimientos"),
                 ("plantilla", "Mi plantilla"), ("ventas", "Vender"),
                 ("riesgo", "Riesgo"), ("rivales", "Rivales"),
                 ("ranking", "Ranking"), ("rentabilidad", "Rentabilidad")]
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


PITCH = '''<section id="once">
  <h2>Alineación<span class="badge-count" id="pitch-formation"></span></h2>
  <p class="note">Arrastra un jugador del campo al banquillo o al revés para cambiarlo.
    Cambia la formación con el selector y las plazas se ajustan solas. Debajo de cada
    uno: puntos por jornada, tendencia de valor y xPts esperados.</p>
  <div class="pitch-bar">
    <label>Formación <select id="pitch-formation-select"></select></label>
    <span id="pitch-status" class="kpi-label"></span>
    <button id="pitch-reset" type="button">Descartar cambios</button>
    <button id="pitch-save" class="primary" type="button" disabled>Guardar alineación</button>
  </div>
  <div class="pitch-wrap">
    <div class="pitch" id="pitch"></div>
    <aside class="bench" id="bench">
      <h3>Banquillo</h3>
      <div class="bench-list" id="bench-list"></div>
    </aside>
  </div>
</section>'''


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
