# ohmech — a Mech Royale bot (`mechfight/1`)

Entry for the Builders Buildathon **Mech Royale** track. The bot is a single static Go binary that speaks the
`mechfight/1` JSON-lines protocol on stdin/stdout (logs on stderr). `Dockerfile` at the top level builds it.

## Layout

| Path | What |
| --- | --- |
| `Dockerfile` | Multi-stage build: Go 1.23 → `scratch` image (~4 MB), runs as uid 65534, map fallback at `/bot/arena`. |
| `main.go` | Protocol loop: newest-observation rule, panic recovery (coast on last move/aim), JSON sanitising. |
| `proto.go` | Message types (from `kit/mechfight.schema.json`). |
| `world.go` | Map model: terrain heightmap, prism rasterisation (1 m), standable surface, line of sight (shots and vision incl. tree crowns). |
| `nav.go` | 2 m navigation grid with mech clearance, step/slope rules, drop edges, components, time-capped weighted A*, cover score. |
| `bot.go` | Memory (enemies, pickups, cubes, threats from sounds/damage), target choice, aiming (projectile lead / hitscan), firing for gun, laser and lightning, loot and upgrade choices. |
| `move.go` | Movement: zone safety, goal selection (cover, near next-circle centre, away from enemies), quiet walking (< 5 m/s makes no step sounds), combat strafing, retreat-to-cover, mortar dodge, laser juke, stuck recovery. |
| `devtools.go` | Offline tools (`-mapstats`, `-astar`). |
| `kit/` | The official bot kit (protocol, schema, conformance check, map). |
| `sim/` | Local approximate simulator used for testing (not part of the image). |

## Strategy

Score is `16 - place`, i.e. the number of mechs that die before us. In practice the 15 house bots are one squad
that escalates: the weakest hunts first, more join as they are thinned out, all of them late. So the bot plays a
series of duels and tries to win each one cheaply:

1. **Fight what is on us, leave bystanders alone.** A mech is "hot" if it hit us recently, points its torso at us within 150 m, charges a laser at us, or is very close; a nearly dead mech in reach is always fair game. Only hot mechs are targets or count as threats.
2. **Gun plus one special, fired together.** The gun is never swapped out; the second slot takes laser > lightning > mortar. The laser is released at full charge, or earlier when the current charge already kills; gun shots are led with a smoothed target velocity (also during the first part of a laser charge); the mortar uses a ballistic solver (lead, low/high arc, clear-arc check).
3. **Loot and cubes.** 360° look at spawn, crates while a slot is empty, drones/crates for cubes when nobody is on us, cubes and weapons from kills, uncontested supply drops.
4. **Positioning.** Covered spots inside the next circle, away from known mechs. With two or more hot enemies it moves to a spot that keeps a shot on its target but hides it from the others; it backs out of hot lightning carriers' range; it sidesteps bullets, charging lasers and mortar impact points.
5. **Evade and retreat.** With three or more hot enemies, or when hurt (HP thresholds by enemy count), it weaves to a ground spot none of them can see and regenerates — but it keeps firing; hiding to heal costs kills.
6. **Zone and navigation.** Always inside the next circle with margin. 2 m nav grid with mech clearance, free drops and jump/jet climbs (stadium field, roofs); no climbing near enemies.
7. **Free-for-all.** When mechs die that we did not kill, it switches to a more cautious profile (evade at two enemies, shorter engagement range).
8. **Robustness.** Static binary, `GOMAXPROCS=1`, time-capped A*, per-decision time guard (typical decision < 1 ms), panic recovery, NaN-safe output.

Measured practice scores on the real scorer as the bot evolved: 3.99 → 4.22 → 4.73 (gate + keep gun) → 5.06 (lethal laser release).

Strategy constants live in `params.go`; `MECHBOT_PARAMS="key=val,..."` overrides them for local experiments only.

## Local simulator

`sim/` is an approximate re-implementation of the server (movement/collision, zone, vision, sound, all weapons, loot,
upgrades, supply drops) with a 15-bot house squad for practice mode and an FFA mode. Use the escalating squad
(`--escalate --house-noise 0.5 --esc-per-kill 1`): calibrated against real scores, and its deltas matched the real
scorer twice (+0.4 predicted / +0.5 real, +0.3 / +0.33). The default (non-escalating) squad sends several hunters at
once and mis-ranks strategies (it favoured kiting, which lost 0.5 on the real scorer).

```sh
(cd sim && go build -o mechsim .)
go build -o mechbot .
(cd sim && ./mechsim --escalate --house-noise 0.5 --esc-per-kill 1 --mode practice --bot ../mechbot --matches 48 --jobs 8 --seed 1 --out ../out/run1)
SIMFLAGS="--escalate --house-noise 0.5 --esc-per-kill 1" tools/ab.sh out/ab 256 1 practice 6 $PWD/mechbot "base:" "variant:evade_n=2"   # paired A/B
python3 tools/plotrep.py out/run1/replay_match0.jsonl out/rep.png               # needs --replay
```

## Test

```sh
go build -o mechbot . && python3 kit/check/check.py -- ./mechbot
docker build -t my-bot .
python3 kit/check/check.py --map-dir /arena -- docker run --rm -i --network none --cpus 1 --memory 512m \
  --read-only --tmpfs /tmp:rw,size=64m --user 65534:65534 -v "$PWD/kit/arena:/arena:ro" my-bot
```
