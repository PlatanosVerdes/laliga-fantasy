#!/usr/bin/env python3
"""What the Python scheduler would decide for a recorded payload at a given instant.

The counterpart of `fantasy-go wake`. Both take `now` as an argument on purpose: a
scheduler that can only be observed live cannot be compared, and the whole saving of the
refresh design is that Go must not rebuild when Python would not.

    usage: wake.py <payload.json> <now RFC3339> [tick_s] [last_full|-] [watched]
"""
from __future__ import annotations

import json
import sys
from datetime import datetime
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from fantasy import policies, schedule   # noqa: E402


def main() -> int:
    if len(sys.argv) < 3:
        print(__doc__)
        return 2

    blob = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
    payload = blob.get("universe", blob)
    now = datetime.fromisoformat(sys.argv[2]).timestamp()
    tick = float(sys.argv[3]) if len(sys.argv) > 3 else 120.0
    last_full = (datetime.fromisoformat(sys.argv[4]).timestamp()
                 if len(sys.argv) > 4 and sys.argv[4] != "-" else None)
    watched = len(sys.argv) > 5 and sys.argv[5] == "true"

    armed = policies.load()

    deadlines = schedule.deadlines(payload, now=now, policies=armed)
    print(f"vencimientos: {len(deadlines)}")
    for when, why in deadlines[:10]:
        print(f"  {when - now:+7.0f}s  {why}")

    live_all = schedule.live_matches(payload, now=now)
    live_mine = schedule.live_matches(payload, now=now, mine_only=True)
    print(f"en juego: {len(live_all)} (mios {len(live_mine)})")

    when, why, kind = schedule.next_wake(payload, now=now, tick=tick,
                                         last_full=last_full, watched=watched,
                                         policies=armed)
    print(f"decision: +{when - now:.0f}s {kind} {why}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
