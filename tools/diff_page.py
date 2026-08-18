#!/usr/bin/env python3
"""Compare the whole page, byte for byte, from the same dump.

This is the last comparison of the port and the strictest: 800 KB of one document, where a
single character out of place in any of forty notes shows up as a diff. The timestamp and the
league name are arguments on both sides, because a page that reads the clock cannot be
compared with anything.

The dump is Python's own `report --json`, plus what the policy engine computes (the standing
instructions plan, the scheduled raids, the policies file) pasted in under `_plan`, `_raids`
and `_policies`: those rows are the policy engine's output rather than the model's, and the
renderer has to be handed them.

    usage: diff_page.py [path-to-fantasy-go] [path-to-report.json]
"""
from __future__ import annotations

import difflib
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from fantasy import policies, report  # noqa: E402

GENERATED = "18/08/2026 16:20"
LEAGUE = "Liga Fantasy Comité 2026-"


def main() -> int:
    binary = sys.argv[1] if len(sys.argv) > 1 else "./fantasy-go"
    dump = Path(sys.argv[2]) if len(sys.argv) > 2 else ROOT / "data" / "report.json"
    if not Path(binary).exists():
        print(f"no encuentro el binario de Go en {binary}")
        return 2
    if not dump.exists():
        print(f"no encuentro el volcado en {dump}")
        return 2

    blob = json.loads(dump.read_text(encoding="utf-8"))
    universe, advice = blob["universe"], blob["advice"]

    advice["_plan"] = policies.plan(universe["players"])
    advice["_raids"] = policies.raid_plan(universe["players"],
                                          cash=advice.get("budget") or 0)
    advice["_policies"] = policies.load()

    scratch = Path(tempfile.mkdtemp())
    handed = scratch / "page.json"
    handed.write_text(json.dumps({"universe": universe, "advice": advice}, default=str))

    # Python keeps the squad shape keyed by int; a round trip through JSON turns those into
    # strings, so they are put back before calling the renderer that expects them.
    for_python = dict(advice)
    for_python["shape"] = {int(key): value
                           for key, value in (advice.get("shape") or {}).items()}

    # The feed is a separate argument to build(), and the dump keeps the events under the
    # universe. Forgetting it renders the empty state, which then looks like the port has
    # invented a whole section.
    python = report.build(universe, for_python, context={"league_name": LEAGUE},
                          activity=universe.get("activity") or [])
    # The only thing Python reads from the clock.
    python = re.sub(r"<p>\d{2}/\d{2}/\d{4} \d{2}:\d{2}", f"<p>{GENERATED}", python, count=1)

    go = subprocess.run([binary, "page", str(handed), GENERATED, LEAGUE],
                        capture_output=True, text=True, cwd=ROOT).stdout

    print(f"  py {len(python):>8} bytes")
    print(f"  go {len(go):>8} bytes")
    if python == go:
        print("\nVERDE: la pagina es identica byte a byte")
        return 0

    shown = 0
    for tag, i1, i2, j1, j2 in difflib.SequenceMatcher(None, python, go).get_opcodes():
        if tag == "equal" or shown >= 6:
            continue
        print(f"  {tag} en el byte {i1}:")
        print(f"      py={python[i1:i2][:140]!r}")
        print(f"      go={go[j1:j2][:140]!r}")
        shown += 1
    print("\nROJO: la pagina difiere")
    return 1


if __name__ == "__main__":
    sys.exit(main())
