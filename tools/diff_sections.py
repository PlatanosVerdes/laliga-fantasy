#!/usr/bin/env python3
"""Compare rendered sections, byte for byte, from the same rows.

The rows are handed to both implementations rather than read from a live model on each
side: otherwise a difference could be two data reads disagreeing rather than two renderers,
and the point of a byte comparison is that it has exactly one explanation.

Three kinds of comparison, because the page has three kinds of section:

* **tables** — most of them, compared whole, and again with zero rows, because the empty
  state is output too and each section words it differently;
* **shapes** — the calendar and the movements feed, which are not tables at all: a calendar
  answers *when does the league open up* and a feed is a list of sentences;
* **synthetic rows** — for sections the current world has none of. A bucket can be
  legitimately empty (no clause exposure today is good news, and every clause in the league
  being locked is normal in August) but the renderer still has to be compared, so plausible
  rows do the job and the line says they are made up.

    usage: diff_sections.py [path-to-fantasy-go] [path-to-report.json]
"""
from __future__ import annotations

import difflib
import json
import re
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from fantasy import policies, report  # noqa: E402

# (name, bucket of the advice it renders, whether the filter bar acts on it)
TABLES = [
    ("plantilla", "squad", False),
    ("mercado", "bids_now", True),
    ("misventas", "my_listings", False),
    ("seguimiento", "watchlist", False),
    ("ventas", "sells", False),
    ("riesgo", "exposure", False),
    ("enventa", "asks", True),
    ("clausulas", "raids", False),
    ("ofertas", "offers", False),
    ("vencimientos", "my_clauses_soon", False),
    ("proximas", "upcoming_raids", False),
    ("rivales", "rivals", False),
    # No bucket of their own: the advice and policy layers compose these, so they are built.
    ("acciones", None, False),
    ("siempre", None, False),
    ("programados", None, False),
]

# The empty state is not a default: it is a sentence per section.
EMPTY = {
    "riesgo": "Sin exposicion relevante",
    "enventa": "Nadie ha puesto a nadie en venta",
    "clausulas": "Ninguna cláusula a tu alcance",
    "vencimientos": "Ninguna se desbloquea en los proximos 10 dias.",
    "proximas": "Ninguna cláusula interesante se abre en los proximos 10 dias.",
}

# Tables Python builds without a section marker, so their rows still say "mio": unowned
# players and rivals'. A row that is somehow yours in one of those is worth flagging.
NO_SECTION = {"seguimiento", "enventa", "clausulas", "proximas", "rivales", "acciones",
              "programados"}

RAID_PLAN_STATUS = {"pagar_clausula": "good", "esperando": "neutral", "cancelada": "warning",
                    "bloqueada": "critical", "sin_saldo": "warning", "ninguna": "neutral"}


# --- the columns, exactly as report.py builds them --------------------------------------

def player_columns(cost_label=None, extra=()):
    return report._player_columns(cost_label=cost_label, extra=extra)


def clause_columns():
    return [
        ("Se abre en", lambda r: r, "hours"),
        ("Fecha", lambda r: str(r.get("unlock_at") or "")[:16].replace("T", " "), "text"),
        ("Jugador", lambda r: r, "player"),
        ("Valor", lambda r: r["value"], "money"),
        ("Cláusula", lambda r: r.get("clause"), "money"),
        ("xPts/j", lambda r: r["xpts"], "num"),
        ("Score", lambda r: r["score"], "num"),
    ]


def columns_for(name: str):
    if name in ("plantilla", "seguimiento"):
        return player_columns()

    if name == "mercado":
        columns = player_columns(cost_label="Puja minima")
        columns.insert(4, ("Puja max. rentable", lambda r: r.get("ideal_bid"), "ideal"))
        return columns

    if name == "enventa":
        columns = player_columns(cost_label="Pide")
        columns.insert(2, ("Vende", lambda r: r.get("seller"), "text"))
        columns.insert(5, ("Sobre valor", lambda r: r.get("ask_ratio"), "ratio"))
        columns.append(("", lambda r: r, "bid"))
        return columns

    if name == "clausulas":
        columns = player_columns(cost_label="Cláusula")
        columns.insert(1, ("Dueño", lambda r: r.get("owner"), "text"))
        columns.insert(4, ("x valor", lambda r: r.get("clause_premium"), "num"))
        columns.append(("Clausulazo", lambda r: r, "raid"))
        return columns

    if name == "ventas":
        return player_columns(extra=[("Motivos", lambda r: r.get("reasons"), "list")])

    if name == "misventas":
        return [
            ("Jugador", lambda r: r, "player"),
            ("Valor", lambda r: r["value"], "money"),
            ("Pides", lambda r: r.get("entry_cost"), "money"),
            ("Sobre valor", lambda r: r.get("ask_ratio"), "ratio"),
            ("xPts/j", lambda r: r["xpts"], "num"),
            ("Valor 7d", lambda r: r.get("projected_pct"), "pct"),
        ]

    if name == "ofertas":
        return [
            ("Jugador", lambda r: r, "player"),
            ("Valor", lambda r: r["value"], "money"),
            ("Pides", lambda r: r.get("ask"), "money"),
            ("Te ofrecen", lambda r: r.get("offer_amount"), "money"),
            ("Sobre su valor", lambda r: r.get("vs_value"), "ratio_sell"),
            ("Ofertas", lambda r: r.get("offer_count"), "int"),
            ("Caduca", lambda r: str(r.get("offer_expires") or "")[:16].replace("T", " "),
             "text"),
            ("xPts/j", lambda r: r["xpts"], "num"),
            ("", lambda r: r, "offer"),
        ]

    if name == "riesgo":
        return [
            ("Jugador", lambda r: r, "player"),
            ("Valor", lambda r: r["value"], "money"),
            ("Cláusula", lambda r: r.get("clause"), "money"),
            ("x valor", lambda r: r.get("clause_margin"), "num"),
            ("Rivales que pueden", lambda r: r.get("threats"), "int"),
            ("El mas rico", lambda r: r.get("top_threat"), "text"),
            ("xPts/j", lambda r: r["xpts"], "num"),
            ("Score", lambda r: r["score"], "num"),
        ]

    if name == "vencimientos":
        return clause_columns()

    if name == "proximas":
        columns = clause_columns()
        columns.insert(3, ("Dueño", lambda r: r.get("owner"), "text"))
        columns.insert(0, ("¿Renta?", lambda r: r, "verdict_raid"))
        columns.append(("x valor", lambda r: r.get("clause_premium"), "num"))
        columns.append(("Pts/M pagando", lambda r: r.get("ppm_at_clause"), "mag"))
        columns.append(("Techo futbolfantasy", lambda r: r.get("ideal_bid"), "ideal"))
        columns.append(("Clausulazo", lambda r: r, "raid"))
        return columns

    if name == "rivales":
        return [
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

    if name == "acciones":
        return [
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

    if name == "siempre":
        # This one reads the policies file, which the Go renderer does not: the caller pastes
        # the two numbers onto each row instead, so the section stays a pure function of its
        # input.
        entries = policies.load()
        return [
            ("Jugador", lambda r: r.get("name"), "text"),
            ("Accion", lambda r: r.get("action").replace("_", " "), "text"),
            ("Importe", lambda r: r.get("amount"), "money"),
            ("Precio minimo",
             lambda r: (entries.get(r["player_id"]) or {}).get("min_price"), "money"),
            ("Acepto desde",
             lambda r: (report._fmt_money((entries.get(r["player_id"]) or {}).get("accept_above"))
                        if (entries.get(r["player_id"]) or {}).get("accept_above")
                        else "no vendo solo"), "text"),
            ("Motivo", lambda r: r.get("why"), "text"),
            ("Resultado", lambda r: r.get("result") or "pendiente", "text"),
        ]

    if name == "programados":
        return [
            ("Jugador", lambda r: r.get("name"), "text"),
            ("Dueño", lambda r: r.get("owner"), "text"),
            ("Cláusula", lambda r: r.get("clause"), "money"),
            ("Mi limite", lambda r: r.get("max_pay"), "money"),
            ("Estado", lambda r: (r.get("action"), RAID_PLAN_STATUS.get(r.get("action"))),
             "status"),
            ("Motivo", lambda r: r.get("why"), "text"),
        ]

    raise SystemExit(f"seccion sin columnas: {name}")


# --- rows for the sections the world has none of ----------------------------------------

def build_rows(name: str, blob: dict) -> list[dict]:
    advice = blob.get("advice") or {}
    universe = blob.get("universe") or {}
    base = (advice.get("squad") or [])[:5]

    if name == "riesgo":
        return [{**row,
                 "clause": (row.get("value") or 0) * 1.8,
                 "clause_margin": 1.8,
                 "threats": index,
                 "top_threat": ["La rataneta", None, "TheMessias"][index % 3]}
                for index, row in enumerate(base[:3])]

    if name == "clausulas":
        # A rival's player with the clause open, and one of them shielded, because
        # "blindado" is a branch of the raid button rather than a disabled state.
        return [{**row,
                 "is_mine": False,
                 "owner": ["La rataneta", "TheMessias", "Villaone"][index % 3],
                 "entry_cost": (row.get("value") or 0) * 1.5,
                 "clause": (row.get("value") or 0) * 1.5,
                 "clause_premium": 1.5,
                 "shielded": index == 1,
                 "raid_scheduled": index == 2,
                 "max_pay": 0}
                for index, row in enumerate(base[:3])]

    if name == "acciones":
        # The composition — which player earns which verdict — belongs to the advice layer,
        # which is not ported. One row per verdict exercises all five badges.
        verdicts = ["out", "buy", "clause", "protect", "sell"]
        whys = ["ya no juega", "renta a ese precio", "clausula abierta",
                "quedas expuesto", "score bajo; valor cayendo"]
        return [{**row, "verdict": verdicts[index % 5], "why": whys[index % 5],
                 "entry_cost": None if index % 2 else (row.get("value") or 0) * 1.1}
                for index, row in enumerate(base)]

    if name == "siempre":
        # The real plan, so this is what the page would actually say today.
        plan = policies.plan(universe.get("players") or [])
        entries = policies.load()
        return [{**action,
                 "policy_min_price": (entries.get(action["player_id"]) or {}).get("min_price"),
                 "policy_accept_above":
                     (entries.get(action["player_id"]) or {}).get("accept_above")}
                for action in plan]

    if name == "programados":
        # The six states a scheduled raid can be in, because the pill is the point of the
        # table: "cancelada" is a warning and not an error — standing down because the
        # clause rose is the instruction working.
        return [{"player_id": str(index), "name": f"Jugador {index}",
                 "owner": "La rataneta", "clause": 10_000_000 + index * 1_000_000,
                 "max_pay": 12_000_000, "action": state, "why": f"motivo de {state}"}
                for index, state in enumerate(RAID_PLAN_STATUS)]

    return []


# --- the comparisons ---------------------------------------------------------------------

def render_python(name: str, rows: list[dict], filterable: bool) -> str:
    section = "" if name in NO_SECTION else name
    extra = {"empty": EMPTY[name]} if name in EMPTY else {}
    return report._table(columns_for(name), rows, section=section, filterable=filterable,
                         **extra)


def render_go(binary: str, name: str, rows: list[dict], scratch: Path,
              extra: list[str] = ()) -> str:
    path = scratch / f"{name}.json"
    path.write_text(json.dumps(rows))
    return subprocess.run([binary, "section", name, str(path), *extra],
                          capture_output=True, text=True, cwd=ROOT).stdout


def show_difference(name: str, python: str, go: str) -> None:
    print(f"  DISTINTA  {name}  py={len(python)} go={len(go)}")
    shown = 0
    for tag, i1, i2, j1, j2 in difflib.SequenceMatcher(None, python, go).get_opcodes():
        if tag == "equal" or shown >= 4:
            continue
        print(f"      py={python[i1:i2][:110]!r}")
        print(f"      go={go[j1:j2][:110]!r}")
        shown += 1


def compare_tables(binary: str, blob: dict, scratch: Path) -> int:
    advice = blob.get("advice") or {}
    failures = 0
    for name, bucket, filterable in TABLES:
        rows = (advice.get(bucket) or []) if bucket else []
        origin = ""
        if not rows:
            rows = build_rows(name, blob)
            origin = " (filas sinteticas)" if bucket else ""
        if not rows:
            print(f"  {name}: sin filas y sin manera de fabricarlas")
            failures += 1
            continue

        python = render_python(name, rows, filterable)
        go = render_go(binary, name, rows, scratch)
        if python == go:
            print(f"  ok  {name:12} {len(rows):3} filas · {len(python):>6} bytes "
                  f"identicos{origin}")
        else:
            failures += 1
            show_difference(name, python, go)
    return failures


def compare_shapes(binary: str, blob: dict, scratch: Path) -> int:
    universe, advice = blob.get("universe") or {}, blob.get("advice") or {}
    clauses = universe.get("clauses") or {}
    entries = (clauses.get("mine") or []) + (clauses.get("rivals") or [])
    events = blob.get("activity") or []

    cases = [
        ("calendario", entries, [str(int(advice.get("spending_power") or 0))],
         report._calendar_section(universe, advice),
         r'(<div class="cal">.*?</div></div></div>)'),
        ("movimientos", events, [], report._activity_section(events),
         r'<p class="note">.*?</p>(.*)</section>$'),
    ]

    failures = 0
    print("\n  formas (no son tablas):")
    for name, rows, extra, whole, pattern in cases:
        found = re.search(pattern, whole, re.S)
        python = found.group(1) if found else whole
        go = render_go(binary, name, rows, scratch, extra)
        if python == go:
            print(f"    ok  {name:12} {len(rows):3} filas · {len(python):>6} bytes identicos")
        else:
            failures += 1
            show_difference(name, python, go)
    return failures


def compare_empty(binary: str, scratch: Path) -> int:
    failures = 0
    print("\n  con cero filas:")
    for name, _bucket, filterable in TABLES:
        python = render_python(name, [], filterable)
        go = render_go(binary, name, [], scratch)
        if python == go:
            print(f"    ok  {name:12} {python}")
        else:
            failures += 1
            show_difference(name, python, go)
    return failures


def main() -> int:
    binary = sys.argv[1] if len(sys.argv) > 1 else "./fantasy-go"
    dump = Path(sys.argv[2]) if len(sys.argv) > 2 else ROOT / "data" / "report.json"
    if not Path(binary).exists():
        print(f"no encuentro el binario de Go en {binary}")
        return 2
    if not dump.exists():
        print(f"no encuentro el volcado en {dump}: generalo con "
              f"`python3 fantasy.py report --json`")
        return 2

    blob = json.loads(dump.read_text(encoding="utf-8"))
    scratch = Path(tempfile.mkdtemp())

    failures = compare_tables(binary, blob, scratch)
    failures += compare_shapes(binary, blob, scratch)
    failures += compare_empty(binary, scratch)

    print(f"\n{'VERDE' if not failures else 'ROJO'}: {len(TABLES)} tablas y 2 formas, "
          f"{failures} discrepancias")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
