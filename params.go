package main

// Tunable strategy parameters. Defaults are what ships; MECHBOT_PARAMS="key=val,key=val"
// overrides them (used only by the local simulator for A/B tests).

import (
	"os"
	"strconv"
	"strings"
)

var P = map[string]float64{
	"retreat_hp":     0.45, // retreat below this HP fraction when any enemy is near
	"retreat_hp2":    0.60, // ... when 2+ enemies
	"retreat_hp3":    0.75, // ... when 3+ enemies
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
	"w_center":       0.3,
	"w_travel":       0.7,
	"w_cover":        0.9,
	"w_enemy":        1.2,
	"drops":          1, // try to capture uncontested supply drops
	"take_mortar":    0,
	"laser_seek_m":   0,   // travel this far for a known laser pickup while we have none (0 = normal loot range)
	"laser_pre":      0,   // pre-charge the laser on a known enemy without line of sight
	"laser_flee":     1,   // 1 = also charge the laser while retreating / racing the zone (half speed)
	"retreat_zz":     0.6, // lateral weave amplitude while retreating
	"wv_lightning":   10,  // weapon values for loadout choices
	"wv_laser":       9,
	"wv_gun":         6,
	"jet_in_danger":  0,    // allow jet-climb paths while enemies are around
	"regen_hold":     0,    // when hurt and out of contact, hold a sheltered spot to regenerate
	"regen_hold_hp":  0.75, // ... below this HP fraction
	"fight_pos":      1,    // seek spots that hide us from all but the target when 2+ enemies see us
	"nav_nonmain":    2.5,  // path cost multiplier on roofs / stadium stands
	"nav_jet_rise":   7,    // jet-climb edge cost per metre of rise
	"nav_jet_fixed":  70,   // ... plus this fixed cost
	"stuck_esc":      1,    // escalate repeated stuck events (head for ground network)
	"upg_order":      0,    // 0 survival-first, 1 offense-first, 2 mixed
	"ffa_detect":     2,    // number of deaths we didn't cause that switches to the FFA profile (0 = never)
	"evade_n":        3,    // break contact when this many enemies are known nearby
	"evade_n_1w":     99,   // ... same, while holding a single weapon
	"cover_max_m":    66,
	"engage_m_1w":    400, // ... while holding a single weapon
	"lead_smooth":    0.3, // EMA factor for target velocity when leading gun shots (0 = raw)
	"engage_m":       400, // only open fire on mechs within this range (unless shot at / target weak)
}

// FFA overrides: applied once the bot sees mechs dying that it didn't kill (a real free-for-all,
// not the practice squad, whose members never fight each other). MECHBOT_FFA overrides them.
var FFA = map[string]float64{
	"evade_n":     2,
	"aggr_hp":     0.9,
	"engage_m":    100,
	"w_enemy":     2.5,
	"farm_max_t":  120,
	"retreat_hp":  0.55,
	"retreat_hp2": 0.7,
	"retreat_hp3": 0.85,
}

func parseKV(env string, m map[string]float64) {
	for _, kv := range strings.Split(os.Getenv(env), ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok {
			continue
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			m[k] = f
		}
	}
}

func init() {
	parseKV("MECHBOT_PARAMS", P)
	if os.Getenv("MECHBOT_FFA") != "" {
		FFA = map[string]float64{}
		parseKV("MECHBOT_FFA", FFA)
	}
}

func applyFFA() {
	for k, v := range FFA {
		P[k] = v
	}
}
