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

# Added to league_teams by the advice layer (_rival_cash), not by the model, so their
# absence on the Go side is the state of the port and not a defect. Listed rather than
# ignored: an unexplained gap is how a port quietly loses a field.
ADVICE_LAYER = {"cash_position", "is_me", "power", "power_note"}


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
    shared_ids = sorted(set(py_rows) & set(go_rows))
    if shared_ids:
        # A key Go never emits is invisible when you only walk Go's keys, and that is
        # exactly how a missing field hides: the dict compares unequal upstream while
        # every field it does have matches.
        absent = sorted(set(py_rows[shared_ids[0]]) - set(go_rows[shared_ids[0]]) - CLOCK_DERIVED)
        if absent:
            print(f"    claves que Go no emite: {', '.join(absent)}")
            bad += len(absent)
    for row_id in shared_ids:
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


def compare_cash(py: dict, go: dict, tolerance: float, limit: int) -> int:
    """The cash model, per manager.

    This is the subtlest code in the project: it anchors the whole league on one measured
    figure and folds rewards into a base. A port that is quietly wrong here produces
    plausible numbers, which is the worst failure mode, so every manager is compared and
    not just ours.
    """
    bad = 0
    print("\n  cash_model:")
    py_model, go_model = py.get("cash_model") or {}, go.get("cash_model") or {}
    for field in sorted(set(py_model) | set(go_model)):
        left, right = py_model.get(field), go_model.get(field)
        if close_enough(left, right, tolerance):
            print(f"    ok {field}: {left!r}")
        else:
            print(f"    NO {field}: py={left!r} go={right!r}")
            bad += 1

    py_teams = {str(k): v for k, v in (py.get("league_teams") or {}).items()}
    go_raw = go.get("league_teams") or {}
    go_teams = ({str(k): v for k, v in go_raw.items()} if isinstance(go_raw, dict)
                else {str(t["team_id"]): t for t in go_raw})
    print(f"  league_teams: py={len(py_teams)} go={len(go_teams)}")
    for label, ids in (("faltan en Go", set(py_teams) - set(go_teams)),
                       ("sobran en Go", set(go_teams) - set(py_teams))):
        if ids:
            print(f"    {label}: {sorted(ids)}")
            bad += len(ids)

    for team_id in sorted(set(py_teams) & set(go_teams)):
        left, right = py_teams[team_id], go_teams[team_id]
        for field in sorted(set(left) & set(right)):
            if not close_enough(left[field], right[field], tolerance):
                if bad < limit:
                    print(f"    {left.get('manager') or team_id}.{field}: "
                          f"py={left[field]!r} go={right[field]!r}")
                bad += 1
    first = sorted(set(py_teams) & set(go_teams))[:1]
    if first:
        left, right = py_teams[first[0]], go_teams[first[0]]
        absent = sorted(set(left) - set(right) - CLOCK_DERIVED)
        pending = [field for field in absent if field in ADVICE_LAYER]
        lost = [field for field in absent if field not in ADVICE_LAYER]
        if pending:
            print(f"    de la capa de consejo, aun sin portar: {', '.join(pending)}")
        if lost:
            print(f"    claves que Go no emite: {', '.join(lost)}")
            bad += len(lost)
    print(f"    diferencias: {bad}")
    return bad


def compare_activity(py: dict, go: dict, tolerance: float, limit: int) -> int:
    """The transfer log, event by event. It is what the cash model is derived from, so a
    difference here is a difference in everyone's money."""
    py_events = py.get("activity") or []
    go_events = go.get("activity") or []
    print(f"\n  activity: py={len(py_events)} go={len(go_events)}")
    bad = abs(len(py_events) - len(go_events))
    fields = ("date", "type_id", "kind", "known", "user1", "user2", "buyer", "seller",
              "actor", "player", "player_id", "amount")
    for index, (left, right) in enumerate(zip(py_events, go_events)):
        for field in fields:
            if not close_enough(left.get(field), right.get(field), tolerance):
                if bad < limit:
                    print(f"    [{index}].{field}: py={left.get(field)!r} go={right.get(field)!r}")
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
    bad_cash = compare_cash(py, go, args.tolerance, args.limit)
    bad_activity = compare_activity(py, go, args.tolerance, args.limit)

    total = bad_fields + bad_market + bad_fixtures + bad_cash + bad_activity
    print(f"\n{'VERDE: sin diferencias' if total == 0 else f'ROJO: {total} diferencias'}")
    return 0 if total == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
