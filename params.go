package main

// Tunable strategy parameters. Defaults are what ships; MECHBOT_PARAMS="key=val,key=val"
// overrides them (used only by the local simulator for A/B tests).

import (
	"os"
	"strconv"
	"strings"
)

var P = map[string]float64{
	"retreat_hp":     0.30, // retreat below this HP fraction when any enemy is near
	"retreat_hp2":    0.42, // ... when 2+ enemies
	"retreat_hp3":    0.60, // ... when 3+ enemies
	"retreat_exit":   0.80, // stop retreating above this
	"aggr_hp":        0.55, // aggressive (close in) only above this HP fraction
	"pref_lightning": 26,
	"pref_laser":     120,
	"pref_gun":       70,
	"farm":           1,   // shoot NPCs / crates when no mechs around
	"farm_max_t":     330, // ... until this match time
	"quiet":          1,   // walk below step-sound speed when nothing is urgent
	"laser_over_gun": 1,   // swap the gun out for a laser
	"loot_early_m":   220,
	"spawn_scan_s":   1.8,
	"loot_m":         70,
	"w_center":       1.0,
	"w_travel":       0.7,
	"w_cover":        0.9,
	"w_enemy":        1.2,
	"drops":          1, // try to capture uncontested supply drops
	"take_mortar":    0,
	"engage_m":       400, // only open fire on mechs within this range (unless shot at / target weak)
}

func init() {
	for _, kv := range strings.Split(os.Getenv("MECHBOT_PARAMS"), ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok {
			continue
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			P[k] = f
		}
	}
}
