package main

// Decision logic. Survival first (placement scoring): stay in the zone, move quietly, use cover,
// grab good weapons (lightning, laser), fight efficiently, retreat to regenerate when hurt.

import (
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

type Track struct {
	ID       int
	Kind     string
	Pos, Vel Vec3
	SVel     Vec3 // smoothed velocity (EMA) for leading shots
	Aim      Vec2
	HP       float64
	MaxHP    float64
	Cubes    float64
	Weapons  []string
	Charging bool
	LastSeen float64
	InObs    bool
	LOS      bool // shot line from our muzzle this tick
	HitUsT   float64
}

type ItemMem struct {
	ID   int
	Kind string
	Pos  Vec3
	Seen float64
}

type Threat struct {
	Bearing float64
	T       float64
	W       float64
	Pos     Vec3 // estimated source position (weapon sounds only)
	HasPos  bool
}

type Bot struct {
	cfg  *Config
	w    *World
	nav  *Nav
	rng  *rand.Rand
	slot int

	t    float64
	o    *Obs
	me   *SelfObs
	pos  Vec3
	eyeH float64
	mzH  float64

	enemies map[int]*Track
	npcs    map[int]*Track
	pickups map[int]*ItemMem
	cubes   map[int]*ItemMem
	threats []Threat

	zoneInstant bool
	lastZone    Zone

	// navigation
	path       []int
	pathIdx    int
	pathGoal   Vec3
	pathT      float64
	goal       Vec3
	goalOK     bool
	goalT      float64
	goalNext   Vec2
	hist       []Vec3
	histMv     []float64
	unstuckT   float64
	unstuckD   Vec2
	stuckN     int
	jetting    bool
	stuckTimes []float64
	corner     Vec3
	kite       Vec3
	kiteOK     bool
	kiteT      float64
	hide       Vec3
	hideOK     bool
	hideT      float64
	cornerOK   bool
	cornerT    float64

	// combat
	targetID    int
	strafe      float64
	strafeUntil float64
	retreat     bool
	cover       Vec3
	coverOK     bool
	coverT      float64
	lastHurtT   float64
	lastHitT    float64
	failTake    map[int]float64
	lastAim     Vec2
	mode        string
	kills       int
	ffa         bool
	shots       map[string]int
	dmgBy       map[string]float64
	hits        map[string]int
	lastStatT   float64
	tickStart   time.Time
	lastDmgLog  float64
	spawnYaw    float64
	spawnSet    bool
}

var traceOn = os.Getenv("MECHBOT_TRACE") != ""

func NewBot(h *Hello) *Bot {
	b := &Bot{cfg: &h.Config, slot: h.Slot, rng: rand.New(rand.NewSource(h.Seed + 7)),
		enemies: map[int]*Track{}, npcs: map[int]*Track{}, pickups: map[int]*ItemMem{}, cubes: map[int]*ItemMem{},
		failTake: map[int]float64{}, targetID: -1, shots: map[string]int{}, hits: map[string]int{}, dmgBy: map[string]float64{}, strafe: 1, lastHurtT: -100, lastHitT: -100}
	dirs := []string{h.Map.Dir, "/arena", "/bot/arena", "kit/arena"}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "arena"), filepath.Join(filepath.Dir(exe), "kit", "arena"))
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		w, err := loadWorld(d)
		if err == nil {
			b.w = w
			break
		}
		logf("map %s: %v", d, err)
	}
	if b.w != nil {
		b.nav = buildNav(b.w)
		b.w.Nav = b.nav
	}
	return b
}

// ---------------------------------------------------------------- helpers

func wrap(a float64) float64 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a < -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

func hdist(a, b Vec3) float64 { return math.Hypot(a[0]-b[0], a[1]-b[1]) }
func dist3(a, b Vec3) float64 {
	return math.Sqrt((a[0]-b[0])*(a[0]-b[0]) + (a[1]-b[1])*(a[1]-b[1]) + (a[2]-b[2])*(a[2]-b[2]))
}
func bearing(from, to Vec3) float64 { return math.Atan2(to[1]-from[1], to[0]-from[0]) }

func norm2(x, y float64) Vec2 {
	l := math.Hypot(x, y)
	if l < 1e-9 {
		return Vec2{0, 0}
	}
	return Vec2{x / l, y / l}
}

func (b *Bot) mechf(k string, def float64) float64 {
	if v, ok := b.me.Stats[k]; ok {
		return v
	}
	if v, ok := b.cfg.Mech[k]; ok {
		return v
	}
	return def
}

func (b *Bot) wcfg(kind, k string, def float64) float64 {
	if m, ok := b.cfg.Weapons[kind]; ok {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return def
}

func wstat(w *WeaponObs, k string, def float64) float64 {
	if w != nil && w.Stats != nil {
		if v, ok := w.Stats[k]; ok {
			return v
		}
	}
	return def
}

func (b *Bot) weapon(kind string) (*WeaponObs, int) {
	for i, w := range b.me.Weapons {
		if w != nil && w.Kind == kind {
			return w, i
		}
	}
	return nil, -1
}

func (b *Bot) scaleOf(cubes float64) float64 {
	return 1 + b.cfg.Cubes.SizePerCube*cubes
}

// ---------------------------------------------------------------- zone

func (b *Bot) zoneAt(tau float64) (Vec2, float64) {
	z := &b.o.Zone
	if tau >= z.ShrinkEnd {
		return z.NextCenter, z.NextRadius
	}
	if b.zoneInstant && b.t >= z.ShrinkStart && b.t < z.ShrinkEnd {
		f := (tau - b.t) / math.Max(1e-3, z.ShrinkEnd-b.t)
		f = clampf(f, 0, 1)
		return Vec2{z.Center[0] + (z.NextCenter[0]-z.Center[0])*f, z.Center[1] + (z.NextCenter[1]-z.Center[1])*f},
			z.Radius + (z.NextRadius-z.Radius)*f
	}
	if tau <= z.ShrinkStart {
		return z.Center, z.Radius
	}
	f := (tau - z.ShrinkStart) / math.Max(1e-3, z.ShrinkEnd-z.ShrinkStart)
	f = clampf(f, 0, 1)
	return Vec2{z.Center[0] + (z.NextCenter[0]-z.Center[0])*f, z.Center[1] + (z.NextCenter[1]-z.Center[1])*f},
		z.Radius + (z.NextRadius-z.Radius)*f
}

// insideAt: margin m inside the zone circle at time tau?
func (b *Bot) insideAt(p Vec3, tau, m float64) bool {
	c, r := b.zoneAt(tau)
	return math.Hypot(p[0]-c[0], p[1]-c[1]) <= r-m
}

// ---------------------------------------------------------------- memory

func (b *Bot) update(o *Obs) {
	if !b.spawnSet {
		b.spawnSet = true
		b.spawnYaw = o.Self.Aim[0]
	}
	b.o = o
	b.t = o.T
	b.me = &o.Self
	b.pos = o.Self.Pos
	sc := b.scaleOf(o.Self.Cubes)
	b.eyeH = b.mechf("eye_height", 8*sc)
	b.mzH = b.mechf("muzzle_height", 7*sc)
	z := o.Zone
	if b.lastZone.NextRadius == z.NextRadius && b.lastZone.NextCenter == z.NextCenter &&
		b.t > z.ShrinkStart+0.05 && b.t < z.ShrinkEnd && math.Abs(b.lastZone.Radius-z.Radius) > 1e-6 {
		b.zoneInstant = true
	}
	b.lastZone = z

	for _, t := range b.enemies {
		t.InObs = false
		t.LOS = false
	}
	for _, t := range b.npcs {
		t.InObs = false
		t.LOS = false
	}
	muzzle := Vec3{b.pos[0], b.pos[1], b.pos[2] + b.mzH}
	for _, a := range o.Mechs {
		m := b.enemies
		if a.Kind == "npc" {
			m = b.npcs
		}
		t := m[a.ID]
		if t == nil {
			t = &Track{ID: a.ID, Kind: a.Kind, HitUsT: -100}
			m[a.ID] = t
		}
		if al := P["lead_smooth"]; al > 0 && t.LastSeen > 0 && b.t-t.LastSeen < 0.5 {
			for k := 0; k < 3; k++ {
				t.SVel[k] += al * (a.Vel[k] - t.SVel[k])
			}
		} else {
			t.SVel = a.Vel
		}
		t.Pos, t.Vel, t.Aim = a.Pos, a.Vel, a.Aim
		if a.HP != nil {
			t.HP = *a.HP
		}
		if a.MaxHP != nil {
			t.MaxHP = *a.MaxHP
		} else if t.MaxHP == 0 {
			t.MaxHP = 1400
		}
		if a.HP == nil && t.HP == 0 {
			t.HP = t.MaxHP
		}
		t.Cubes = a.Cubes
		t.Weapons = a.Weapons
		t.Charging = a.Charging
		t.LastSeen = b.t
		t.InObs = true
		if b.w != nil {
			h := 4.5 * b.scaleOf(a.Cubes)
			if a.Kind == "npc" {
				h = 1.75
			}
			t.LOS = b.w.ShotLOS(muzzle, Vec3{a.Pos[0], a.Pos[1], a.Pos[2] + h}) ||
				b.w.ShotLOS(muzzle, Vec3{a.Pos[0], a.Pos[1], a.Pos[2] + h*1.6})
		} else {
			t.LOS = true
		}
	}
	for id, t := range b.enemies {
		if b.t-t.LastSeen > 40 {
			delete(b.enemies, id)
		}
	}
	for id, t := range b.npcs {
		if b.t-t.LastSeen > 20 {
			delete(b.npcs, id)
		}
	}
	// pickups & cubes memory
	seenP := map[int]bool{}
	for _, p := range o.Pickups {
		seenP[p.ID] = true
		b.pickups[p.ID] = &ItemMem{ID: p.ID, Kind: p.Kind, Pos: p.Pos, Seen: b.t}
	}
	seenC := map[int]bool{}
	for _, c := range o.Cubes {
		seenC[c.ID] = true
		b.cubes[c.ID] = &ItemMem{ID: c.ID, Pos: c.Pos, Seen: b.t}
	}
	// forget items that should be visible (close) but aren't
	if b.w != nil {
		eye := Vec3{b.pos[0], b.pos[1], b.pos[2] + b.eyeH}
		for id, p := range b.pickups {
			if !seenP[id] && hdist(p.Pos, b.pos) < 35 && b.w.SightLOS(eye, Vec3{p.Pos[0], p.Pos[1], p.Pos[2] + 0.5}) {
				delete(b.pickups, id)
			}
		}
		for id, c := range b.cubes {
			if !seenC[id] && (hdist(c.Pos, b.pos) < 35 && b.w.SightLOS(eye, Vec3{c.Pos[0], c.Pos[1], c.Pos[2] + 0.5}) || b.t-c.Seen > 60) {
				delete(b.cubes, id)
			}
		}
	}
	// events
	for _, e := range o.Events {
		switch e.Type {
		case "damaged":
			b.lastHurtT = b.t
			b.dmgBy[e.Weapon] += e.Amount
			if e.Rear {
				b.dmgBy["REAR"] += e.Amount
			}
			b.dmgBy["mode:"+b.mode] += e.Amount
			if e.By != nil {
				if t := b.enemies[int(*e.By)]; t != nil && b.t-t.LastSeen < 1 {
					b.dmgBy[rangeKey("taken", hdist(t.Pos, b.pos))] += e.Amount
				} else {
					b.dmgBy["taken@unseen"] += e.Amount
				}
			}
			if e.Bearing != nil {
				b.threats = append(b.threats, Threat{Bearing: *e.Bearing, T: b.t, W: 3})
			}
			if e.By != nil {
				if t := b.enemies[int(*e.By)]; t != nil {
					t.HitUsT = b.t
				}
				if t := b.npcs[int(*e.By)]; t != nil {
					t.HitUsT = b.t
				}
			}
		case "hit":
			b.lastHitT = b.t
			if e.Target < 100 { // mechs only
				b.hits[e.Weapon]++
				if t := b.enemies[int(e.Target)]; t != nil {
					b.dmgBy[rangeKey("dealt", hdist(t.Pos, b.pos))] += e.Amount
				}
			}
		case "took":
			if id, ok := e.ID.(float64); ok {
				delete(b.pickups, int(id))
			}
		case "take_failed":
			if id, ok := e.ID.(float64); ok {
				b.failTake[int(id)] = b.t
				if e.Reason == "gone" || e.Reason == "have" {
					delete(b.pickups, int(id))
				}
			}
		case "kill":
			b.kills++
			if t := b.enemies[int(e.Target)]; t != nil {
				delete(b.enemies, int(e.Target))
			}
		}
	}
	// free-for-all detection: mechs dying that we didn't kill
	total := int(b.cfg.Match.Mechs)
	if total <= 0 {
		total = 16
	}
	if !b.ffa && P["ffa_detect"] > 0 && b.t <= P["ffa_detect_t"] && float64(total-o.Alive-b.kills) >= P["ffa_detect"] {
		b.ffa = true
		applyFFA()
		logf("t=%.1f: free-for-all detected (alive %d, our kills %d)", b.t, o.Alive, b.kills)
	}
	for _, s := range o.Sounds {
		w := 0.0
		switch s.Kind {
		case "gun", "laser", "lightning", "mortar", "laser_charge":
			w = 2
		case "step", "boost", "jet", "reload":
			w = 1.5
		case "explosion":
			w = 0.7
		}
		if w > 0 {
			if s.Dist == "near" {
				w *= 1.5
			}
			th := Threat{Bearing: s.Bearing, T: b.t - s.Age, W: w}
			if s.Kind == "gun" || s.Kind == "laser" || s.Kind == "lightning" || s.Kind == "mortar" {
				r := 240.0
				switch s.Dist {
				case "near":
					r = 40
				case "mid":
					r = 110
				}
				th.Pos = Vec3{b.pos[0] + math.Cos(s.Bearing)*r, b.pos[1] + math.Sin(s.Bearing)*r, b.pos[2]}
				th.HasPos = true
			}
			b.threats = append(b.threats, th)
		}
	}
	keep := b.threats[:0]
	for _, th := range b.threats {
		if b.t-th.T < 12 {
			keep = append(keep, th)
		}
	}
	b.threats = keep
	if len(b.hist) > 30 {
		b.hist = b.hist[len(b.hist)-30:]
		b.histMv = b.histMv[len(b.histMv)-30:]
	}
}

func rangeKey(pfx string, d float64) string {
	switch {
	case d < 40:
		return pfx + "@000-040"
	case d < 80:
		return pfx + "@040-080"
	case d < 120:
		return pfx + "@080-120"
	case d < 160:
		return pfx + "@120-160"
	}
	return pfx + "@160+"
}

// ---------------------------------------------------------------- upgrades

var upgradePrio = []string{
	"armor", "plating", "nanites", "laser_charge", "lightning_efficiency", "laser_damage", "lightning_range",
	"gun_rate", "gun_damage", "sensors", "lightning_chain", "capacitor", "reactor", "gyros", "servos", "gun_mag",
	"jets", "gun_velocity", "laser_pierce", "mortar_reload", "mortar_splash", "mortar_velocity",
}

var upgradePrioOffense = []string{
	"gun_rate", "gun_damage", "laser_damage", "laser_charge", "lightning_efficiency", "lightning_range", "armor",
	"plating", "nanites", "lightning_chain", "sensors", "gyros", "capacitor", "reactor", "servos", "gun_mag",
	"jets", "gun_velocity", "laser_pierce", "mortar_reload", "mortar_splash", "mortar_velocity",
}

var upgradePrioMixed = []string{
	"armor", "gun_rate", "laser_damage", "lightning_efficiency", "plating", "gun_damage", "laser_charge",
	"nanites", "lightning_range", "lightning_chain", "sensors", "gyros", "capacitor", "reactor", "servos", "gun_mag",
	"jets", "gun_velocity", "laser_pierce", "mortar_reload", "mortar_splash", "mortar_velocity",
}

func (b *Bot) pickUpgrade(act *Action) {
	of := b.o.Offer
	if of == nil || len(of.Choices) == 0 {
		return
	}
	prio := upgradePrio
	switch int(P["upg_order"]) {
	case 1:
		prio = upgradePrioOffense
	case 2:
		prio = upgradePrioMixed
	}
	best, bestR := 0, 1000
	for i, c := range of.Choices {
		r := 500
		for k, id := range prio {
			if id == c.ID {
				r = k
				break
			}
		}
		if r < bestR {
			best, bestR = i, r
		}
	}
	p := best
	act.Pick = &p
}

// ---------------------------------------------------------------- weapons / loot

func wval(kind string) float64 {
	switch kind {
	case "lightning":
		return P["wv_lightning"]
	case "laser":
		return P["wv_laser"]
	case "gun":
		return P["wv_gun"]
	case "mortar":
		return 1
	}
	return 0
}

// lootGain: improvement from taking kind; returns gain and slot to drop (-1 = empty slot).
func (b *Bot) lootGain(kind string) (float64, int) {
	if (kind == "mortar" && P["take_mortar"] == 0) || kind == "" {
		return 0, -1
	}
	if kind == "laser" && P["laser_over_gun"] == 0 {
		if g, _ := b.weapon("gun"); g != nil && b.me.Weapons[0] != nil && b.me.Weapons[1] != nil {
			if l, _ := b.weapon("lightning"); l != nil {
				return 0, -1
			}
		}
	}
	for _, w := range b.me.Weapons {
		if w != nil && w.Kind == kind {
			return 0, -1
		}
	}
	for i, w := range b.me.Weapons {
		if w == nil {
			_ = i
			return wval(kind), -1
		}
	}
	lo, loI := 1e9, -1
	for i, w := range b.me.Weapons {
		if w != nil && wval(w.Kind) < lo {
			// can't swap a charging laser
			if w.Kind == "laser" && w.Charge > 0 {
				continue
			}
			lo, loI = wval(w.Kind), i
		}
	}
	if loI >= 0 && wval(kind) > lo+0.5 {
		return wval(kind) - lo, loI
	}
	return 0, -1
}

func (b *Bot) tryTake(act *Action) {
	r := b.cfg.Loot.PickupRadius
	if r == 0 {
		r = 4
	}
	for _, p := range b.o.Pickups {
		if hdist(p.Pos, b.pos) > r*0.92 || math.Abs(p.Pos[2]-b.pos[2]) > 3.8 {
			continue
		}
		if ft, ok := b.failTake[p.ID]; ok && b.t-ft < 1.0 {
			continue
		}
		g, slot := b.lootGain(p.Kind)
		if g <= 0 {
			continue
		}
		id := p.ID
		act.Take = &id
		if slot >= 0 {
			s := slot
			act.Drop = &s
		}
		return
	}
}

// ---------------------------------------------------------------- main step

// busy: this decision has already used a good part of the 50 ms budget; skip optional searches.
func (b *Bot) busy() bool { return time.Since(b.tickStart) > 20*time.Millisecond }

func (b *Bot) Act(o *Obs) *Action {
	b.tickStart = time.Now()
	b.update(o)
	act := &Action{Tick: o.Tick}
	act.Aim = b.me.Aim
	if b.lastAim != (Vec2{}) {
		act.Aim = b.lastAim
	}
	b.pickUpgrade(act)
	if b.w == nil {
		// no map: walk to zone center, shoot nearest
		c := o.Zone.NextCenter
		d := norm2(c[0]-b.pos[0], c[1]-b.pos[1])
		act.Move = d
		b.fight(act, b.pickTarget())
		return act
	}

	tgt := b.pickTarget()
	b.decideMove(act, tgt)
	b.fight(act, tgt)
	b.tryTake(act)
	b.lastAim = act.Aim
	for i, w := range b.me.Weapons {
		if w != nil && act.Fire[i] && b.targetID >= 0 && w.Cooldown <= 0.1 && (w.Ammo == nil || *w.Ammo > 0) && w.Reload == 0 && w.Kind != "laser" {
			b.shots[w.Kind]++
		}
	}
	if b.t-b.lastStatT >= 30 {
		b.lastStatT = b.t
		logf("stats t=%.0f shots=%v hits=%v kills=%d hp=%.0f cubes=%.0f ffa=%v", b.t, b.shots, b.hits, b.kills, b.me.HP, b.me.Cubes, b.ffa)
	}
	if b.t-b.lastDmgLog >= 2 && len(b.dmgBy) > 0 {
		b.lastDmgLog = b.t
		logf("dmg t=%.1f %v", b.t, b.dmgBy)
	}
	if traceOn {
		tid := -1
		if tgt != nil {
			tid = tgt.ID
		}
		logf("t=%.1f mode=%s pos=(%.0f,%.0f,%.1f) hp=%.0f en=%.0f tgt=%d mv=(%.2f,%.2f) b=%v j=%v aim=(%.2f,%.2f) cur=(%.2f,%.2f) fire=%v take=%v nE=%d",
			b.t, b.mode, b.pos[0], b.pos[1], b.pos[2], b.me.HP, b.me.Energy, tid, act.Move[0], act.Move[1], act.Boost, act.Jump,
			act.Aim[0], act.Aim[1], b.me.Aim[0], b.me.Aim[1], act.Fire, act.Take != nil, len(b.o.Mechs))
	}
	b.hist = append(b.hist, b.pos)
	b.histMv = append(b.histMv, math.Hypot(act.Move[0], act.Move[1]))
	return act
}

// ---------------------------------------------------------------- targeting

func (b *Bot) myDPS(d float64) float64 {
	dps := 0.0
	for _, w := range b.me.Weapons {
		if w == nil {
			continue
		}
		switch w.Kind {
		case "gun":
			if d < wstat(w, "range", 230) {
				dps += 90 * math.Max(0.25, 1-d/300)
			}
		case "laser":
			if d < wstat(w, "range", 280) {
				dps += 120
			}
		case "lightning":
			if d < wstat(w, "range", 45) && b.me.Energy > 6 {
				dps += 220
			}
		}
	}
	return math.Max(dps, 1)
}

func (b *Bot) pickTarget() *Track {
	var best *Track
	bestS := math.Inf(1)
	for _, t := range b.enemies {
		if !t.InObs {
			continue
		}
		d := dist3(t.Pos, b.pos)
		hp := t.HP
		if P["rear_pref"] > 0 {
			// facing away from us: our hits land on its rear armour (x rear_mult)
			if math.Abs(wrap(t.Aim[0]-bearing(t.Pos, b.pos))) > math.Pi/2 {
				hp /= b.mechf("rear_mult", 1.5)
			}
		}
		s := hp/b.myDPS(d) + d/60
		if !t.LOS {
			s += P["nolos_pen"]
		}
		if b.t-t.HitUsT < 3 {
			s -= 1.5
		}
		if t.ID == b.targetID {
			s -= 1.5
		}
		if d > 260 {
			s += 10
		}
		if s < bestS {
			bestS, best = s, t
		}
	}
	if best != nil {
		b.targetID = best.ID
	} else {
		b.targetID = -1
	}
	return best
}

// farmTarget: NPC or crate to shoot when no mechs around.
type farm struct {
	pos  Vec3
	aimZ float64
	vel  Vec3
	npc  bool
	id   int
}

func (b *Bot) farmTarget() *farm {
	muzzle := Vec3{b.pos[0], b.pos[1], b.pos[2] + b.mzH}
	var best *farm
	bestD := math.Inf(1)
	for _, t := range b.npcs {
		if !t.InObs || !t.LOS {
			continue
		}
		d := dist3(t.Pos, b.pos)
		if d > 110 {
			continue
		}
		if d < bestD {
			bestD = d
			best = &farm{pos: t.Pos, aimZ: 1.75, vel: t.Vel, npc: true, id: t.ID}
		}
	}
	if b.t < P["farm_max_t"] && b.me.HP > 0.5*b.me.MaxHP {
		crR, crBonus := 80.0, 20.0
		if b.me.Weapons[1] == nil || b.me.Weapons[0] == nil {
			crR, crBonus = 130, 60 // want a weapon drop: crates first
		}
		for _, c := range b.o.Crates {
			d := dist3(c.Pos, b.pos)
			if d > crR || d+crBonus > bestD {
				continue
			}
			if !b.w.ShotLOS(muzzle, Vec3{c.Pos[0], c.Pos[1], c.Pos[2] + 1.2}) {
				continue
			}
			bestD = d
			best = &farm{pos: c.Pos, aimZ: 1.2, id: c.ID}
		}
	}
	return best
}

// ---------------------------------------------------------------- aiming & firing

// leadAim returns aim yaw/pitch to hit a target point moving at vel with projectile speed s.
func leadAim(from, to, vel Vec3, s float64) (float64, float64, float64) {
	dx, dy, dz := to[0]-from[0], to[1]-from[1], to[2]-from[2]
	a := vel[0]*vel[0] + vel[1]*vel[1] + vel[2]*vel[2] - s*s
	bq := 2 * (dx*vel[0] + dy*vel[1] + dz*vel[2])
	c := dx*dx + dy*dy + dz*dz
	t := 0.0
	if math.Abs(a) < 1e-9 {
		if math.Abs(bq) > 1e-9 {
			t = -c / bq
		}
	} else {
		disc := bq*bq - 4*a*c
		if disc >= 0 {
			sq := math.Sqrt(disc)
			t1 := (-bq - sq) / (2 * a)
			t2 := (-bq + sq) / (2 * a)
			t = math.Inf(1)
			if t1 > 0 {
				t = t1
			}
			if t2 > 0 && t2 < t {
				t = t2
			}
			if math.IsInf(t, 1) {
				t = 0
			}
		}
	}
	if t > 3 {
		t = 3
	}
	px, py, pz := dx+vel[0]*t, dy+vel[1]*t, dz+vel[2]*t
	return math.Atan2(py, px), math.Atan2(pz, math.Hypot(px, py)), t
}

// mortarSolve: yaw/pitch to land a shell (speed v, gravity g) launched from `from` on a target at
// pos moving at vel (horizontal), trying the low arc then the high arc; checks the arc is clear.
func (b *Bot) mortarSolve(from, pos, vel Vec3, v, g float64) (float64, float64, bool) {
	tf := 1.5
	for _, high := range []bool{false, true} {
		var yaw, pitch float64
		ok := false
		for it := 0; it < 4; it++ {
			p := Vec3{pos[0] + vel[0]*tf, pos[1] + vel[1]*tf, pos[2]}
			p[2] = math.Max(p[2], b.w.SurfAt(p[0], p[1])) + 1
			dx := math.Hypot(p[0]-from[0], p[1]-from[1])
			dz := p[2] - from[2]
			disc := v*v*v*v - g*(g*dx*dx+2*dz*v*v)
			if disc < 0 || dx < 1 {
				ok = false
				break
			}
			sq := math.Sqrt(disc)
			tan := (v*v - sq) / (g * dx)
			if high {
				tan = (v*v + sq) / (g * dx)
			}
			pitch = math.Atan(tan)
			yaw = math.Atan2(p[1]-from[1], p[0]-from[0])
			tf = dx / (v * math.Cos(pitch))
			ok = true
		}
		if !ok || pitch > b.mechf("max_pitch", 1.3) || pitch < b.mechf("min_pitch", -0.7) {
			continue
		}
		// trace the arc: must not hit geometry before ~85% of the flight
		cx, cy, cz := from[0], from[1], from[2]
		vx, vy, vz := v*math.Cos(pitch)*math.Cos(yaw), v*math.Cos(pitch)*math.Sin(yaw), v*math.Sin(pitch)
		clear := true
		dt := 0.05
		for t := 0.0; t < tf*0.85; t += dt {
			cx += vx * dt
			cy += vy * dt
			cz += vz * dt
			vz -= g * dt
			if b.w.solidAt(cx, cy, cz) {
				clear = false
				break
			}
		}
		if clear {
			return yaw, pitch, true
		}
	}
	return 0, 0, false
}

func aimErr(cur, want Vec2) (float64, float64) {
	return math.Abs(wrap(want[0] - cur[0])), math.Abs(want[1] - cur[1])
}

func (b *Bot) fight(act *Action, tgt *Track) {
	muzzle := Vec3{b.pos[0], b.pos[1], b.pos[2] + b.mzH}
	var aimPt, vel Vec3
	var tRad, tHalfH float64
	have := false
	los := false
	isMech := false
	if tgt != nil {
		sc := b.scaleOf(tgt.Cubes)
		aimPt = Vec3{tgt.Pos[0], tgt.Pos[1], tgt.Pos[2] + 4.5*sc}
		vel = tgt.Vel
		if P["lead_smooth"] > 0 {
			vel = tgt.SVel
		}
		tRad, tHalfH = 2.5*sc, 4.5*sc
		have, los, isMech = true, tgt.LOS, true
	} else if f := b.farmTarget(); f != nil && b.mode != "zone" && P["farm"] > 0 {
		aimPt = Vec3{f.pos[0], f.pos[1], f.pos[2] + f.aimZ}
		vel = f.vel
		tRad, tHalfH = 1.4, 1.2
		if f.npc {
			tRad, tHalfH = 1.8, 1.75
		}
		have, los = true, true
	}
	_ = isMech
	if have && b.mode == "wary" && tgt != nil && !b.engage(tgt) {
		// stay quiet: track it with the torso but don't give away our position
		act.Aim = Vec2{bearing(muzzle, aimPt), 0}
		b.holdLaser(act, false)
		return
	}
	if !have {
		// scan for threats
		act.Aim = Vec2{b.scanYaw(act), 0}
		b.idleReload(act)
		b.holdLaser(act, false)
		return
	}
	d := dist3(aimPt, muzzle)
	gun, gi := b.weapon("gun")
	las, li := b.weapon("laser")
	lit, ti := b.weapon("lightning")

	// choose aim: laser charging/ready gets hitscan aim; else lightning if close; else gun lead
	hitYaw := bearing(muzzle, aimPt)
	hitPitch := math.Atan2(aimPt[2]-muzzle[2], math.Hypot(aimPt[0]-muzzle[0], aimPt[1]-muzzle[1]))
	// hitscan aim at the position after ~1 tick
	p1 := Vec3{aimPt[0] + vel[0]*0.05, aimPt[1] + vel[1]*0.05, aimPt[2] + vel[2]*0.05}
	scanYaw := bearing(muzzle, p1)
	scanPitch := math.Atan2(p1[2]-muzzle[2], math.Hypot(p1[0]-muzzle[0], p1[1]-muzzle[1]))
	want := Vec2{hitYaw, hitPitch}
	laserActive := las != nil && d < wstat(las, "range", 280) && (las.Charge > 0 || las.Cooldown < 0.3)
	if laserActive && P["aim_mix"] > 0 && gun != nil && las.Charge < wstat(las, "charge", 0.75)*0.65 {
		laserActive = false // early in the charge: aim for the gun (lead), swing to the laser line before release
	}
	litActive := lit != nil && d < wstat(lit, "range", 45)*1.1
	if laserActive || litActive {
		want = Vec2{scanYaw, scanPitch}
	} else if gun != nil {
		spd := wstat(gun, "speed", 252)
		y, p, _ := leadAim(Vec3{muzzle[0], muzzle[1], muzzle[2]}, aimPt, vel, spd)
		want = Vec2{y, p}
	}
	// mortar: if loaded and the laser isn't about to fire, swing to the ballistic solution
	mor, mi := b.weapon("mortar")
	morYaw, morPitch, morOK := 0.0, 0.0, false
	if mor != nil && tgt != nil && d > P["mortar_min_m"] && mor.Cooldown <= 0.3 && !(las != nil && las.Charge > 0) {
		tube := Vec3{b.pos[0], b.pos[1], b.pos[2] + b.mechf("height", 9)*b.scaleOf(b.me.Cubes) + 0.5}
		morYaw, morPitch, morOK = b.mortarSolve(tube, tgt.Pos, tgt.SVel, wstat(mor, "speed", 72), wstat(mor, "gravity", 22))
		if morOK {
			want = Vec2{morYaw, morPitch}
		}
	}
	minP := b.mechf("min_pitch", -0.7)
	maxP := b.mechf("max_pitch", 1.3)
	want[1] = clampf(want[1], minP, maxP)
	act.Aim = want
	cur := b.me.Aim
	if morOK && mor.Cooldown <= 0.05 {
		ey, ep := aimErr(cur, Vec2{morYaw, morPitch})
		if ey < 0.03 && ep < 0.03 {
			act.Fire[mi] = true
		}
	}

	// gun
	if gun != nil {
		spd := wstat(gun, "speed", 252)
		y, p, _ := leadAim(muzzle, aimPt, vel, spd)
		ey, ep := aimErr(cur, Vec2{y, p})
		tolY := math.Atan2(tRad*P["gun_tol"], d) + 0.004
		tolP := math.Atan2(tHalfH*P["gun_tol"], d) + 0.004
		quiet := b.mode == "retreat" && P["retreat_gun_m"] > 0 && d > P["retreat_gun_m"]
		quiet = quiet || b.earlyQuiet(d)
		if los && d < wstat(gun, "range", 230)*0.97 && ey < tolY && ep < tolP && !quiet {
			act.Fire[gi] = true
		}
		if gun.Ammo != nil && *gun.Ammo == 0 && gun.Reload == 0 {
			act.Reload = true
		}
	}
	// lightning
	if lit != nil {
		cone := wstat(lit, "cone_deg", 30) * math.Pi / 180
		rng := wstat(lit, "range", 45)
		ey, _ := aimErr(cur, Vec2{hitYaw, 0})
		cost := wstat(lit, "energy", 3)
		reserve := 0.0
		if tgt == nil {
			reserve = 40 // farming: keep energy
		}
		if los && d < rng*0.93 && ey < cone*P["lit_cone"] && b.me.Energy >= cost+reserve {
			act.Fire[ti] = true
		}
	}
	// laser
	if las != nil {
		rng := wstat(las, "range", 280)
		full := wstat(las, "charge", 0.75)
		ey, ep := aimErr(cur, Vec2{hitYaw, hitPitch})
		tolY := math.Atan2(tRad*P["laser_tol"], d) + 0.002
		tolP := math.Atan2(tHalfH*P["laser_tol"], d) + 0.002
		inRange := d < rng*0.97
		if las.Charge > 0 {
			if las.Charge >= full*0.97 && los && inRange && ey < tolY && ep < tolP {
				act.Fire[li] = false // release: fires now
			} else if b.fleeing() {
				act.Fire[li] = false // let go (partial shot) to get our speed back
			} else {
				act.Fire[li] = true
			}
		} else if los && inRange && las.Cooldown <= 0.05 && (tgt != nil || d < 120) && !b.fleeing() && !b.earlyQuiet(d) {
			act.Fire[li] = true
		} else if P["laser_pre"] > 0 && !los && tgt != nil && d < rng*0.8 && las.Cooldown <= 0.05 && !b.fleeing() {
			act.Fire[li] = true // pre-charge: ready to fire the moment it steps into view
		}
	}
}

// earlyQuiet: slipping away from a spawn contact -- don't give our position away with shots
// at anything not already close.
func (b *Bot) earlyQuiet(d float64) bool {
	return P["early_quiet_m"] > 0 && b.t < P["early_evade_t"] && b.mode == "retreat" && d > P["early_quiet_m"]
}

// fleeing: modes where half-speed laser charging costs us (running away / racing the zone).
func (b *Bot) fleeing() bool {
	return P["laser_flee"] == 0 && (b.mode == "retreat" || b.mode == "zone" || b.mode == "dodge")
}

func (b *Bot) holdLaser(act *Action, _ bool) {
	if las, li := b.weapon("laser"); las != nil && las.Charge > 0 {
		// keep holding until a target shows up (auto-fires after hold_s) -- unless we're running:
		// charging halves our speed, so let it go
		act.Fire[li] = !b.fleeing()
	}
}

func (b *Bot) idleReload(act *Action) {
	if gun, _ := b.weapon("gun"); gun != nil && gun.Ammo != nil && gun.Reload == 0 {
		mag := wstat(gun, "mag", 12)
		if *gun.Ammo < mag*0.6 && b.t-b.lastHitT > 2 {
			act.Reload = true
		}
	}
}

func (b *Bot) scanYaw(act *Action) float64 {
	if b.t < P["spawn_scan_s"] {
		return wrap(b.spawnYaw + b.t*3.6) // look all around once at spawn: find loot and threats
	}
	// look toward the most recent weighted threat bearing, else along movement, sweeping
	var sx, sy float64
	for _, th := range b.threats {
		w := th.W * math.Max(0, 1-(b.t-th.T)/12)
		sx += math.Cos(th.Bearing) * w
		sy += math.Sin(th.Bearing) * w
	}
	// remembered enemies not in view
	for _, t := range b.enemies {
		if b.t-t.LastSeen < 20 {
			w := 2 * math.Max(0, 1-(b.t-t.LastSeen)/20)
			a := bearing(b.pos, t.Pos)
			sx += math.Cos(a) * w
			sy += math.Sin(a) * w
		}
	}
	base := 0.0
	amp := 1.0
	if math.Hypot(sx, sy) > 0.5 {
		base = math.Atan2(sy, sx)
		amp = 0.6
	} else if math.Hypot(act.Move[0], act.Move[1]) > 0.1 {
		base = math.Atan2(act.Move[1], act.Move[0])
		amp = 1.3
	} else {
		c := b.o.Zone.NextCenter
		base = math.Atan2(c[1]-b.pos[1], c[0]-b.pos[0])
		amp = 2.2
	}
	return wrap(base + amp*math.Sin(b.t*1.1))
}
