#!/usr/bin/env python3
"""Compare the whole page from each implementation's *own* model.

The other harnesses hand both sides the same input. This one does not: each builds the world
itself — its own API client, its own scrapers, its own matcher, its own advice layer — and the
two documents are compared. It is the only comparison that can catch a difference in what the
two decide to fetch, and the only one that says the port is finished.

Clock-derived text is masked, and nothing else: the generation stamp, the fractional hours in
sort keys, and the countdowns in hours and days. Everything else must match byte for byte.

    usage: diff_full.py [path-to-fantasy-go]
"""
from __future__ import annotations

import re
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def mask(text: str) -> str:
    text = re.sub(r"<p>[^<·]*·", "<p>FECHA ·", text, count=1)
    text = re.sub(r"\d+\.\d{6,}", "RELOJ", text)
    text = re.sub(r"(\d+)h\b", "Hh", text)
    text = re.sub(r"(\d+\.\d)d\b", "Dd", text)
    return text


def main() -> int:
    binary = sys.argv[1] if len(sys.argv) > 1 else "./fantasy-go"
    budget = sys.argv[2] if len(sys.argv) > 2 else None
    if not Path(binary).exists():
        print(f"no encuentro el binario de Go en {binary}")
        return 2

    scratch = Path(tempfile.mkdtemp())
    python_page, go_page = scratch / "py.html", scratch / "go.html"

    subprocess.run([sys.executable, "fantasy.py", "report", "--output", str(python_page)],
                   capture_output=True, text=True, cwd=ROOT)
    command = [binary, "report", "--generado", "X", "--output", str(go_page)]
    if budget:
        command += ["--budget", budget]
    subprocess.run(command, capture_output=True, text=True, cwd=ROOT)

    if not python_page.exists() or not go_page.exists():
        print("una de las dos no ha generado la pagina")
        return 1

    python, go = mask(python_page.read_text()), mask(go_page.read_text())
    print(f"  py {len(python)} bytes · go {len(go)} bytes")
    if python == go:
        print("\nVERDE: la pagina es identica, y cada uno ha construido su propio mundo")
        return 0

    shortest = min(len(python), len(go))
    index = next((i for i in range(shortest) if python[i] != go[i]), shortest)
    print(f"  primer byte distinto: {index}")
    print(f"    py: {python[index:index + 140]}")
    print(f"    go: {go[index:index + 140]}")
    print("\nROJO")
    return 1


if __name__ == "__main__":
    sys.exit(main())
