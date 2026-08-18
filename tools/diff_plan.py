#!/usr/bin/env python3
"""Compare the policy engine: what the standing instructions would do, and why.

Every row here is a euro away from being an operation, so the comparison is of the whole
action — the verb, the amount, the offer it points at and the sentence that explains it. A
plan that agrees on the verb and disagrees on the amount would be worse than one that
disagrees on both.

The real squad is used for the current state, and then a synthetic squad covers the branches
today happens not to be in: an auto-sell that fires, one that is refused because the player is
the last at his position, a raid that pays, one cancelled by a raise, one blocked by a shield,
one waiting, and one with no cash.

    usage: diff_plan.py [path-to-fantasy-go] [path-to-report.json]
"""
from __future__ import annotations

import json
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from fantasy import policies  # noqa: E402

FIELDS = ["player_id", "name", "action", "amount", "offer_id", "market_id", "why",
          "clause", "max_pay", "owner", "owner_team_id", "player_team_id"]


def synthetic() -> list[dict]:
    """A squad that reaches the branches the real one does not."""
    def player(pid, name, position, **extra):
        base = {"id": pid, "name": name, "is_mine": True, "position_id": position,
                "position": {1: "POR", 2: "DEF", 3: "MED", 4: "DEL"}[position],
                "value": 10_000_000, "offers": [], "market": None}
        base.update(extra)
        return base

    squad = [player(f"pad{index}", f"Relleno {index}", 3) for index in range(4)]
    squad += [
        # Auto-sell that should fire: offer over the asking price, and cover at his position.
        player("1", "Vendible", 3, market={"market_id": "m1", "min_bid": 10_200_000},
               offers=[{"id": "o1", "money": 11_000_000}]),
        # The only keeper: same offer, and it must refuse.
        player("2", "Portero unico", 1, market={"market_id": "m2", "min_bid": 10_200_000},
               offers=[{"id": "o2", "money": 12_000_000}]),
        # Listed, offer below the bar.
        player("3", "Sin llegar", 3, market={"market_id": "m3", "min_bid": 12_000_000},
               offers=[{"id": "o3", "money": 9_000_000}]),
        # Not listed at all: should be relisted.
        player("4", "Fuera del mercado", 3),
        # A good offer with nothing authorised: notice only.
        player("5", "Solo aviso", 3, market={"market_id": "m5", "min_bid": 9_000_000},
               offers=[{"id": "o5", "money": 9_500_000}]),
    ]
    rivals = [
        {"id": "10", "name": "Clausulazo", "is_mine": False, "owner": "La rataneta",
         "owner_team_id": "77", "player_team_id": "pt10", "value": 8_000_000,
         "clause": 9_000_000, "clause_locked": False, "position_id": 3, "offers": []},
        {"id": "11", "name": "Subida", "is_mine": False, "owner": "TheMessias",
         "owner_team_id": "78", "player_team_id": "pt11", "value": 8_000_000,
         "clause": 20_000_000, "clause_locked": False, "position_id": 3, "offers": []},
        {"id": "12", "name": "Blindado", "is_mine": False, "owner": "Villaone",
         "owner_team_id": "79", "player_team_id": "pt12", "value": 8_000_000,
         "clause": 9_000_000, "clause_locked": False, "shielded": True,
         "position_id": 3, "offers": []},
        {"id": "13", "name": "Bloqueada", "is_mine": False, "owner": "LILTEAM",
         "owner_team_id": "80", "player_team_id": "pt13", "value": 8_000_000,
         "clause": 9_000_000, "clause_locked": True, "clause_hours_left": 30.0,
         "position_id": 3, "offers": []},
        {"id": "14", "name": "Caro", "is_mine": False, "owner": "JMjugon",
         "owner_team_id": "81", "player_team_id": "pt14", "value": 80_000_000,
         "clause": 120_000_000, "clause_locked": False, "position_id": 3, "offers": []},
    ]
    return squad + rivals


SYNTHETIC_POLICIES = {
    "1": {"id": "1", "always_listed": True, "auto_sell": True},
    "2": {"id": "2", "always_listed": True, "auto_sell": True},
    "3": {"id": "3", "always_listed": True, "auto_sell": True},
    "4": {"id": "4", "always_listed": True},
    "5": {"id": "5", "always_listed": True},
    "10": {"id": "10", "raid": True, "max_pay": 12_000_000},
    "11": {"id": "11", "raid": True, "max_pay": 12_000_000},
    "12": {"id": "12", "raid": True, "max_pay": 12_000_000},
    "13": {"id": "13", "raid": True, "max_pay": 12_000_000},
    "14": {"id": "14", "raid": True, "max_pay": 200_000_000},
}


def compare(name, python, go) -> int:
    if len(python) != len(go):
        print(f"  NO  {name}: py={len(python)} filas, go={len(go)}")
        return 1
    wrong = 0
    for index, (left, right) in enumerate(zip(python, go)):
        for field in FIELDS:
            if field not in left:
                continue
            one, other = left.get(field), right.get(field)
            if isinstance(one, (int, float)) and isinstance(other, (int, float)):
                if abs(float(one) - float(other)) > 1e-9:
                    wrong += 1
                    print(f"      {name}[{index}].{field}: py={one!r} go={other!r}")
            elif str(one) != str(other):
                wrong += 1
                print(f"      {name}[{index}].{field}: py={one!r} go={other!r}")
    if wrong:
        print(f"  NO  {name}: {wrong} campos distintos")
        return 1
    print(f"  ok  {name:22} {len(python):2} filas, mismas acciones y mismos motivos")
    return 0


def run(binary: str, players: list[dict], cash: float, scratch: Path):
    path = scratch / "players.json"
    path.write_text(json.dumps(players, default=str))
    result = subprocess.run([binary, "plan", str(path), str(int(cash))],
                            capture_output=True, text=True, cwd=ROOT)
    if result.returncode != 0:
        raise SystemExit(result.stderr)
    return json.loads(result.stdout)


def main() -> int:
    binary = sys.argv[1] if len(sys.argv) > 1 else "./fantasy-go"
    dump = Path(sys.argv[2]) if len(sys.argv) > 2 else ROOT / "data" / "report.json"
    if not Path(binary).exists():
        print(f"no encuentro el binario de Go en {binary}")
        return 2

    scratch = Path(tempfile.mkdtemp())
    failures = 0

    if dump.exists():
        blob = json.loads(dump.read_text(encoding="utf-8"))
        players = blob["universe"]["players"]
        cash = (blob.get("advice") or {}).get("budget") or 0
        go = run(binary, players, cash, scratch)
        failures += compare("plan real", policies.plan(players), go["plan"])
        failures += compare("clausulazos reales",
                            policies.raid_plan(players, cash=cash), go["raids"])

    # The synthetic squad needs its own policies file, so it is written where both sides read.
    saved = policies.load()
    try:
        policies.save(SYNTHETIC_POLICIES)
        players = synthetic()
        go = run(binary, players, 15_000_000, scratch)
        failures += compare("plan sintetico", policies.plan(players), go["plan"])
        failures += compare("clausulazos sinteticos",
                            policies.raid_plan(players, cash=15_000_000), go["raids"])
    finally:
        policies.save(saved)

    print(f"\n{'VERDE' if not failures else 'ROJO'}: {failures} discrepancias")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
