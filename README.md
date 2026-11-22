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

Scoring is placement only, so the bot plays for survival:

1. **Zone first.** It tracks the shrinking circle (both "phase start" and "instantaneous" readings of `zone`) and always heads for a safe spot inside the *next* circle with margin, hurrying/boosting only when the required speed demands it. Navigation is a 2 m grid over the standable surface with mech clearance, step/slope rules, free drops and jump/jet climb edges (so the stadium field and roofs are reachable), but roofs/stadium stands and climbs are expensive and climbs are disabled near enemies.
2. **Covered positions.** Out of combat it picks goal spots with building cover, inside the next circle, away from recently seen enemies; the torso sweeps toward recent sound/damage bearings and does a 360° look at spawn to find loot.
3. **Loadout and cubes.** Takes lightning (240 dps close range, chains) and laser (hitscan, 315 per full charge), dropping the gun for the second; avoids mortars (laser cook-off). Breaks crates (50 % weapon drop) while a slot is empty, farms NPC drones/crates for cubes when no mechs are around, collects cubes and uncontested supply drops.
4. **Fighting.** Picks the target that dies fastest; leads gun shots with a smoothed target velocity (~90 % hit rate in the simulator), releases the laser only when fully charged and on target, fires lightning inside its cone. Strafes with random reversals, sidesteps bullets predicted to hit and charging lasers aimed at it, dodges mortar impact points, faces enemies (rear hits do ×1.5). With two or more enemies able to see it, it moves to a nearby spot that keeps a shot on its target but hides it from the others.
5. **Evade and retreat.** When three or more enemies are known nearby (squads converge on a sighting), or when hurt, it breaks contact: it weaves to a reachable ground spot, away from the threat centroid, that none of the known enemies can see, firing at whatever stays in view, and regenerates there.
6. **Practice vs free-for-all.** The practice house squad never kills its own members; once the bot sees mechs dying that it didn't kill it switches to a more cautious free-for-all profile (evade at two enemies, close in only when clearly ahead).
7. **Upgrades.** Armor → plating → repair nanites → weapon upgrades.
8. **Robustness.** Single static binary, `GOMAXPROCS=1` (1-CPU quota, 128-thread sandbox), time-capped A* and a per-decision time guard (typical decision < 1 ms), panic recovery that coasts on the last move, newest-observation rule, NaN-safe output.

Strategy constants live in `params.go`; `MECHBOT_PARAMS="key=val,..."` overrides them for local experiments only.

## Local simulator

`sim/` is an approximate re-implementation of the server (movement/collision, zone, vision, sound, all weapons, loot,
upgrades, supply drops) with a 15-bot house squad for practice mode and an FFA mode. On it the kit example bot scores
about 1.05 (official practice baseline 1.21), so scores are roughly comparable.

```sh
(cd sim && go build -o mechsim .)
go build -o mechbot .
(cd sim && ./mechsim --mode practice --bot ../mechbot --matches 48 --jobs 8 --seed 1 --out ../out/run1)
tools/ab.sh out/ab 96 1 practice 8 $PWD/mechbot "base:" "variant:evade_n=2"   # paired A/B on identical seeds
python3 tools/plotrep.py out/run1/replay_match0.jsonl out/rep.png               # needs --replay
```

## Test

```sh
go build -o mechbot . && python3 kit/check/check.py -- ./mechbot
docker build -t my-bot .
python3 kit/check/check.py --map-dir /arena -- docker run --rm -i --network none --cpus 1 --memory 512m \
  --read-only --tmpfs /tmp:rw,size=64m --user 65534:65534 -v "$PWD/kit/arena:/arena:ro" my-bot
```
