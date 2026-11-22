package main

// Message types for mechfight/1 (see kit/protocol.md and kit/mechfight.schema.json).

type Vec3 [3]float64
type Vec2 [2]float64

type Hello struct {
	Type     string `json:"type"`
	Protocol string `json:"protocol"`
	Slot     int    `json:"slot"`
	Seed     int64  `json:"seed"`
	Map      struct {
		Dir   string   `json:"dir"`
		Files []string `json:"files"`
	} `json:"map"`
	Config Config `json:"config"`
}

type ZonePhase struct {
	HoldS   float64 `json:"hold_s"`
	ShrinkS float64 `json:"shrink_s"`
	Radius  float64 `json:"radius"`
	Dps     float64 `json:"dps"`
}

type UpgradeDef struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Requires string  `json:"requires"`
	Max      float64 `json:"max"`
}

type Config struct {
	Match struct {
		TickHz      float64 `json:"tick_hz"`
		DecideEvery float64 `json:"decide_every"`
		DurationS   float64 `json:"duration_s"`
		Mechs       float64 `json:"mechs"`
	} `json:"match"`
	Bot struct {
		TickBudgetMs float64 `json:"tick_budget_ms"`
	} `json:"bot"`
	Mech   map[string]float64 `json:"mech"`
	Vision struct {
		Range     float64 `json:"range"`
		FovDeg    float64 `json:"fov_deg"`
		NearRange float64 `json:"near_range"`
		MemoryS   float64 `json:"memory_s"`
	} `json:"vision"`
	Weapons map[string]map[string]float64 `json:"weapons"`
	Cubes   struct {
		PickupRadius float64 `json:"pickup_radius"`
		StatCap      float64 `json:"stat_cap"`
		SizePerCube  float64 `json:"size_per_cube"`
	} `json:"cubes"`
	Loot struct {
		PickupRadius float64 `json:"pickup_radius"`
		EquipS       float64 `json:"equip_s"`
	} `json:"loot"`
	Npc  map[string]float64 `json:"npc"`
	Zone struct {
		StartCenter []float64   `json:"start_center"`
		StartRadius float64     `json:"start_radius"`
		Phases      []ZonePhase `json:"phases"`
	} `json:"zone"`
	Drops *struct {
		Times     []float64 `json:"times"`
		FallS     float64   `json:"fall_s"`
		Radius    float64   `json:"radius"`
		CaptureS  float64   `json:"capture_s"`
		CubesGain float64   `json:"cubes"`
	} `json:"drops"`
	Upgrades []UpgradeDef `json:"upgrades"`
}

type WeaponObs struct {
	Kind     string             `json:"kind"`
	Cooldown float64            `json:"cooldown"`
	Ammo     *float64           `json:"ammo"`
	Reload   float64            `json:"reload"`
	Charge   float64            `json:"charge"`
	Stats    map[string]float64 `json:"stats"`
}

type SelfObs struct {
	ID        int                `json:"id"`
	Pos       Vec3               `json:"pos"`
	Vel       Vec3               `json:"vel"`
	Aim       Vec2               `json:"aim"`
	HP        float64            `json:"hp"`
	MaxHP     float64            `json:"max_hp"`
	Energy    float64            `json:"energy"`
	MaxEnergy float64            `json:"max_energy"`
	Grounded  bool               `json:"grounded"`
	Cubes     float64            `json:"cubes"`
	Upgrades  []string           `json:"upgrades"`
	Weapons   []*WeaponObs       `json:"weapons"`
	InZone    bool               `json:"in_zone"`
	Stats     map[string]float64 `json:"stats"`
}

type Actor struct {
	ID       int      `json:"id"`
	Kind     string   `json:"kind"`
	Pos      Vec3     `json:"pos"`
	Vel      Vec3     `json:"vel"`
	Aim      Vec2     `json:"aim"`
	HP       *float64 `json:"hp"`
	MaxHP    *float64 `json:"max_hp"`
	Cubes    float64  `json:"cubes"`
	Weapons  []string `json:"weapons"`
	Charging bool     `json:"charging"`
}

type Proj struct {
	ID   int    `json:"id"`
	Kind string `json:"kind"`
	Pos  Vec3   `json:"pos"`
	Vel  Vec3   `json:"vel"`
}

type Item struct {
	ID  int      `json:"id"`
	Pos Vec3     `json:"pos"`
	HP  *float64 `json:"hp"`
}

type Pickup struct {
	ID   int    `json:"id"`
	Kind string `json:"kind"`
	Pos  Vec3   `json:"pos"`
}

type Sound struct {
	Kind    string  `json:"kind"`
	Bearing float64 `json:"bearing"`
	Dist    string  `json:"dist"`
	Age     float64 `json:"age"`
}

type Beam struct {
	By   int       `json:"by"`
	Kind string    `json:"kind"`
	Path []Vec3    `json:"path"`
	Age  float64   `json:"age"`
}

type Event struct {
	Type    string      `json:"type"`
	Amount  float64     `json:"amount"`
	Weapon  string      `json:"weapon"`
	Bearing *float64    `json:"bearing"`
	By      *float64    `json:"by"`
	Rear    bool        `json:"rear"`
	Target  float64     `json:"target"`
	Effect  string      `json:"effect"`
	ID      interface{} `json:"id"`
	Kind    string      `json:"kind"`
	Slot    float64     `json:"slot"`
	Reason  string      `json:"reason"`
	Cubes   float64     `json:"cubes"`
}

type Drop struct {
	ID       int      `json:"id"`
	Pos      Vec3     `json:"pos"`
	Radius   float64  `json:"radius"`
	LandsIn  float64  `json:"lands_in"`
	CaptureS float64  `json:"capture_s"`
	Progress float64  `json:"progress"`
	Holder   *float64 `json:"holder"`
}

type Zone struct {
	Center      Vec2    `json:"center"`
	Radius      float64 `json:"radius"`
	NextCenter  Vec2    `json:"next_center"`
	NextRadius  float64 `json:"next_radius"`
	ShrinkStart float64 `json:"shrink_start"`
	ShrinkEnd   float64 `json:"shrink_end"`
	Dps         float64 `json:"dps"`
}

type OfferChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Offer struct {
	ID      float64       `json:"id"`
	Choices []OfferChoice `json:"choices"`
	Expires float64       `json:"expires"`
}

type Obs struct {
	Type        string   `json:"type"`
	Tick        float64  `json:"tick"`
	T           float64  `json:"t"`
	Alive       int      `json:"alive"`
	Self        SelfObs  `json:"self"`
	Mechs       []Actor  `json:"mechs"`
	Projectiles []Proj   `json:"projectiles"`
	Crates      []Item   `json:"crates"`
	Cubes       []Item   `json:"cubes"`
	Pickups     []Pickup `json:"pickups"`
	Sounds      []Sound  `json:"sounds"`
	Beams       []Beam   `json:"beams"`
	Events      []Event  `json:"events"`
	Drops       []Drop   `json:"drops"`
	Zone        Zone     `json:"zone"`
	Offer       *Offer   `json:"offer"`
}

type Action struct {
	Tick   float64  `json:"tick"`
	Move   Vec2     `json:"move"`
	Boost  bool     `json:"boost,omitempty"`
	Jump   bool     `json:"jump,omitempty"`
	Aim    Vec2     `json:"aim"`
	Fire   [2]bool  `json:"fire"`
	Reload bool     `json:"reload,omitempty"`
	Pick   *int     `json:"pick,omitempty"`
	Take   *int     `json:"take,omitempty"`
	Drop   *int     `json:"drop,omitempty"`
}
