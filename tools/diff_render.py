#!/usr/bin/env python3
"""Compare the page's formatters, cell by cell, against the Go port.

Every table on the page is these primitives repeated a few thousand times. If a number is
spelled differently on one side — a comma where the other writes a dot, a zero where the
other writes an em dash — then every section differs and a section-level diff tells you
nothing. So the primitives are pinned first, over the inputs that look like nothing and
are not:

* 999,500, which a naive implementation rounds to "1.000K" — a thousand-fold lie in the
  unit — and which must read "1,00M";
* a negative, whose sign has to sit in front of the separators;
* an absent value, which is an em dash and not a zero;
* a flat series, where the sparkline's span would be a division by zero;
* a series of four, which is not enough history to draw and must be omitted rather than
  faked.

    usage: diff_render.py [path-to-fantasy-go]
"""
from __future__ import annotations

import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from fantasy import report  # noqa: E402

# The same rows as cmd/fantasy's cellCases, in the same order.
CASES: list[tuple[str, object]] = [
    ("money", None), ("money", 0), ("money", 1), ("money", 999.0), ("money", 1000.0),
    ("money", 999_499.0), ("money", 999_500.0), ("money", 1_000_000.0),
    ("money", 9_867_495.0), ("money", 130_960_400.0), ("money", -2_500_000.0),
    ("money", -999.0),
    ("num", None), ("num", 0), ("num", 2.4055), ("num", -1.5), ("num", 121.0),
    ("int", None), ("int", 0), ("int", 211.0), ("int", -3.0),
    ("pct", None), ("pct", 0), ("pct", 5.83), ("pct", -11.4), ("pct", 12.0), ("pct", -30.0),
    ("mag", None), ("mag", 0), ("mag", 0.244), ("mag", 0.45), ("mag", 1.7),
    ("text", None), ("text", "Barcelona (casa)"), ("text", "M. Dituro"),
    ("text", "O'Neill & co"),
    ("spark", []), ("spark", [1.0, 2.0, 3.0]),
    ("spark", [9_000_000.0, 9_100_000.0, 8_900_000.0, 9_400_000.0, 9_867_495.0]),
    ("spark", [5.0, 5.0, 5.0, 5.0, 5.0, 5.0]),
    ("starts", None), ("starts", 0), ("starts", 29.0), ("starts", 30.0), ("starts", 50.0),
    ("starts", 75.0), ("starts", 100.0),
    ("star", {"id": "1300", "name": "Camavinga", "starred": True}),
    ("star", {"id": "184", "name": "David Soria"}),
    ("player", {"id": "1300", "name": "Camavinga", "team": "Real Madrid",
                "team_short": "RMA", "team_id": "1", "position": "MED", "position_id": 3.0,
                "available": True}),
    ("player", {"id": "7", "name": "Lesionado", "team": "Elche CF",
                "team_short": "ELC", "team_id": "7", "position": "POR", "position_id": 1.0,
                "available": False, "status": "injured"}),
    ("player", {"id": "8", "name": "Dudoso", "team": "Getafe",
                "team_short": "GET", "team_id": "17", "position": "DEL", "position_id": 4.0,
                "available": True, "status": "doubtful", "prior_based": True,
                "is_mine": True}),
]


def go_rows(binary: str) -> list[tuple[str, str, str]]:
    """Read `fantasy-go cells`: kind|value|sort|inner."""
    result = subprocess.run([binary, "cells"], capture_output=True, text=True, cwd=ROOT)
    if result.returncode != 0:
        raise SystemExit(result.stderr)
    rows = []
    for line in result.stdout.splitlines():
        if not line:
            continue
        kind, _value, sort, inner = line.split("|", 3)
        rows.append((kind, sort, inner))
    return rows


def main() -> int:
    binary = sys.argv[1] if len(sys.argv) > 1 else "./fantasy-go"
    if not Path(binary).exists():
        print(f"no encuentro el binario de Go en {binary}")
        return 2

    go = go_rows(binary)
    if len(go) != len(CASES):
        print(f"Go ha devuelto {len(go)} casos y hay {len(CASES)}")
        return 1

    failures = 0
    for index, (kind, value) in enumerate(CASES):
        inner, sort = report._cell(value, kind)
        go_kind, go_sort, go_inner = go[index]
        problems = []
        if kind != go_kind:
            problems.append(f"tipo py={kind} go={go_kind}")
        if sort != go_sort:
            problems.append(f"clave de orden py={sort!r} go={go_sort!r}")
        if inner != go_inner:
            problems.append(f"html\n        py={inner}\n        go={go_inner}")
        if problems:
            failures += 1
            print(f"  DISTINTO  {kind} {value!r}")
            for problem in problems:
                print(f"      {problem}")
        else:
            shown = inner if len(inner) < 60 else inner[:57] + "..."
            print(f"  ok  {kind:6} {str(value)[:26]:26} {shown}")

    print(f"\n{'VERDE' if not failures else 'ROJO'}: {len(CASES)} celdas, "
          f"{failures} discrepancias")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
