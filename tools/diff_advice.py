#!/usr/bin/env python3
"""Compare the advice layer: same universe in, same buckets out.

Not just the buckets' contents but their *order*, because the order is the recommendation:
the first row of `bids_now` is what the page tells you to bid on. Every computed field is
compared too — priority, pressure, risk, verdict, points per million at the clause — since a
bucket can hold the right players in the right order and still be wrong about why.

    usage: diff_advice.py [path-to-fantasy-go] [path-to-report.json]
"""
from __future__ import annotations

import json
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from fantasy import analysis  # noqa: E402

# The fields the advice layer computes, as opposed to the ones it copies from the model.
COMPUTED = ["priority", "pressure", "risk", "threats", "top_threat", "verdict",
            "ppm_at_clause", "vs_market", "clause_premium", "worth_taking", "vs_value",
            "vs_ask", "offer_amount", "offer_count", "ask", "ask_ratio", "underpriced",
            "overpriced", "affordable", "position_gap", "entry_cost", "route", "reasons",
            "power", "power_note", "cash_position", "is_me", "estimated_cash", "net_flow"]

SCALARS = ["budget", "max_debt", "spending_power", "clauses_locked", "clauses_unlock_from",
           "squad_ppm_benchmark", "free_agent_count"]


def close(left, right) -> bool:
    if isinstance(left, (int, float)) and isinstance(right, (int, float)):
        return abs(float(left) - float(right)) <= 1e-9
    return left == right


def main() -> int:
    binary = sys.argv[1] if len(sys.argv) > 1 else "./fantasy-go"
    dump = Path(sys.argv[2]) if len(sys.argv) > 2 else ROOT / "data" / "report.json"
    if not Path(binary).exists():
        print(f"no encuentro el binario de Go en {binary}")
        return 2

    blob = json.loads(dump.read_text(encoding="utf-8"))
    universe = blob["universe"]
    budget = (blob.get("advice") or {}).get("budget") or 0

    scratch = Path(tempfile.mkdtemp())
    path = scratch / "universe.json"
    path.write_text(json.dumps(universe, default=str))

    python = analysis.recommend(universe, budget=budget, max_debt=0, limit=15)
    result = subprocess.run([binary, "advise", str(path), str(int(budget))],
                            capture_output=True, text=True, cwd=ROOT)
    if result.returncode != 0:
        print(result.stderr)
        return 2
    go = json.loads(result.stdout)

    failures = 0
    for name, bucket in sorted(python.items()):
        if not isinstance(bucket, list):
            continue
        theirs = go.get(name) or []
        ours = [str(row.get("id") or row.get("team_id")) for row in bucket]
        yours = [str(row.get("id") or row.get("team_id")) for row in theirs]
        if ours != yours:
            failures += 1
            print(f"  NO  {name}: py={len(bucket)} go={len(theirs)}")
            for index, (left, right) in enumerate(zip(ours, yours)):
                if left != right:
                    print(f"        primera diferencia en {index}: py={left} go={right}")
                    break
            continue

        wrong = 0
        for index, (left, right) in enumerate(zip(bucket, theirs)):
            for field in COMPUTED:
                if field not in left:
                    continue
                if not close(left.get(field), right.get(field)):
                    wrong += 1
                    if wrong <= 3:
                        print(f"      {name}[{index}].{field}: "
                              f"py={left.get(field)!r} go={right.get(field)!r}")
        if wrong:
            failures += 1
            print(f"  NO  {name}: {wrong} campos calculados distintos")
        else:
            print(f"  ok  {name:20} {len(bucket):3} filas, mismo orden y mismos campos")

    for field in SCALARS:
        if close(python.get(field), go.get(field)):
            print(f"  ok  {field:20} {python.get(field)!r}")
        else:
            failures += 1
            print(f"  NO  {field}: py={python.get(field)!r} go={go.get(field)!r}")

    print(f"\n{'VERDE' if not failures else 'ROJO'}: {failures} discrepancias")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
