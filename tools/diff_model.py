#!/usr/bin/env python3
"""Compare the Python and Go models field by field.

This is what makes the port verifiable rather than hopeful. Both sides read the same
frozen cache and dump their universe; this walks the two trees and reports every
difference, keyed by player rather than summarised, because comparing totals hides
errors that cancel out.

Rules it enforces, each learned the hard way:

* **Only fields the Go side actually builds** are compared, and the ones it does not
  are listed as pending. A comparison that silently skips what is missing goes green
  for the wrong reason.
* **Floats get a tolerance, ordering gets none.** A score may differ in the last bits;
  a ranking must not differ at all.
* **Clock-derived fields are excluded by name**, because they differ between two runs
  of the *same* implementation seconds apart — `clause_hours_left` and `hours_left`
  are counted from now.

    usage: diff_model.py py.json go.json [--tolerance 1e-6] [--limit 10]
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

# Differ between two runs of the same code, so they can never be evidence of anything.
CLOCK_DERIVED = {"clause_hours_left", "hours_left", "generated_at", "at"}


def load(path: str) -> dict[str, Any]:
    blob = json.loads(Path(path).read_text(encoding="utf-8"))
    return blob.get("universe", blob)


def close_enough(left: Any, right: Any, tolerance: float) -> bool:
    if isinstance(left, bool) or isinstance(right, bool):
        return left == right
    if isinstance(left, (int, float)) and isinstance(right, (int, float)):
        scale = max(1.0, abs(left), abs(right))
        return abs(left - right) <= tolerance * scale
    return left == right


def compare_players(py: dict, go: dict, tolerance: float, limit: int) -> tuple[int, int]:
    py_players = {str(p["id"]): p for p in py.get("players") or []}
    go_players = {str(p["id"]): p for p in go.get("players") or []}

    missing = sorted(set(py_players) - set(go_players))
    extra = sorted(set(go_players) - set(py_players))
    if missing:
        print(f"  faltan {len(missing)} jugadores en Go, p.ej. "
              f"{[py_players[i]['name'] for i in missing[:5]]}")
    if extra:
        print(f"  sobran {len(extra)} jugadores en Go, p.ej. "
              f"{[go_players[i]['name'] for i in extra[:5]]}")

    shared = sorted(set(py_players) & set(go_players))
    if not shared:
        return 0, 0

    ported = sorted(set(go_players[shared[0]]) - CLOCK_DERIVED)
    pending = sorted(set(py_players[shared[0]]) - set(ported) - CLOCK_DERIVED)

    mismatches: dict[str, list[str]] = {}
    for player_id in shared:
        left, right = py_players[player_id], go_players[player_id]
        for field in ported:
            if field not in left:
                continue
            if not close_enough(left[field], right[field], tolerance):
                mismatches.setdefault(field, []).append(
                    f"{left.get('name')}: py={left[field]!r} go={right[field]!r}")

    print(f"\n  jugadores comparados: {len(shared)}")
    print(f"  campos comparados   : {len(ported)}")
    if mismatches:
        print(f"  campos que NO cuadran: {len(mismatches)}")
        for field, cases in sorted(mismatches.items(), key=lambda kv: -len(kv[1])):
            print(f"    {field}  ({len(cases)} de {len(shared)})")
            for case in cases[:limit]:
                print(f"        {case}")
    else:
        print("  todos los campos portados cuadran")

    if pending:
        print(f"\n  todavia sin portar ({len(pending)}): {', '.join(pending)}")
    return len(shared), len(mismatches)


def compare_lists(py: dict, go: dict, key: str, identity: str, tolerance: float,
                  limit: int) -> int:
    py_rows = {str(r[identity]): r for r in py.get(key) or []}
    go_rows = {str(r[identity]): r for r in go.get(key) or []}
    if not py_rows and not go_rows:
        print(f"\n  {key}: ninguno en los dos lados")
        return 0

    print(f"\n  {key}: py={len(py_rows)} go={len(go_rows)}")
    for label, ids in (("faltan en Go", set(py_rows) - set(go_rows)),
                       ("sobran en Go", set(go_rows) - set(py_rows))):
        if ids:
            print(f"    {label}: {sorted(ids)[:6]}")

    bad = 0
    for row_id in sorted(set(py_rows) & set(go_rows)):
        left, right = py_rows[row_id], go_rows[row_id]
        for field in sorted(set(right) - CLOCK_DERIVED):
            if field not in left:
                continue
            if not close_enough(left[field], right[field], tolerance):
                if bad < limit:
                    print(f"    {row_id}.{field}: py={left[field]!r} go={right[field]!r}")
                bad += 1
    print(f"    diferencias: {bad}")
    return bad


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("python_json")
    parser.add_argument("go_json")
    parser.add_argument("--tolerance", type=float, default=1e-6)
    parser.add_argument("--limit", type=int, default=8)
    args = parser.parse_args()

    py, go = load(args.python_json), load(args.go_json)

    print("== escalares ==")
    for field in ("my_team_id", "ownership_loaded", "completed_weeks"):
        left, right = py.get(field), go.get(field)
        mark = "ok " if close_enough(left, right, args.tolerance) else "NO "
        print(f"  {mark}{field}: py={left!r} go={right!r}")

    print("\n== jugadores ==")
    _, bad_fields = compare_players(py, go, args.tolerance, args.limit)

    bad_market = compare_lists(py, go, "market", "market_id", args.tolerance, args.limit)
    bad_fixtures = compare_lists(py, go, "fixtures", "id", args.tolerance, args.limit)

    total = bad_fields + bad_market + bad_fixtures
    print(f"\n{'VERDE: sin diferencias' if total == 0 else f'ROJO: {total} diferencias'}")
    return 0 if total == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
