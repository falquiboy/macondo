#!/usr/bin/env python3
"""Fused rack-goodness / skill report for a Spanish (FILE2017/FISE) GCG.

Runs macondo's rackluck (skill = efficiency, plus a fast luck proxy) and, with
--elise, also Elise's rategame (robust percentile draw-luck, ~9 min), then prints
one combined per-turn table and a per-player verdict.

Usage:
  python3 analizar_partida.py <partida.gcg>            # rapido (solo macondo)
  python3 analizar_partida.py <partida.gcg> --elise    # + suerte robusta de Elise
"""
import json
import os
import re
import subprocess
import sys
from pathlib import Path

# Repo root derived from this file's location (cmd/rackluck/analizar_partida.py),
# so the tool is portable across machines (WSL, macOS, Linux) with no hardcoding.
MACONDO = Path(__file__).resolve().parents[2]
RACKLUCK = MACONDO / "bin" / ("rackluck.exe" if os.name == "nt" else "rackluck")
DATA = Path(os.environ.get("MACONDO_DATA_PATH", MACONDO / "data"))

# Elise lives outside the repo (it is not open-source and not everyone has it).
# Point ELISE_RUNNER at run_rategame.py to enable --elise; otherwise it is skipped
# gracefully. tiledrawrate.txt is written next to the eliseconsole-data dir.
_elise_default = ("/mnt/c/Users/alisf/Documents/Codex/2026-06-03/"
                  "quiero-que-elise-se-rife-un/work/run_rategame.py")
ELISE_RUNNER = Path(os.environ.get("ELISE_RUNNER", _elise_default))
ELISE_OUT = ELISE_RUNNER.parent / "tools" / "eliseconsole-data" / "tiledrawrate.txt"


def run_rackluck(gcg: str) -> dict:
    if not RACKLUCK.exists():
        sys.exit(f"no encuentro el binario {RACKLUCK}; compílalo con "
                 f"'go build -o bin/rackluck ./cmd/rackluck' (o usa analizar-partida.sh, que lo hace).")
    env = {**os.environ, "MACONDO_DATA_PATH": str(DATA)}
    res = subprocess.run([str(RACKLUCK), "-json", gcg], stdout=subprocess.PIPE,
                         stderr=subprocess.DEVNULL, env=env, timeout=120)
    return json.loads(res.stdout.decode("utf-8", "replace"))


def run_elise(gcg: str) -> dict:
    """Returns {turn:int -> percentile:float}. Per-player averages are computed
    from these by the caller using macondo's turn->player map, so engine naming
    differences (Elise 'Player 1' vs GCG 'Player_1') don't matter."""
    if not ELISE_RUNNER.exists():
        print(f"[--elise omitido] no encuentro {ELISE_RUNNER}. "
              f"Define ELISE_RUNNER=<ruta a run_rategame.py> para habilitarlo.", file=sys.stderr)
        return {}
    subprocess.run([sys.executable, str(ELISE_RUNNER), gcg, "rategame", "560"],
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=600)
    pct = {}
    if ELISE_OUT.exists():
        for ln in ELISE_OUT.read_text(encoding="utf-8", errors="replace").splitlines():
            m = re.match(r"^(\d+)\t.*?([\d.]+)%\s*$", ln)
            if m:
                pct[int(m.group(1))] = float(m.group(2))
    return pct


def main():
    if len(sys.argv) < 2:
        print("uso: analizar_partida.py <partida.gcg> [--elise]", file=sys.stderr)
        sys.exit(2)
    gcg = sys.argv[1]
    want_elise = "--elise" in sys.argv[2:]

    rl = run_rackluck(gcg)
    epct = run_elise(gcg) if want_elise else {}

    # Per-player mean Elise percentile, joined on turn number via macondo's
    # turn->player map (robust to engine naming differences).
    eavg = {}
    if want_elise:
        acc = {}
        for t in rl["turns"]:
            if t["turn"] in epct:
                acc.setdefault(t["player"], []).append(epct[t["turn"]])
        eavg = {pi: sum(v) / len(v) for pi, v in acc.items()}

    pnames = [p["name"] for p in rl["players"]]

    print()
    hdr = f'{"#":<4}{"Jugador":<12}{"Atril":<10}{"Jugó":<5}{"Potenc.":>9}{"Pérdida":>9}'
    if want_elise:
        hdr += f'{"SuertE%":>9}'
    print(hdr)
    print("-" * len(hdr))
    for t in rl["turns"]:
        nm = pnames[t["player"]] if t["player"] < len(pnames) else f'P{t["player"]}'
        line = f'{t["turn"]:<4}{nm[:12]:<12}{t["rack"][:10]:<10}{t["action"]:<5}{t["potential"]:>9.1f}{t["loss"]:>9.1f}'
        if want_elise:
            p = epct.get(t["turn"])
            line += f'{p:>8.1f}%' if p is not None else f'{"-":>9}'
        print(line)

    print()
    print("RESUMEN — habilidad vs suerte")
    print("-" * 72)
    sh = f'{"Jugador":<12}{"Turnos":>7}{"PotenMed":>10}{"PérdMed":>9}{"Marcador":>10}'
    if want_elise:
        sh += f'{"SuertE%":>9}'
    print(sh)
    for idx, p in enumerate(rl["players"]):
        line = f'{p["name"][:12]:<12}{p["turns"]:>7}{p["mean_potential"]:>10.1f}{p["mean_loss"]:>9.1f}{p["score"]:>10}'
        if want_elise:
            a = eavg.get(idx)
            line += f'{a:>8.1f}%' if a is not None else f'{"-":>9}'
        print(line)

    print()
    print("Cómo leerlo:")
    print("  PotenMed  mayor = mejores atriles/posiciones (oportunidad).")
    print("  PérdMed   menor = capturó más de ese potencial (habilidad).")
    if want_elise:
        print("  SuertE%   percentil de robo de Elise (50 = promedio; menor = peor bolsa).")

    if len(rl["players"]) == 2:
        a, b = rl["players"]
        skill = a["name"] if a["mean_loss"] <= b["mean_loss"] else b["name"]
        print()
        print(f'Habilidad: {skill} jugó más eficiente '
              f'({min(a["mean_loss"], b["mean_loss"]):.1f} vs {max(a["mean_loss"], b["mean_loss"]):.1f} pérdida media).')
        if want_elise and eavg:
            lucky_idx = max(eavg, key=eavg.get)
            print(f'Suerte:    {pnames[lucky_idx]} tuvo mejor bolsa '
                  f'({max(eavg.values()):.1f}% vs {min(eavg.values()):.1f}% percentil medio de robo).')


if __name__ == "__main__":
    main()
