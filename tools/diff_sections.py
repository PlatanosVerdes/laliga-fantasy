#!/usr/bin/env python3
"""Compare rendered sections, byte for byte, from the same rows.

The rows are handed to both implementations rather than read from a live model on each
side: otherwise a difference could be two data reads disagreeing rather than two renderers,
and the point of a byte comparison is that it has exactly one explanation.

Byte-identical is a strong claim and a cheap one to check, which is why it is the bar here.
The primitives are pinned by diff_render.py; this puts them together into the tables the
page is actually made of.

    usage: diff_sections.py [path-to-fantasy-go] [path-to-report.json]
"""
from __future__ import annotations

import difflib
import json
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from fantasy import report  # noqa: E402

# (name, which bucket of the advice, whether the filter bar acts on it)
SECTIONS = [
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
    # No bucket of its own: the advice layer composes these rows, so the harness makes them.
    ("acciones", "__sinteticas__", False),
]

# The empty-state text is part of the output, so it belongs to the spec and not to a
# default: "Sin exposicion relevante" is what the risk table says when there is none.
EMPTY = {"riesgo": "Sin exposicion relevante",
         "enventa": "Nadie ha puesto a nadie en venta",
         "clausulas": "Ninguna cláusula a tu alcance",
         "vencimientos": "Ninguna se desbloquea en los proximos 10 dias.",
         "proximas": "Ninguna cláusula interesante se abre en los proximos 10 dias."}


def columns_for(name: str):
    """The same columns Python's build uses for that section, in the same order."""
    if name == "plantilla":
        return report._player_columns()
    if name == "mercado":
        columns = report._player_columns(cost_label="Puja minima")
        columns.insert(4, ("Puja max. rentable", lambda row: row.get("ideal_bid"), "ideal"))
        return columns
    if name == "misventas":
        return [
            ("Jugador", lambda r: r, "player"),
            ("Valor", lambda r: r["value"], "money"),
            ("Pides", lambda r: r.get("entry_cost"), "money"),
            ("Sobre valor", lambda r: r.get("ask_ratio"), "ratio"),
            ("xPts/j", lambda r: r["xpts"], "num"),
            ("Valor 7d", lambda r: r.get("projected_pct"), "pct"),
        ]
    if name == "seguimiento":
        return report._player_columns()
    if name == "ventas":
        return report._player_columns(extra=[("Motivos", lambda r: r.get("reasons"), "list")])
    if name == "enventa":
        columns = report._player_columns(cost_label="Pide")
        columns.insert(2, ("Vende", lambda r: r.get("seller"), "text"))
        columns.insert(5, ("Sobre valor", lambda r: r.get("ask_ratio"), "ratio"))
        columns.append(("", lambda r: r, "bid"))
        return columns
    if name == "clausulas":
        columns = report._player_columns(cost_label="Cláusula")
        columns.insert(1, ("Dueño", lambda r: r.get("owner"), "text"))
        columns.insert(4, ("x valor", lambda r: r.get("clause_premium"), "num"))
        columns.append(("Clausulazo", lambda r: r, "raid"))
        return columns
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
    if name in ("vencimientos", "proximas"):
        columns = [
            ("Se abre en", lambda r: r, "hours"),
            ("Fecha", lambda r: str(r.get("unlock_at") or "")[:16].replace("T", " "), "text"),
            ("Jugador", lambda r: r, "player"),
            ("Valor", lambda r: r["value"], "money"),
            ("Cláusula", lambda r: r.get("clause"), "money"),
            ("xPts/j", lambda r: r["xpts"], "num"),
            ("Score", lambda r: r["score"], "num"),
        ]
        if name == "vencimientos":
            return columns
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
    raise SystemExit(f"seccion sin columnas: {name}")


def synthetic(name: str, advice: dict) -> list[dict]:
    """Rows for a section the current world happens to have none of.

    A bucket can be legitimately empty — no clause exposure today is good news, and every
    clause in the league being locked is normal early in the season — but the renderer still
    has to be compared. What is under test is the HTML, so plausible rows do the job as long
    as the line says they are made up.
    """
    base = (advice.get("squad") or [])[:3]
    if not base:
        return []

    if name in ("vencimientos", "proximas"):
        columns = [
            ("Se abre en", lambda r: r, "hours"),
            ("Fecha", lambda r: str(r.get("unlock_at") or "")[:16].replace("T", " "), "text"),
            ("Jugador", lambda r: r, "player"),
            ("Valor", lambda r: r["value"], "money"),
            ("Cláusula", lambda r: r.get("clause"), "money"),
            ("xPts/j", lambda r: r["xpts"], "num"),
            ("Score", lambda r: r["score"], "num"),
        ]
        if name == "vencimientos":
            return columns
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
    if name == "riesgo":
        return [{**row,
                 "clause": (row.get("value") or 0) * 1.8,
                 "clause_margin": 1.8,
                 "threats": index,
                 "top_threat": ["La rataneta", None, "TheMessias"][index % 3]}
                for index, row in enumerate(base)]

    if name == "acciones":
        # The composition — which player earns which verdict — belongs to the advice layer,
        # which is not ported. What is under test here is the rendering, so one row per
        # verdict exercises all five badges and the order they sort in.
        verdicts = ["out", "buy", "clause", "protect", "sell"]
        base5 = (advice.get("squad") or [])[:5]
        return [{**row, "verdict": verdicts[index % len(verdicts)],
                 "why": ["ya no juega", "renta a ese precio", "clausula abierta",
                         "quedas expuesto", "score bajo; valor cayendo"][index % 5],
                 "entry_cost": None if index % 2 else (row.get("value") or 0) * 1.1}
                for index, row in enumerate(base5)]

    if name == "clausulas":
        # A rival's player with the clause open: not mine, owned, and one of them shielded,
        # because "blindado" is a branch of the raid button.
        return [{**row,
                 "is_mine": False,
                 "owner": ["La rataneta", "TheMessias", "Villaone"][index % 3],
                 "entry_cost": (row.get("value") or 0) * 1.5,
                 "clause": (row.get("value") or 0) * 1.5,
                 "clause_premium": 1.5,
                 "shielded": index == 1,
                 "raid_scheduled": index == 2,
                 "max_pay": 0}
                for index, row in enumerate(base)]

    return []


def compare_empty(binary: str, scratch: Path) -> int:
    """The empty state is output too, and every section words it differently."""
    path = scratch / "empty.json"
    path.write_text("[]")
    failures = 0
    print("\n  con cero filas:")
    for name, _bucket, filterable in SECTIONS:
        python = report._table(columns_for(name), [], section=name, filterable=filterable,
                              **({"empty": EMPTY[name]} if name in EMPTY else {}))
        go = subprocess.run([binary, "section", name, str(path)],
                            capture_output=True, text=True, cwd=ROOT).stdout
        if python == go:
            print(f"    ok  {name:12} {python}")
        else:
            failures += 1
            print(f"    DISTINTO  {name}\n        py={python}\n        go={go}")
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

    advice = json.loads(dump.read_text(encoding="utf-8")).get("advice") or {}
    scratch = Path(tempfile.mkdtemp())
    failures = 0

    for name, bucket, filterable in SECTIONS:
        rows = advice.get(bucket) or []
        origin = ""
        if not rows:
            # A bucket can legitimately be empty — no clause exposure today is good news —
            # but the renderer still has to be compared. Synthetic rows do that job: what
            # is under test is the HTML, and plausible input exercises the same code.
            rows = synthetic(name, advice)
            origin = " (filas sinteticas)"
        if not rows:
            print(f"  {name}: sin filas y sin manera de fabricarlas")
            failures += 1
            continue

        path = scratch / f"{name}.json"
        path.write_text(json.dumps(rows))
        # `seguimiento` is the one table Python builds without a section marker, so its
        # rows still say "mio" — the players are unowned, and a row that is somehow yours
        # there is worth flagging.
        # Three tables Python builds without a section marker, so their rows still say
        # "mio": seguimiento (unowned players), enventa and clausulas (rivals'). A row
        # that is somehow yours in one of those is worth flagging.
        section = "" if name in ("seguimiento", "enventa", "clausulas") else name
        python = report._table(columns_for(name), rows,
                              section=section, filterable=filterable,
                              **({"empty": EMPTY[name]} if name in EMPTY else {}))
        go = subprocess.run([binary, "section", name, str(path)],
                            capture_output=True, text=True, cwd=ROOT).stdout

        if python == go:
            print(f"  ok  {name:12} {len(rows):3} filas · {len(python):>6} bytes "
                  f"identicos{origin}")
            continue

        failures += 1
        print(f"  DISTINTA  {name}  py={len(python)} go={len(go)}")
        matcher = difflib.SequenceMatcher(None, python, go)
        shown = 0
        for tag, i1, i2, j1, j2 in matcher.get_opcodes():
            if tag == "equal" or shown >= 4:
                continue
            print(f"      py={python[i1:i2][:110]!r}")
            print(f"      go={go[j1:j2][:110]!r}")
            shown += 1

    failures += compare_empty(binary, scratch)

    print(f"\n{'VERDE' if not failures else 'ROJO'}: {len(SECTIONS)} secciones, "
          f"{failures} discrepancias")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
