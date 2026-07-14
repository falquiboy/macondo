# rackluck — bondad de atriles (habilidad vs suerte) desde un GCG

Descompone una partida (GCG) de Scrabble español (FISE / FILE2017) en dos ejes:

- **Habilidad (eficiencia):** cuánto del potencial de cada atril capturó el jugador
  (equity de la mejor jugada estática menos la equity de lo que jugó), usando el KLV
  español (`data/strategy/FILE2017/leaves.klv2`).
- **Suerte (oportunidad):** qué tan buenos fueron los atriles. Un proxy rápido lo da
  el propio `rackluck` (potencial medio + luck por muestreo contrafactual); la versión
  robusta la aporta el comando `rategame` de la CLI de Elise (percentil de robo, 4000
  sims/robo), fusionada por número de turno.

Meta: reconocer al mejor jugador **pese a su mala suerte** — "quién hizo más con menos".

## Uso

Desde la raíz del repo, en un shell Unix (WSL, macOS, Linux):

```bash
bash analizar-partida.sh "/ruta/a/partida.gcg"           # rápido (segundos)
bash analizar-partida.sh "/ruta/a/partida.gcg" --elise   # + suerte de Elise (~9 min)
```

El wrapper compila `bin/rackluck` si falta. `rackluck` suelto (flags **antes** del gcg,
Go deja de leer flags tras el primer posicional):

```bash
MACONDO_DATA_PATH=./data ./bin/rackluck -json "/ruta/a/partida.gcg"
# flags: -json  -samples N (100)  -seed N (1, determinista)  -lexicon FILE2017  -quiet
```

## Montaje en otra máquina (p. ej. una Mac)

1. `git clone` de este fork y `cd macondo`.
2. **Datos con copyright (no están en git):** copiar a `data/`:
   - `data/lexica/gaddag/FILE2017.kwg`
   - `data/strategy/FILE2017/leaves.klv2` (si no viniera ya trackeado)
   Y la distribución `data/letterdistributions/spanish` (viene con macondo).
3. Requisitos: Go (para compilar), Python 3.
4. Correr `bash analizar-partida.sh <gcg>`.

## Modo `--elise` (opcional)

Elise no es open-source y vive fuera del repo. Para habilitar el eje de suerte robusto:

```bash
export ELISE_RUNNER=/ruta/a/run_rategame.py   # driver de la consola de Elise
bash analizar-partida.sh <gcg> --elise
```

Sin `ELISE_RUNNER` válido, `--elise` se omite con un aviso y el resto del reporte
funciona igual.
