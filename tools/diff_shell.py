#!/usr/bin/env python3
"""Compare the pieces of the page that are not sections: widgets, header, footer, tabs.

Rendered from arguments rather than from a model, so each one is compared on its own and a
difference has one place to be. The widgets are where this matters most: the league meter is
a bar drawn from a rank, and an off-by-one in the rank moves the bar without changing any
number on screen — the kind of wrong that looks right.

    usage: diff_shell.py [path-to-fantasy-go]
"""
from __future__ import annotations

import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from fantasy import report  # noqa: E402

OTHERS = [130960400, 100600000, 93617740, 55266919, 48869526, 0]


def python_case(case: str) -> str:
    if case == "pestanas":
        # The literal in report.py, extracted rather than duplicated: a copy here would
        # drift and the comparison would then be against the copy.
        source = (ROOT / "fantasy" / "report.py").read_text(encoding="utf-8")
        start = source.index('<div class="tabs" id="tabs"')
        end = source.index("</div>'", start) + len("</div>")
        return source[start:end]

    if case == "pie":
        return "\n".join(footer(weight) for weight in (0, 0.125, 0.5, 1)) + "\n"

    if case == "widgets":
        return "\n".join([
            report._kpi("Jornada 1", "en juego", "J2 desde vie 22 ago 19:30",
                        rank="cierra jue 20 ago 03:00", status="neutral"),
            report._kpi("Mi puesto", "7\u00ba", "121 puntos", rank="7\u00ba de 13",
                        meter=0.5, status="neutral", tab="liga"),
            report._kpi("Mi saldo", "93.62M", "le llega a la mayoria del mercado",
                        rank="3\u00ba de 13", meter=1.0, status="good", tab="liga"),
            report._kpi("Sin rango", "\u2014"),
        ]) + "\n"

    if case == "rangos":
        lines = []
        for value in (130960400, 93617740, 0, 999):
            label, share, status = report._rank_of(value, OTHERS)
            lines.append(f"{value:.0f}|{label}|{share:.6f}|{status}")
        label, share, status = report._rank_of(5, [])
        lines.append(f"vacio|{label}|{share:.6f}|{status}")
        label, share, status = report._rank_of(5, [5])
        lines.append(f"uno|{label}|{share:.6f}|{status}")
        return "\n".join(lines) + "\n"

    if case == "cabecera":
        return header("Liga Fantasy Comité 2026-", 1, two_kpis(), True) + "\n" \
            + header("", 3, [], False) + "\n"

    raise SystemExit(f"caso desconocido: {case}")


def two_kpis() -> list[str]:
    return ['<div class="kpi">uno</div>', '<div class="kpi">dos</div>']


def footer(weight: float) -> str:
    """The footer as report.py builds it, with the one number it interpolates."""
    return (
        "<footer>Datos: API oficial de LaLiga Fantasy y futbolfantasy.com. "
        "<code>xPts</code> es una estimacion propia: puntos por jornada de la temporada pasada "
        f"y de la actual (peso actual {weight:.0%}), ajustados por "
        "probabilidad de ser titular, dificultad del proximo rival y confianza del dato. "
        "<code>est.</code> marca a quien no tiene historico y se estima por precio. "
        "El barrido de valor a 7 dias es una proyeccion amortiguada, no una promesa. "
        "Herramienta de consulta: no ejecuta ninguna operacion.</footer>")


def header(league: str, week: int, kpis: list[str], with_tabs: bool) -> str:
    """The header as report.py builds it, with the same pieces in the same order."""
    tabs = python_case("pestanas") if with_tabs else ""
    return (
        '<header><h1>LaLiga Fantasy · panel de decisiones</h1>'
        f'<p>{report._esc("18/08/2026 16:20")}'
        + (f' · liga <strong>{report._esc(league)}</strong>' if league else "")
        + f' · jornada {report._esc(week)}</p>'
        + '<span class="live"><span id="live-dot" class="live-off"></span>'
          '<span id="live-stamp">estatico</span></span>'
        + "</header>"
        f'<div class="kpis">{"".join(kpis)}</div>'
        + tabs)


CASES = ["pestanas", "pie", "widgets", "rangos", "cabecera"]


def main() -> int:
    binary = sys.argv[1] if len(sys.argv) > 1 else "./fantasy-go"
    if not Path(binary).exists():
        print(f"no encuentro el binario de Go en {binary}")
        return 2

    failures = 0
    for case in CASES:
        python = python_case(case)
        go = subprocess.run([binary, "shell", case], capture_output=True, text=True,
                            cwd=ROOT).stdout
        if python.rstrip("\n") == go.rstrip("\n"):
            print(f"  ok  {case:10} {len(python):>6} bytes identicos")
            continue
        failures += 1
        print(f"  DISTINTO  {case}  py={len(python)} go={len(go)}")
        for left, right in zip(python.splitlines(), go.splitlines()):
            if left != right:
                print(f"      py={left[:150]}")
                print(f"      go={right[:150]}")
                break

    print(f"\n{'VERDE' if not failures else 'ROJO'}: {len(CASES)} piezas, "
          f"{failures} discrepancias")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
