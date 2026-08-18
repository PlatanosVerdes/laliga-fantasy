#!/usr/bin/env python3
"""Compare, operation by operation, the exact request each implementation would send.

This is the one part of the port where "roughly the same" is not good enough. The ids are
unobvious and were each paid for with a 500: the squad-slot id where the player id looks
right, the market id for a direct offer, factor 2 for a clause raise, a goalkeeper that is
a single id and not a list. A port that swaps two of them fails in the most expensive way
available — by selling the wrong player, or by paying a clause twice.

Nothing is sent. Both sides build the call with the same fixed arguments and the method,
path and body are compared literally, key by key.

    usage: diff_writes.py [path-to-fantasy-go]
"""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from fantasy import writes  # noqa: E402

# One set of arguments for every operation, so a swapped id shows up as a swapped letter.
FIXED = dict(league_id="L", team_id="T", market_id="M", bid_id="B", offer_id="O",
             player_id="P", player_team_id="PT", amount=1000)

CASES = {
    "bid": lambda: writes._bid("L", "M", 1000),
    "modify_bid": lambda: writes._modify_bid("L", "M", "B", 1000),
    "cancel_bid": lambda: writes._cancel_bid("L", "M", "B"),
    "raise_clause": lambda: writes._raise_clause("L", "PT", 1000),
    "accept_offer": lambda: writes._accept_offer("L", "M", "O", 1000),
    "decline_offer": lambda: writes._decline_offer("L", "M", "O"),
    "sell_to_market": lambda: writes._sell_to_market("L", "PT", 1000),
    "direct_offer": lambda: writes._direct_offer("L", "M", 1000),
    "pay_clause": lambda: writes._pay_clause("L", "PT", 1000),
    "save_lineup": lambda: writes._save_lineup("T", "G", ["D"], ["M"], ["S"], [3, 4, 3]),
    "withdraw": lambda: writes._withdraw("L", "M"),
}


def parse_go(output: str) -> dict[str, dict]:
    """Read `fantasy-go calls`: a line per operation, an indented JSON body under it."""
    calls: dict[str, dict] = {}
    current = None
    for line in output.splitlines():
        if not line.strip():
            continue
        if line.startswith(" "):
            if current:
                calls[current]["body"] = json.loads(line.strip())
            continue
        name, method, path = line.split(maxsplit=2)
        current = name
        calls[name] = {"method": method, "path": path.strip(), "body": None}
    return calls


# The same rows as cmd/fantasy's validationTable, in the same order. Kept in both places
# on purpose: a shared fixture file would be one more thing that can drift silently, and
# these fifteen lines are the contract.
VALIDATION = [
    ("puja normal", "bid", 1_000_000, dict(min_bid=900_000, ideal_bid=2_000_000), 50_000_000),
    ("puja por debajo del minimo", "bid", 800_000, dict(min_bid=900_000, ideal_bid=2_000_000), 50_000_000),
    ("puja de cero", "bid", 0, dict(), 50_000_000),
    ("puja mayor que el saldo", "bid", 2_000_000, dict(ideal_bid=9_000_000), 1_000_000),
    ("puja sobre el techo rentable", "bid", 3_000_000, dict(ideal_bid=2_000_000), 50_000_000),
    ("puja sin rentabilidad conocida", "bid", 1_000_000, dict(), 50_000_000),
    ("puja que se come medio saldo", "bid", 600_000, dict(ideal_bid=900_000), 1_000_000),
    ("puja con rivales", "bid", 1_000_000, dict(ideal_bid=2_000_000, bids=3), 50_000_000),
    ("clausula pagada de menos", "pay_clause", 9_000_000, dict(clause=10_000_000), 50_000_000),
    ("clausula exacta", "pay_clause", 10_000_000, dict(clause=10_000_000), 50_000_000),
    ("clausula sin saldo", "pay_clause", 10_000_000, dict(clause=10_000_000), 5_000_000),
    ("oferta directa negativa", "direct_offer", -1, dict(), 50_000_000),
    ("aceptar por debajo del valor", "accept_offer", 8_000_000, dict(value=10_000_000), 0),
    ("aceptar por encima del techo", "accept_offer", 12_000_000,
     dict(value=10_000_000, ideal_bid=11_000_000), 0),
    ("retirar del mercado", "withdraw", 0, dict(), 50_000_000),
]


def python_validation() -> list[str]:
    """Run Python's own guard over the table, with the cash stubbed.

    prepare() reads /money, so the reader is replaced: the harness must not depend on a
    session, and it must certainly not spend anything.
    """
    from fantasy import laliga

    rows = []
    original = laliga.team_money
    try:
        for label, operation, amount, player, cash in VALIDATION:
            laliga.team_money = lambda team_id, ttl=0, _cash=cash: {"teamMoney": _cash}
            try:
                summary = writes.prepare(operation, league_id="L", my_team_id="T",
                                         amount=amount, market_id="M", offer_id="O",
                                         player_team_id="PT",
                                         player={"name": "X", **player},
                                         allow_writes=True)
                rows.append(f"{label:32} {'acepta':8} {len(summary['warnings'])}")
            except writes.WriteError:
                rows.append(f"{label:32} {'rechaza':8} 0")
    finally:
        laliga.team_money = original
    return rows


def compare_validation(binary: str) -> int:
    result = subprocess.run([binary, "checks"], capture_output=True, text=True, cwd=ROOT)
    go_rows = [line.rstrip() for line in result.stdout.splitlines() if line.strip()]
    py_rows = [row.rstrip() for row in python_validation()]

    print("\n== validaciones ==")
    failures = 0
    for index, py_row in enumerate(py_rows):
        go_row = go_rows[index] if index < len(go_rows) else "(falta)"
        if py_row == go_row:
            print(f"  ok  {py_row}")
        else:
            failures += 1
            print(f"  DISTINTO\n      py: {py_row}\n      go: {go_row}")
    if len(go_rows) != len(py_rows):
        print(f"  la tabla de Go tiene {len(go_rows)} filas y la de Python {len(py_rows)}")
        failures += 1
    return failures


def main() -> int:
    binary = sys.argv[1] if len(sys.argv) > 1 else "./fantasy-go"
    if not Path(binary).exists():
        print(f"no encuentro el binario de Go en {binary}")
        return 2

    result = subprocess.run([binary, "calls"], capture_output=True, text=True, cwd=ROOT)
    if result.returncode != 0:
        print(result.stderr)
        return 2
    go_calls = parse_go(result.stdout)

    failures = 0
    for name, build in CASES.items():
        call = build()
        left = {"method": call.method, "path": call.path, "body": call.body}
        right = go_calls.get(name)
        if right is None:
            print(f"  FALTA en Go: {name}")
            failures += 1
            continue

        problems = []
        if left["method"] != right["method"]:
            problems.append(f"metodo py={left['method']} go={right['method']}")
        if left["path"] != right["path"]:
            problems.append(f"ruta\\n        py={left['path']}\\n        go={right['path']}")
        if (left["body"] or None) != (right["body"] or None):
            problems.append(f"cuerpo\\n        py={left['body']}\\n        go={right['body']}")

        if problems:
            failures += 1
            print(f"  DISTINTO  {name}")
            for problem in problems:
                print(f"      {problem}")
        else:
            body = json.dumps(left["body"], sort_keys=True) if left["body"] else ""
            print(f"  ok  {name:15} {left['method']:6} {left['path']}  {body}")

    extra = sorted(set(go_calls) - set(CASES))
    if extra:
        print(f"  operaciones que Go tiene y Python no: {extra}")
        failures += len(extra)

    failures += compare_validation(binary)

    print(f"\n{'VERDE' if not failures else 'ROJO'}: {len(CASES)} operaciones y "
          f"{len(VALIDATION)} validaciones, {failures} discrepancias")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
