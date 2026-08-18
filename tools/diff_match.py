#!/usr/bin/env python3
"""Compare the cross-source matcher: same players, same futbolfantasy feed, same pairs.

This is the heuristic that decides whether "aitor fdez" is Aitor Fernández, and getting it
wrong shows up as a player wearing somebody else's value curve — plausible and wrong, which
is the failure mode worth spending a harness on.

Only the ids are compared. Comparing whole rows would compare the scrapers again, and those
have their own harness.

    usage: diff_match.py [path-to-fantasy-go]
"""
from __future__ import annotations

import json
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from fantasy import futbolfantasy as ff, laliga, matching  # noqa: E402


def main() -> int:
    binary = sys.argv[1] if len(sys.argv) > 1 else "./fantasy-go"
    if not Path(binary).exists():
        print(f"no encuentro el binario de Go en {binary}")
        return 2

    players, teams, market = laliga.all_players(), laliga.teams_master(), ff.market()
    scratch = Path(tempfile.mkdtemp())
    (scratch / "players.json").write_text(json.dumps(players))
    (scratch / "teams.json").write_text(json.dumps(teams))
    (scratch / "market.json").write_text(json.dumps(market, default=str))

    python, unmatched = matching.match_market(players, market,
                                              matching.build_team_index(teams))
    expected = {key: str(row["ff_id"]) for key, row in python.items()}

    result = subprocess.run([binary, "match", str(scratch / "players.json"),
                             str(scratch / "market.json"), str(scratch / "teams.json")],
                            capture_output=True, text=True, cwd=ROOT)
    if result.returncode != 0:
        print(result.stderr)
        return 2
    go = json.loads(result.stdout)
    got = go.get("matched") or {}

    names = {str(p["id"]): matching.player_label(p) for p in players}
    wrong = sorted(set(expected) | set(got))
    problems = [key for key in wrong if expected.get(key) != got.get(key)]

    print(f"  emparejados: py {len(expected)} · go {len(got)}")
    print(f"  sin emparejar: py {len(unmatched)} · go {len(go.get('unmatched') or [])}")
    for key in problems[:8]:
        print(f"    {names.get(key, key)}: py={expected.get(key)} go={got.get(key)}")

    failures = len(problems) + abs(len(unmatched) - len(go.get("unmatched") or []))
    print(f"\n{'VERDE' if not failures else 'ROJO'}: {len(players)} jugadores contra "
          f"{len(market)} filas, {failures} discrepancias")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
