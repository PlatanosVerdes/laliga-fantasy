#!/usr/bin/env python3
"""Compare the two servers' JSON, live, on the same frozen cache.

The browser code is shared: whichever implementation is serving, the page reads the same
keys out of /api/state and the same messages out of /api/events. So the contract is worth
checking against a running pair rather than against a hand-written schema.

Reuses the model comparator, because the payload rows *are* the model's rows — comparing
them a second way would only mean two places to keep in step.

    usage: diff_api.py http://127.0.0.1:8010 http://127.0.0.1:8020
"""
from __future__ import annotations

import json
import sys
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from diff_model import (compare_activity, compare_cash, compare_lists,  # noqa: E402
                        compare_players)


def fetch(base: str, path: str) -> dict:
    with urllib.request.urlopen(base + path, timeout=60) as answer:
        return json.loads(answer.read())


def main() -> int:
    if len(sys.argv) < 3:
        print(__doc__)
        return 2
    python_base, go_base = sys.argv[1], sys.argv[2]

    print("== /healthz ==")
    for label, base in (("py", python_base), ("go", go_base)):
        health = fetch(base, "/healthz")
        print(f"  {label}: status={health.get('status')} version={health.get('version')} "
              f"runs={health.get('runs')}")
    # Both must answer the fields a probe and a dashboard rely on. Missing ones are listed
    # rather than shrugged at: /healthz is what an alert watches.
    expected = {"status", "version", "generated_at", "age_seconds", "runs", "subscribers",
                "last_error", "last_effect"}
    missing = expected - set(fetch(go_base, "/healthz"))
    if missing:
        print(f"  a /healthz de Go le faltan: {sorted(missing)}")

    py, go = fetch(python_base, "/api/state"), fetch(go_base, "/api/state")

    print("\n== claves de /api/state ==")
    shared = sorted(set(py) & set(go))
    print(f"  en los dos: {shared}")
    pending = sorted(set(py) - set(go))
    if pending:
        print(f"  todavia solo en Python: {pending}")
    extra = sorted(set(go) - set(py))
    if extra:
        print(f"  solo en Go: {extra}")

    print("\n== jugadores ==")
    _, bad_players = compare_players(py, go, 1e-6, 6)
    bad = bad_players
    bad += compare_lists(py, go, "market", "market_id", 1e-6, 6)
    bad += compare_lists(py, go, "fixtures", "id", 1e-6, 6)
    bad += compare_cash(py, go, 1e-6, 6)
    bad += compare_activity(py, go, 1e-6, 6)

    print(f"\n{'VERDE' if not bad else f'ROJO: {bad} diferencias'}")
    return 0 if not bad else 1


if __name__ == "__main__":
    sys.exit(main())
