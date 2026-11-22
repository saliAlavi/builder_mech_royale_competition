package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
)

type Vec3 struct{ X, Y, Z float64 }

func V(x, y, z float64) Vec3        { return Vec3{x, y, z} }
func (a Vec3) Add(b Vec3) Vec3      { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec3) Sub(b Vec3) Vec3      { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec3) Scale(s float64) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }
func (a Vec3) Dot(b Vec3) float64   { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func (a Vec3) Len() float64         { return math.Sqrt(a.X*a.X + a.Y*a.Y + a.Z*a.Z) }
func (a Vec3) HLen() float64        { return math.Hypot(a.X, a.Y) }
func (a Vec3) HDist(b Vec3) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }
func (a Vec3) Dist(b Vec3) float64  { return a.Sub(b).Len() }
func (a Vec3) Norm() Vec3 {
	l := a.Len()
	if l < 1e-12 {
		return Vec3{}
	}
	return a.Scale(1 / l)
}
func dirFromAim(yaw, pitch float64) Vec3 {
	return Vec3{math.Cos(pitch) * math.Cos(yaw), math.Cos(pitch) * math.Sin(yaw), math.Sin(pitch)}
}

func wrapAngle(a float64) float64 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a < -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

func r2(x float64) float64  { return math.Round(x*100) / 100 }
func r3(x float64) float64  { return math.Round(x*1000) / 1000 }
func r1(x float64) float64  { return math.Round(x*10) / 10 }
func v3r(v Vec3) [3]float64 { return [3]float64{r2(v.X), r2(v.Y), r2(v.Z)} }

type ZonePhase struct {
	HoldS   float64 `json:"hold_s"`
	ShrinkS float64 `json:"shrink_s"`
	Radius  float64 `json:"radius"`
	Dps     float64 `json:"dps"`
}

type UpgradeEffect struct {
	Path string   `json:"path"`
	Mul  *float64 `json:"mul"`
	Add  *float64 `json:"add"`
}

type UpgradeDef struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Desc     string          `json:"desc"`
	Max      int             `json:"max"`
	Requires string          `json:"requires"`
	Effects  []UpgradeEffect `json:"effects"`
}

type Config struct {
	Match struct {
		TickHz             int     `json:"tick_hz"`
		DecideEvery        int     `json:"decide_every"`
		DurationS          float64 `json:"duration_s"`
		Mechs              int     `json:"mechs"`
		SpawnMinSeparation float64 `json:"spawn_min_separation"`
		SpawnZoneFraction  float64 `json:"spawn_zone_fraction"`
		ArenaMargin        float64 `json:"arena_margin"`
	} `json:"match"`
	Bot struct {
		TickBudgetMs  float64 `json:"tick_budget_ms"`
		HelloBudgetMs float64 `json:"hello_budget_ms"`
		CoastS        float64 `json:"coast_s"`
		OfferTimeoutS float64 `json:"offer_timeout_s"`
		MaxLineBytes  int     `json:"max_line_bytes"`
	} `json:"bot"`
	Mech   map[string]float64 `json:"mech"`
	Vision struct {
		Range        float64 `json:"range"`
		FovDeg       float64 `json:"fov_deg"`
		NearRange    float64 `json:"near_range"`
		CanopyBlocks bool    `json:"canopy_blocks"`
		ShowEnemyHp  bool    `json:"show_enemy_hp"`
		MemoryS      float64 `json:"memory_s"`
	} `json:"vision"`
	Sound struct {
		BearingNoiseDeg float64            `json:"bearing_noise_deg"`
		Near            float64            `json:"near"`
		Mid             float64            `json:"mid"`
		StepSpeed       float64            `json:"step_speed"`
		StepEvery       float64            `json:"step_every"`
		Range           map[string]float64 `json:"range"`
	} `json:"sound"`
	Weapons    map[string]map[string]float64 `json:"weapons"`
	Projectile struct {
		GunRadius float64 `json:"gun_radius"`
		NpcRadius float64 `json:"npc_radius"`
	} `json:"projectile"`
	Cubes struct {
		PickupRadius      float64 `json:"pickup_radius"`
		HpPerCube         float64 `json:"hp_per_cube"`
		DamagePerCube     float64 `json:"damage_per_cube"`
		StatCap           int     `json:"stat_cap"`
		DeathDropExtra    int     `json:"death_drop_extra"`
		SizePerCube       float64 `json:"size_per_cube"`
		UpgradeThresholds []int   `json:"upgrade_thresholds"`
	} `json:"cubes"`
	Crates struct {
		Count        int     `json:"count"`
		RoofFraction float64 `json:"roof_fraction"`
		Hp           float64 `json:"hp"`
		Radius       float64 `json:"radius"`
		Height       float64 `json:"height"`
		Cubes        int     `json:"cubes"`
	} `json:"crates"`
	Loot struct {
		PickupRadius      float64            `json:"pickup_radius"`
		EquipS            float64            `json:"equip_s"`
		FloorCount        int                `json:"floor_count"`
		FloorRoofFraction float64            `json:"floor_roof_fraction"`
		CrateWeaponChance float64            `json:"crate_weapon_chance"`
		DeathDrop         int                `json:"death_drop"`
		Weights           map[string]float64 `json:"weights"`
	} `json:"loot"`
	Npc struct {
		Count           int     `json:"count"`
		Hp              float64 `json:"hp"`
		Radius          float64 `json:"radius"`
		Height          float64 `json:"height"`
		Speed           float64 `json:"speed"`
		Leash           float64 `json:"leash"`
		ChaseLeash      float64 `json:"chase_leash"`
		AggroRange      float64 `json:"aggro_range"`
		DeaggroRange    float64 `json:"deaggro_range"`
		Damage          float64 `json:"damage"`
		Cooldown        float64 `json:"cooldown"`
		ProjectileSpeed float64 `json:"projectile_speed"`
		SpreadDeg       float64 `json:"spread_deg"`
		Cubes           int     `json:"cubes"`
	} `json:"npc"`
	Drops struct {
		Times    []float64 `json:"times"`
		FallS    float64   `json:"fall_s"`
		Radius   float64   `json:"radius"`
		CaptureS float64   `json:"capture_s"`
		Cubes    int       `json:"cubes"`
		Repair   float64   `json:"repair"`
		Spread   float64   `json:"spread"`
	} `json:"drops"`
	Zone struct {
		StartCenter [2]float64  `json:"start_center"`
		StartRadius float64     `json:"start_radius"`
		Phases      []ZonePhase `json:"phases"`
	} `json:"zone"`
	Upgrades []UpgradeDef `json:"upgrades"`
}

// mechStatOrder is the order of self.stats keys in the real obs.
var mechStatOrder = []string{"radius", "height", "eye_height", "muzzle_height", "hp", "walk_speed", "boost_speed", "ground_accel", "air_accel", "gravity", "jump_speed", "jet_accel", "max_rise_speed", "step_up", "max_slope_deg", "energy", "energy_regen", "energy_regen_delay", "boost_drain", "jet_drain", "turn_rate", "pitch_rate", "min_pitch", "max_pitch", "regen_delay", "regen_per_s", "charge_move_mult", "self_damage_mult", "armor", "rear_mult", "front_arc_deg", "near_range_bonus", "vision_bonus", "hearing"}

var weaponStatOrder = map[string][]string{
	"gun":       {"damage", "cooldown", "speed", "range", "spread_deg", "mag", "reload"},
	"mortar":    {"damage", "splash", "splash_radius", "splash_edge", "cooldown", "speed", "gravity", "ttl", "knock", "knock_up", "drain"},
	"laser":     {"damage", "charge", "min_charge", "min_frac", "hold_s", "cooldown", "range", "pierce", "charge_turn_mult", "cookoff"},
	"lightning": {"damage", "cooldown", "range", "cone_deg", "chain", "chain_range", "chain_falloff", "energy", "short_s"},
}

// LoadConfig reads the hello message (first line) from a jsonl or jsonl.gz file and returns the raw config + parsed.
func LoadConfig(path string) (json.RawMessage, *Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var rd *bufio.Reader
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, nil, err
		}
		rd = bufio.NewReaderSize(gz, 1<<20)
	} else {
		rd = bufio.NewReaderSize(f, 1<<20)
	}
	line, err := rd.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, nil, err
	}
	var hello struct {
		Type   string          `json:"type"`
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(line, &hello); err != nil {
		return nil, nil, err
	}
	raw := hello.Config
	if hello.Type != "hello" {
		// maybe the file is a bare config
		raw = line
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, nil, fmt.Errorf("config: %v", err)
	}
	return raw, &c, nil
}
