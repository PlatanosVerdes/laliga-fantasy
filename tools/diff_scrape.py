#!/usr/bin/env python3
"""Compare the futbolfantasy parsers, page by page, field by field.

These read somebody else's HTML with regular expressions, which makes them the most fragile
code in the project — and the easiest to compare properly, because a parser is a pure
function of a page. Every cached page in the snapshot goes through both implementations and
the JSON they produce is compared key by key, so a difference cannot be a different download.

Coverage is the point here: not one example of each page but *all* of them, because the
interesting rows are the odd ones — the player with no history, the absence with no stated
return date, the row whose points cell has nineteen spans and only one that counts.

    usage: diff_scrape.py [path-to-fantasy-go] [path-to-cache-dir]
"""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from fantasy import futbolfantasy as ff  # noqa: E402

# Which parser each cache tag belongs to, and what to call it on the Go side.
PARSERS = {
    "ff_market": ("mercado", lambda page: ff.parse_market(page)),
    "ff_detail": ("detalle", lambda page: ff.parse_detail(page)),
    "ff_player": ("jugador", lambda page: ff.parse_player_page(page)),
    "ff_absences": ("ausencias", lambda page: ff.parse_absences(page, "lesionado")),
}


def normalise(value):
    """JSON round-trip both sides, so a tuple and a list or an int and a float agree."""
    return json.loads(json.dumps(value, default=str))


def differences(left, right, path=""):
    """Every leaf that differs, with where it is."""
    out = []
    if isinstance(left, dict) and isinstance(right, dict):
        for key in sorted(set(left) | set(right)):
            out += differences(left.get(key), right.get(key), f"{path}.{key}")
    elif isinstance(left, list) and isinstance(right, list):
        if len(left) != len(right):
            out.append((f"{path}[]", f"{len(left)} elementos", f"{len(right)} elementos"))
        for index, (one, other) in enumerate(zip(left, right)):
            out += differences(one, other, f"{path}[{index}]")
    elif isinstance(left, (int, float)) and isinstance(right, (int, float)):
        if abs(float(left) - float(right)) > 1e-9:
            out.append((path, left, right))
    elif left != right:
        out.append((path, left, right))
    return out


def main() -> int:
    binary = sys.argv[1] if len(sys.argv) > 1 else "./fantasy-go"
    cache = Path(sys.argv[2]) if len(sys.argv) > 2 else ROOT / "data" / "cache"
    if not Path(binary).exists():
        print(f"no encuentro el binario de Go en {binary}")
        return 2

    pages = sorted(path for tag in PARSERS for path in cache.glob(f"{tag}_*.cache"))
    if not pages:
        print(f"no hay paginas de futbolfantasy en {cache}")
        return 2

    totals: dict[str, list[int]] = {}
    failures = 0
    for path in pages:
        tag = "_".join(path.name.split("_")[:2])
        name, parse = PARSERS[tag]
        page = path.read_text(encoding="utf-8", errors="replace")

        python = normalise(parse(page))
        result = subprocess.run([binary, "scrape", name, str(path)],
                                capture_output=True, text=True, cwd=ROOT)
        if result.returncode != 0:
            print(f"  ERROR  {path.name}: {result.stderr.strip()[:120]}")
            failures += 1
            continue
        go = normalise(json.loads(result.stdout))

        problems = differences(python, go)
        counts = totals.setdefault(name, [0, 0])
        counts[0] += 1
        if problems:
            counts[1] += 1
            failures += 1
            print(f"  DISTINTA  {name} {path.name}  ({len(problems)} diferencias)")
            for where, left, right in problems[:4]:
                print(f"      {where}: py={left!r} go={right!r}")

    print()
    for name, (total, bad) in sorted(totals.items()):
        mark = "ok " if not bad else "NO "
        print(f"  {mark}{name:10} {total:3} paginas, {bad} distintas")
    print(f"\n{'VERDE' if not failures else 'ROJO'}: {len(pages)} paginas, "
          f"{failures} discrepancias")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
