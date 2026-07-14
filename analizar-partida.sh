#!/usr/bin/env bash
# Análisis de bondad de atriles (habilidad vs suerte) de una partida FISE/FILE2017.
#
# Uso:
#   bash analizar-partida.sh "<ruta.gcg>"          # rápido (macondo): habilidad + oportunidad
#   bash analizar-partida.sh "<ruta.gcg>" --elise  # + suerte robusta de Elise (~9 min; requiere ELISE_RUNNER)
#
# Portable (WSL / macOS / Linux). Compila bin/rackluck si falta. Para --elise en
# otra máquina, exporta ELISE_RUNNER=<ruta a run_rategame.py>.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Localiza el compilador de Go (PATH primero, luego rutas típicas Linux/macOS).
find_go() {
  if command -v go >/dev/null 2>&1; then command -v go; return; fi
  for c in /usr/local/go/bin/go /opt/homebrew/bin/go /usr/local/bin/go "$HOME/go/bin/go"; do
    [[ -x "$c" ]] && { echo "$c"; return; }
  done
  echo ""
}

bin="$here/bin/rackluck"
[[ "$OSTYPE" == msys* || "$OSTYPE" == cygwin* ]] && bin="$here/bin/rackluck.exe"

if [[ ! -x "$bin" ]]; then
  go_bin="$(find_go)"
  [[ -z "$go_bin" ]] && { echo "No encuentro 'go'. Instálalo o ponlo en el PATH." >&2; exit 1; }
  echo "compilando rackluck con $go_bin ..." >&2
  ( cd "$here" && "$go_bin" build -o "$bin" ./cmd/rackluck )
fi

exec python3 "$here/cmd/rackluck/analizar_partida.py" "$@"
