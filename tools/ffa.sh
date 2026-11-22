#!/bin/bash
# FFA A/B: all variants share the same 16-slot matches (round-robin), compare mean points.
# usage: tools/ffa.sh OUTDIR MATCHES SEED JOBS BOTBIN "name1:params1" "name2:params2" ... [extra:/path/cmd]
#   a spec "name:@/path/to/cmd" uses that command as-is (e.g. another binary or the example bot).
set -u
OUT=$1; N=$2; SEED=$3; JOBS=$4; BIN=$5; shift 5
ROOT=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$OUT"
args=()
for spec in "$@"; do
  name=${spec%%:*}; params=${spec#*:}
  w="$OUT/$name.sh"
  if [[ "$params" == @* ]]; then
    printf '#!/bin/sh\nexec %s\n' "${params#@}" > "$w"
  else
    printf '#!/bin/sh\nMECHBOT_PARAMS=%q exec %q\n' "$params" "$BIN" > "$w"
  fi
  chmod +x "$w"; args+=(--bot "$w")
done
( cd "$ROOT/sim" && ./mechsim --mode ffa "${args[@]}" --matches "$N" --jobs "$JOBS" --seed "$SEED" --out "$OUT/run" > "$OUT/summary.txt" 2>&1 )
awk '/^\[[0-9]+\] /{n=$2; sub(/.*\//,"",n); sub(/\.sh$/,"",n)} /mean points|mean survival|mean kills/{printf "%s %s\n", n, $0}' "$OUT/summary.txt" | sed 's/  */ /g'
