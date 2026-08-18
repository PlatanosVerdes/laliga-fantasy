#!/usr/bin/env python3
"""Compare the two schedulers decision by decision.

The refresh design only pays off if Go decides exactly what Python decides: a Go loop that
rebuilds when Python would have stayed asleep throws away the saving, and one that sleeps
when Python would have acted arrives late to a clause.

Both sides take `now` as an argument, so every scenario is reproducible. The scenarios are
the states that matter and are rare — an auction about to close, an offer about to expire,
our own match under way, somebody else's match, a finished one, a matchday closing — each
run at four cadences including "the page is open" and "the periodic rebuild is overdue".

    usage: diff_wake.py [path-to-fantasy-go]
"""
from __future__ import annotations

import datetime as dt
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
NOW = dt.datetime(2026, 8, 18, 20, 0, 0, tzinfo=dt.timezone.utc)


def at(seconds: int) -> str:
    return (NOW + dt.timedelta(seconds=seconds)).isoformat().replace("+00:00", "Z")


def payload(**overrides) -> dict:
    base = {"market": [], "players": [], "fixtures": [],
            "week": {"weekNumber": 1, "closingWeekDate": None}, "policies": {}}
    base.update(overrides)
    return base


MINE = {"id": "7", "name": "Sintetico", "team_id": "7", "is_mine": True, "offers": []}

SCENARIOS = {
    "liga en calma": payload(),
    "subasta en 5 min": payload(
        market=[{"market_id": "m1", "player_id": "7", "expires": at(300)}], players=[MINE]),
    "subasta en 3 h": payload(
        market=[{"market_id": "m1", "player_id": "7", "expires": at(10800)}], players=[MINE]),
    "oferta en 90 s": payload(players=[{**MINE, "offers": [{"money": 1, "expirationDate": at(90)}]}]),
    "partido mio en juego": payload(
        fixtures=[{"kickoff": at(-1800), "state": 1, "local": "ELC", "visitor": "BAR",
                   "local_id": "7", "visitor_id": "5"}], players=[MINE]),
    "partido de otros": payload(
        fixtures=[{"kickoff": at(-1800), "state": 1, "local": "BAR", "visitor": "RMA",
                   "local_id": "5", "visitor_id": "1"}], players=[MINE]),
    "partido terminado": payload(
        fixtures=[{"kickoff": at(-3600), "state": 7, "local": "ELC", "visitor": "BAR",
                   "local_id": "7", "visitor_id": "5"}], players=[MINE]),
    "empieza en 20 min": payload(
        fixtures=[{"kickoff": at(1200), "state": 1, "local": "ELC", "visitor": "BAR",
                   "local_id": "7", "visitor_id": "5"}], players=[MINE]),
    "cierre de jornada en 1 h": payload(week={"weekNumber": 1, "closingWeekDate": at(3600)}),
}

# tick, last_full, watched
VARIANTS = [
    ("120", "-", "false"),
    ("120", "-", "true"),
    ("300", "-", "false"),
    ("120", (NOW - dt.timedelta(seconds=1200)).isoformat(), "false"),
]


def main() -> int:
    binary = sys.argv[1] if len(sys.argv) > 1 else "./fantasy-go"
    if not Path(binary).exists():
        print(f"no encuentro el binario de Go en {binary}: "
              f"compilalo con `go build -o fantasy-go ./cmd/fantasy`")
        return 2

    scratch = Path(tempfile.mkdtemp())
    failures = 0
    for name, body in SCENARIOS.items():
        path = scratch / "scenario.json"
        path.write_text(json.dumps({"universe": body}))
        for tick, last_full, watched in VARIANTS:
            args = [str(path), NOW.isoformat(), tick, last_full, watched]
            python = subprocess.run([sys.executable, str(ROOT / "tools" / "wake.py"), *args],
                                    capture_output=True, text=True, cwd=ROOT).stdout
            go = subprocess.run([binary, "wake", *args],
                                capture_output=True, text=True, cwd=ROOT).stdout
            if python.strip() == go.strip():
                decision = next(l for l in python.splitlines() if l.startswith("decision"))
                print(f"  ok  {name:26} tick={tick:>3} watched={watched:5} -> {decision[10:]}")
                continue
            failures += 1
            print(f"  DISTINTO  {name}  tick={tick} watched={watched}")
            for left, right in zip(python.splitlines(), go.splitlines()):
                if left != right:
                    print(f"      py: {left}")
                    print(f"      go: {right}")

    total = len(SCENARIOS) * len(VARIANTS)
    print(f"\n{'VERDE' if not failures else 'ROJO'}: {total} decisiones, {failures} discrepancias")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
