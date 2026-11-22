#!/bin/bash
# A/B test bot variants in the local simulator on identical seeds.
# usage: tools/ab.sh OUTDIR MATCHES SEED MODE JOBS BOTBIN "name1:params1" "name2:params2" ...
#   params are MECHBOT_PARAMS strings (k=v,k=v); empty = defaults.
# Run on a compute node:  srun --account=pas2966 -p debug-nextgen -c 48 -t 55 --mem=32G tools/ab.sh ...
set -u
OUT=$1; N=$2; SEED=$3; MODE=$4; JOBS=$5; BIN=$6; shift 6
ROOT=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$OUT"
for spec in "$@"; do
  name=${spec%%:*}; params=${spec#*:}
  w="$OUT/$name.sh"
  printf '#!/bin/sh\nMECHBOT_PARAMS=%q exec %q\n' "$params" "$BIN" > "$w"; chmod +x "$w"
  ( cd "$ROOT/sim" && ./mechsim --mode "$MODE" --bot "$w" --matches "$N" --jobs "$JOBS" --seed "$SEED" --out "$OUT/$name" > "$OUT/$name.txt" 2>&1 ) &
done
wait
for spec in "$@"; do
  name=${spec%%:*}
  printf '%-14s ' "$name"; grep -E "mean points|mean survival|mean kills|death causes|stuck seconds" "$OUT/$name.txt" | tr -s ' ' | tr '\n' '|'; echo
done
