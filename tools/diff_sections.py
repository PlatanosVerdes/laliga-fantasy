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

# (name, which bucket of the advice, how the columns differ)
SECTIONS = [
    ("plantilla", "squad", {}),
    ("mercado", "bids_now", {"cost_label": "Puja minima"}),
]


def columns_for(name: str, kwargs: dict):
    columns = report._player_columns(**kwargs)
    if name == "mercado":
        columns.insert(4, ("Puja max. rentable", lambda row: row.get("ideal_bid"), "ideal"))
    return columns


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

    for name, bucket, kwargs in SECTIONS:
        rows = advice.get(bucket) or []
        if not rows:
            print(f"  {name}: el volcado no trae filas, no se puede comparar")
            failures += 1
            continue

        path = scratch / f"{name}.json"
        path.write_text(json.dumps(rows))
        python = report._table(columns_for(name, kwargs), rows, section=name,
                              filterable=(name == "mercado"))
        go = subprocess.run([binary, "section", name, str(path)],
                            capture_output=True, text=True, cwd=ROOT).stdout

        if python == go:
            print(f"  ok  {name:12} {len(rows):3} filas · {len(python):>6} bytes identicos")
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

    print(f"\n{'VERDE' if not failures else 'ROJO'}: {len(SECTIONS)} secciones, "
          f"{failures} discrepancias")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
