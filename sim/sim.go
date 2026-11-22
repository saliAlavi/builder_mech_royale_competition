package main

import (
	"math"
	"math/rand/v2"
	"sort"
)

type Weapon struct {
	Kind     string
	Cooldown float64
	Ammo     int
	Reload   float64
	Charge   float64
	Charging bool
	FullHeld float64
	Release  bool // released before min_charge: fire as soon as min reached
	prevFire bool
}

type Cmd struct {
	Move   [2]float64
	Boost  bool
	Jump   bool
	Yaw    float64
	Pitch  float64
	Fire   [2]bool
	Reload bool
	Pick   int // -1 none
	Take   int // -1 none
	Drop   int // -1 none
}

type Offer struct {
	ID      int
	Expires float64
	Choices []*UpgradeDef
}

type heardSound struct {
	Kind    string
	Bearing float64
	Dist    string
	Age     float64
}

type SoundEv struct {
	Kind string
	Pos  Vec3
	T    float64
	Src  int // mech id or -1
}

type BeamEv struct {
	By   int
	Kind string
	Path []Vec3
	T    float64
}

type Mech struct {
	ID       int
	Squad    int // 0 = none, 1 = house squad
	Ext      *ExtBot
	ExtIdx   int // index of external command, -1 house
	House    *HouseBot
	Name     string
	Pos, Vel Vec3
	Yaw      float64
	Pitch    float64
	HP       float64
	MaxHP    float64
	Energy   float64
	Grounded bool
	Cubes    int
	Upgrades []string
	UpCount  map[string]int
	MS       map[string]float64
	WS       map[string]map[string]float64
	Weapons  [2]*Weapon
	Alive    bool
	DeathT   float64
	Place    int
	Kills    int
	Cause    string

	cmd          Cmd
	lastValidT   float64
	lastSpendT   float64
	lastCombatT  float64
	shortUntil   float64
	stepTimer    float64
	jetTimer     float64
	chargeSndT   float64
	seen         map[int]float64
	events       []map[string]any
	offer        *Offer
	offerQueue   int
	offerCounter int
	lastHitBy    int
	lastWeapon   string
	zoneAccum    float64
	boosting     bool
	jetting      bool

	// stats
	ZoneDamage  float64
	Late        int
	Missing     int
	Answers     int
	StuckSec    float64
	stuckRun    float64
	StuckSpots  [][2]float64
	DmgDealt    float64
	DmgTaken    float64
	MaxRTms     float64
	SumRTms     float64
	InsideTicks int
}

func (m *Mech) scale() float64 { return 1 + 0.05*float64(m.Cubes) }
func (m *Mech) radius() float64 {
	return m.MS["radius"] * m.scale()
}
func (m *Mech) height() float64 { return m.MS["height"] * m.scale() }
func (m *Mech) eye() Vec3       { return m.Pos.Add(V(0, 0, m.MS["eye_height"]*m.scale())) }
func (m *Mech) center() Vec3    { return m.Pos.Add(V(0, 0, m.height()/2)) }
func (m *Mech) top() Vec3       { return m.Pos.Add(V(0, 0, m.height())) }
func (m *Mech) maxEnergy() float64 {
	return m.MS["energy"]
}
func (m *Mech) holds(kind string) int {
	for i, w := range m.Weapons {
		if w != nil && w.Kind == kind {
			return i
		}
	}
	return -1
}
func (m *Mech) charging() bool {
	for _, w := range m.Weapons {
		if w != nil && w.Kind == "laser" && w.Charging {
			return true
		}
	}
	return false
}

type NPC struct {
	ID       int
	Pos, Vel Vec3
	Home     Vec3
	HP       float64
	Yaw      float64
	Target   int // mech id or -1
	Cooldown float64
	Wander   Vec3
	WanderT  float64
	scanT    float64
	Alive    bool
}

type Crate struct {
	ID    int
	Pos   Vec3
	HP    float64
	Alive bool
}

type Cube struct {
	ID    int
	Pos   Vec3
	Alive bool
}

type Pickup struct {
	ID    int
	W     *Weapon
	Pos   Vec3
	Alive bool
}

type Drop struct {
	ID       int
	Pos      Vec3
	CallT    float64
	LandT    float64
	Holder   int
	Progress float64
	Landed   bool
	Alive    bool
}

type Projectile struct {
	ID     int
	Kind   string // gun, mortar, npc
	Pos    Vel3
	Vel    Vec3
	Owner  int // mech id, or npc id (>=100)
	Dist   float64
	Range  float64
	Damage float64
	Born   float64
	Alive  bool
	// mortar stats
	ws map[string]float64
	dm float64 // damage mult
}

type Vel3 = Vec3

type zoneCircle struct {
	C [2]float64
	R float64
}

type Sim struct {
	W       *World
	Nav     *NavGrid
	C       *Config
	Mode    string
	Rng     *rand.Rand
	Tick    int
	T       float64
	DT      float64
	Mechs   []*Mech
	NPCs    []*NPC
	Crates  []*Crate
	Cubes   []*Cube
	Pickups []*Pickup
	Drops   []*Drop
	Projs   []*Projectile
	Sounds  []SoundEv
	Beams   []BeamEv
	circles []zoneCircle
	nextID  map[string]int
	projID  int
	dropIdx int
	// squad knowledge
	squadSeen   map[int]float64
	squadHeard  Vec3
	squadHeardT float64
	squadLast   Vec3
	squadLastOK bool
	hunters     map[int]bool
	pending     []*Mech // deaths this tick
	Over        bool
	navScratch  *NavScratch
	treeSc      TreeScratch
}

func (s *Sim) gauss() float64 { return s.Rng.NormFloat64() }

func (s *Sim) alloc(kind string) int {
	id := s.nextID[kind]
	s.nextID[kind] = id + 1
	return id
}

func (s *Sim) aliveCount() int {
	n := 0
	for _, m := range s.Mechs {
		if m.Alive {
			n++
		}
	}
	return n
}

func copyStats(src map[string]float64) map[string]float64 {
	d := make(map[string]float64, len(src))
	for k, v := range src {
		d[k] = v
	}
	return d
}

func (s *Sim) newWeapon(kind string, m *Mech) *Weapon {
	w := &Weapon{Kind: kind}
	if kind == "gun" {
		mag := s.C.Weapons["gun"]["mag"]
		if m != nil {
			mag = m.WS["gun"]["mag"]
		}
		w.Ammo = int(mag)
	}
	return w
}

// ---------- zone ----------

type ZoneState struct {
	Center, Next           [2]float64
	R, NextR               float64
	ShrinkStart, ShrinkEnd float64
	Dps                    float64
}

func (s *Sim) genZone() {
	c := s.C.Zone
	s.circles = []zoneCircle{{c.StartCenter, c.StartRadius}}
	cur := s.circles[0]
	for _, ph := range c.Phases {
		maxOff := cur.R - ph.Radius
		var nc [2]float64
		for tries := 0; tries < 100; tries++ {
			a := s.Rng.Float64() * 2 * math.Pi
			r := maxOff * math.Sqrt(s.Rng.Float64())
			nc = [2]float64{cur.C[0] + r*math.Cos(a), cur.C[1] + r*math.Sin(a)}
			m := math.Min(ph.Radius*0.5, 120) + 20
			if s.W.InBounds(nc[0], nc[1], m) {
				break
			}
		}
		next := zoneCircle{nc, ph.Radius}
		s.circles = append(s.circles, next)
		cur = next
	}
}

func (s *Sim) zone(t float64) ZoneState {
	t0 := 0.0
	ph := s.C.Zone.Phases
	for i, p := range ph {
		he := t0 + p.HoldS
		se := he + p.ShrinkS
		if t < se {
			z := ZoneState{Next: s.circles[i+1].C, NextR: s.circles[i+1].R, ShrinkStart: he, ShrinkEnd: se, Dps: p.Dps}
			a, b := s.circles[i], s.circles[i+1]
			if t < he {
				z.Center, z.R = a.C, a.R
			} else {
				f := (t - he) / p.ShrinkS
				z.Center = [2]float64{a.C[0] + (b.C[0]-a.C[0])*f, a.C[1] + (b.C[1]-a.C[1])*f}
				z.R = a.R + (b.R-a.R)*f
			}
			return z
		}
		t0 = se
	}
	l := s.circles[len(s.circles)-1]
	return ZoneState{Center: l.C, R: l.R, Next: l.C, NextR: l.R, ShrinkStart: t0, ShrinkEnd: t0, Dps: ph[len(ph)-1].Dps}
}

func (z ZoneState) inside(p Vec3) bool {
	return math.Hypot(p.X-z.Center[0], p.Y-z.Center[1]) <= z.R
}

// ---------- setup ----------

func NewSim(w *World, nav *NavGrid, c *Config, mode string, seed uint64) *Sim {
	s := &Sim{W: w, Nav: nav, C: c, Mode: mode, Rng: rand.New(rand.NewPCG(seed, seed*2654435761+1)), DT: 1.0 / float64(c.Match.TickHz)}
	s.nextID = map[string]int{"npc": 100, "crate": 1000, "cube": 5000, "pickup": 8000, "drop": 20000}
	s.squadSeen = map[int]float64{}
	s.squadHeardT = -100
	if nav != nil {
		s.navScratch = nav.NewScratch()
	}
	s.genZone()
	return s
}

func (s *Sim) randOpenPoint(center [2]float64, rad float64) (Vec3, bool) {
	cells := s.W.openCells
	for tries := 0; tries < 400; tries++ {
		p := cells[s.Rng.IntN(len(cells))]
		if math.Hypot(p[0]-center[0], p[1]-center[1]) <= rad {
			x := p[0] + (s.Rng.Float64()-0.5)*0.8
			y := p[1] + (s.Rng.Float64()-0.5)*0.8
			return V(x, y, s.W.Ground(x, y)), true
		}
	}
	return Vec3{}, false
}

func (s *Sim) randRoofPoint(center [2]float64, rad float64) (Vec3, bool) {
	cells := s.W.roofCells
	for tries := 0; tries < 400; tries++ {
		p := cells[s.Rng.IntN(len(cells))]
		if math.Hypot(p[0]-center[0], p[1]-center[1]) <= rad {
			return V(p[0], p[1], p[2]), true
		}
	}
	return Vec3{}, false
}

func (s *Sim) weightedKind() string {
	kinds := []string{"gun", "mortar", "laser", "lightning"}
	tot := 0.0
	for _, k := range kinds {
		tot += s.C.Loot.Weights[k]
	}
	r := s.Rng.Float64() * tot
	for _, k := range kinds {
		r -= s.C.Loot.Weights[k]
		if r < 0 {
			return k
		}
	}
	return "lightning"
}

func (s *Sim) addMech(id int) *Mech {
	m := &Mech{ID: id, Alive: true, ExtIdx: -1, MS: copyStats(s.C.Mech), WS: map[string]map[string]float64{}, UpCount: map[string]int{}, seen: map[int]float64{}, lastHitBy: -1}
	for k, v := range s.C.Weapons {
		m.WS[k] = copyStats(v)
	}
	m.HP = m.MS["hp"]
	m.MaxHP = m.HP
	m.Energy = m.MS["energy"]
	m.Weapons[0] = s.newWeapon("gun", m)
	m.Grounded = true
	m.lastCombatT = -100
	m.lastSpendT = -100
	m.cmd.Pick, m.cmd.Take, m.cmd.Drop = -1, -1, -1
	m.Upgrades = []string{}
	s.Mechs = append(s.Mechs, m)
	return m
}

func (s *Sim) Setup(n int) {
	c := s.C
	zc := c.Zone.StartCenter
	rad := c.Zone.StartRadius * c.Match.SpawnZoneFraction
	sep := c.Match.SpawnMinSeparation
	var spawns []Vec3
	for len(spawns) < n {
		ok := false
		for tries := 0; tries < 300; tries++ {
			p, good := s.randOpenPoint(zc, rad)
			if !good || !s.W.InBounds(p.X, p.Y, c.Match.ArenaMargin) {
				continue
			}
			far := true
			for _, q := range spawns {
				if p.HDist(q) < sep {
					far = false
					break
				}
			}
			if far {
				spawns = append(spawns, p)
				ok = true
				break
			}
		}
		if !ok {
			sep *= 0.9
		}
	}
	s.Rng.Shuffle(len(spawns), func(i, j int) { spawns[i], spawns[j] = spawns[j], spawns[i] })
	for i := 0; i < n; i++ {
		m := s.addMech(i)
		m.Pos = spawns[i]
		m.Yaw = math.Atan2(zc[1]-m.Pos.Y, zc[0]-m.Pos.X)
		m.cmd.Yaw = m.Yaw
	}
	// floor loot
	for i := 0; i < c.Loot.FloorCount; i++ {
		var p Vec3
		var ok bool
		if s.Rng.Float64() < c.Loot.FloorRoofFraction {
			p, ok = s.randRoofPoint(zc, c.Zone.StartRadius)
		} else {
			p, ok = s.randOpenPoint(zc, c.Zone.StartRadius)
		}
		if ok {
			s.addPickup(s.newWeapon(s.weightedKind(), nil), p)
		}
	}
	for i := 0; i < c.Crates.Count; i++ {
		var p Vec3
		var ok bool
		if s.Rng.Float64() < c.Crates.RoofFraction {
			p, ok = s.randRoofPoint(zc, c.Zone.StartRadius)
		} else {
			p, ok = s.randOpenPoint(zc, c.Zone.StartRadius)
		}
		if ok {
			s.Crates = append(s.Crates, &Crate{ID: s.alloc("crate"), Pos: p, HP: c.Crates.Hp, Alive: true})
		}
	}
	for i := 0; i < c.Npc.Count; i++ {
		p, ok := s.randOpenPoint(zc, c.Zone.StartRadius*0.9)
		if ok {
			s.NPCs = append(s.NPCs, &NPC{ID: s.alloc("npc"), Pos: p, Home: p, HP: c.Npc.Hp, Target: -1, Alive: true, Wander: p})
		}
	}
}

func (s *Sim) addPickup(w *Weapon, p Vec3) *Pickup {
	w.Charging = false
	w.Charge = 0
	pk := &Pickup{ID: s.alloc("pickup"), W: w, Pos: p, Alive: true}
	s.Pickups = append(s.Pickups, pk)
	return pk
}

func (s *Sim) addCube(p Vec3) {
	a := s.Rng.Float64() * 2 * math.Pi
	r := 0.5 + s.Rng.Float64()*1.5
	x, y := p.X+r*math.Cos(a), p.Y+r*math.Sin(a)
	z := s.surfaceBelow(x, y, p.Z+1)
	s.Cubes = append(s.Cubes, &Cube{ID: s.alloc("cube"), Pos: V(x, y, z), Alive: true})
}

// surfaceBelow: highest solid top at (x,y) at or below z.
func (s *Sim) surfaceBelow(x, y, z float64) float64 {
	g := s.W.Ground(x, y)
	best := g
	for _, iv := range s.W.Cell(x, y) {
		if float64(iv.T) <= z+0.01 && float64(iv.T) > best {
			best = float64(iv.T)
		}
	}
	return best
}

func (s *Sim) sound(kind string, p Vec3, src int) {
	s.Sounds = append(s.Sounds, SoundEv{Kind: kind, Pos: p, T: s.T, Src: src})
}

func (s *Sim) event(m *Mech, ev map[string]any) {
	if m != nil && m.Ext != nil {
		m.events = append(m.events, ev)
	}
}

// ---------- tick ----------

func (s *Sim) step() {
	dt := s.DT
	z := s.zone(s.T)
	for _, m := range s.Mechs {
		if !m.Alive {
			continue
		}
		s.slewAim(m, dt)
		s.weaponsTick(m, dt)
	}
	for _, m := range s.Mechs {
		if m.Alive {
			s.moveMech(m, dt)
		}
	}
	s.pushApart()
	for _, m := range s.Mechs {
		if m.Alive {
			r := m.radius()
			if pr := s.probe(m.Pos.X, m.Pos.Y, m.Pos.Z+0.3, r*0.6, m.height()-0.8, 0); pr.blocked {
				m.InsideTicks++
			}
		}
	}
	s.projectilesTick(dt)
	s.npcTick(dt)
	// zone, regen, energy
	for _, m := range s.Mechs {
		if !m.Alive {
			continue
		}
		in := z.inside(m.Pos)
		if !in {
			d := z.Dps * dt
			m.HP -= d
			m.ZoneDamage += d
			m.zoneAccum += d
			m.lastCombatT = s.T
			m.lastWeapon = "zone"
			if m.HP <= 0 {
				s.kill(m, -1, "zone")
			}
		} else if s.T-m.lastCombatT >= m.MS["regen_delay"] && m.HP < m.MaxHP {
			m.HP = math.Min(m.MaxHP, m.HP+m.MS["regen_per_s"]*dt)
		}
		if s.T-m.lastSpendT >= m.MS["energy_regen_delay"] {
			m.Energy = math.Min(m.maxEnergy(), m.Energy+m.MS["energy_regen"]*dt)
		}
		// stuck metric (external bots)
		if m.Ext != nil {
			ml := math.Hypot(m.cmd.Move[0], m.cmd.Move[1])
			if ml > 0.5 && m.Vel.HLen() < 1.5 {
				m.stuckRun += dt
				if m.stuckRun >= 2 {
					if m.stuckRun-dt < 2 {
						m.StuckSec += 2
						if len(m.StuckSpots) < 50 {
							m.StuckSpots = append(m.StuckSpots, [2]float64{r1(m.Pos.X), r1(m.Pos.Y)})
						}
					} else {
						m.StuckSec += dt
					}
				}
			} else {
				m.stuckRun = 0
			}
		}
	}
	s.cubesTick()
	s.dropsTick(dt, z)
	s.offersTick()
	s.resolveDeaths()
	s.Tick++
	s.T = float64(s.Tick) * dt
}

func (s *Sim) resolveDeaths() {
	if len(s.pending) == 0 {
		return
	}
	place := s.aliveCount() + len(s.pending)
	for _, m := range s.pending {
		m.Place = place
		m.DeathT = s.T
	}
	s.pending = s.pending[:0]
}

// kill marks mech dead (placement resolved at end of tick).
func (s *Sim) kill(m *Mech, by int, weapon string) {
	if !m.Alive {
		return
	}
	m.Alive = false
	m.HP = 0
	m.Cause = weapon
	s.pending = append(s.pending, m)
	if by >= 0 && by < len(s.Mechs) && by != m.ID {
		k := s.Mechs[by]
		k.Kills++
		s.event(k, map[string]any{"type": "kill", "target": m.ID})
	}
	s.sound("death", m.center(), m.ID)
	for i := 0; i < m.Cubes+s.C.Cubes.DeathDropExtra; i++ {
		s.addCube(m.Pos)
	}
	for i, w := range m.Weapons {
		if w != nil && w.Kind != "gun" {
			s.addPickup(w, m.Pos.Add(V(float64(i)*1.5-0.75, 0, 0)))
		}
		m.Weapons[i] = nil
	}
}

func (s *Sim) cubesTick() {
	pr := s.C.Cubes.PickupRadius
	for _, c := range s.Cubes {
		if !c.Alive {
			continue
		}
		var best *Mech
		bd := 1e9
		for _, m := range s.Mechs {
			if !m.Alive {
				continue
			}
			d := m.Pos.HDist(c.Pos)
			if d <= pr && math.Abs(m.Pos.Z-c.Pos.Z) <= 4 && d < bd {
				best, bd = m, d
			}
		}
		if best != nil {
			c.Alive = false
			s.giveCubes(best, 1)
			s.event(best, map[string]any{"type": "pickup", "cubes": best.Cubes})
		}
	}
	// compact occasionally
	if s.Tick%40 == 0 {
		cs := s.Cubes[:0]
		for _, c := range s.Cubes {
			if c.Alive {
				cs = append(cs, c)
			}
		}
		s.Cubes = cs
		ps := s.Pickups[:0]
		for _, p := range s.Pickups {
			if p.Alive {
				ps = append(ps, p)
			}
		}
		s.Pickups = ps
	}
}

func (s *Sim) refreshMaxHP(m *Mech) {
	cap := s.C.Cubes.StatCap
	c := m.Cubes
	if c > cap {
		c = cap
	}
	nm := m.MS["hp"] * (1 + s.C.Cubes.HpPerCube*float64(c))
	if nm > m.MaxHP {
		m.HP += nm - m.MaxHP
	}
	m.MaxHP = nm
	if m.HP > m.MaxHP {
		m.HP = m.MaxHP
	}
}

func (s *Sim) giveCubes(m *Mech, n int) {
	for i := 0; i < n; i++ {
		m.Cubes++
		for _, th := range s.C.Cubes.UpgradeThresholds {
			if th == m.Cubes {
				m.offerQueue++
			}
		}
	}
	s.refreshMaxHP(m)
}

func (s *Sim) dmgMult(m *Mech) float64 {
	c := m.Cubes
	if c > s.C.Cubes.StatCap {
		c = s.C.Cubes.StatCap
	}
	return 1 + s.C.Cubes.DamagePerCube*float64(c)
}

// ---------- offers ----------

func (s *Sim) offersTick() {
	for _, m := range s.Mechs {
		if !m.Alive {
			continue
		}
		if m.offer != nil && s.T >= m.offer.Expires {
			s.applyUpgrade(m, m.offer.Choices[0])
			m.offer = nil
		}
		if m.offer == nil && m.offerQueue > 0 {
			m.offerQueue--
			var elig []*UpgradeDef
			for i := range s.C.Upgrades {
				u := &s.C.Upgrades[i]
				if m.UpCount[u.ID] >= u.Max {
					continue
				}
				if u.Requires != "" && m.holds(u.Requires) < 0 {
					continue
				}
				elig = append(elig, u)
			}
			s.Rng.Shuffle(len(elig), func(i, j int) { elig[i], elig[j] = elig[j], elig[i] })
			if len(elig) > 3 {
				elig = elig[:3]
			}
			if len(elig) > 0 {
				m.offerCounter++
				m.offer = &Offer{ID: m.offerCounter, Expires: s.T + s.C.Bot.OfferTimeoutS, Choices: elig}
			}
		}
	}
}

func (s *Sim) pick(m *Mech, i int) {
	if m.offer == nil || i < 0 || i >= len(m.offer.Choices) {
		return
	}
	s.applyUpgrade(m, m.offer.Choices[i])
	m.offer = nil
}

func (s *Sim) applyUpgrade(m *Mech, u *UpgradeDef) {
	m.UpCount[u.ID]++
	m.Upgrades = append(m.Upgrades, u.ID)
	oldE := m.MS["energy"]
	for _, e := range u.Effects {
		var tgt map[string]float64
		var key string
		if len(e.Path) > 5 && e.Path[:5] == "mech." {
			tgt, key = m.MS, e.Path[5:]
		} else if len(e.Path) > 8 && e.Path[:8] == "weapons." {
			rest := e.Path[8:]
			for i := 0; i < len(rest); i++ {
				if rest[i] == '.' {
					tgt = m.WS[rest[:i]]
					key = rest[i+1:]
					break
				}
			}
		}
		if tgt == nil {
			continue
		}
		if e.Mul != nil {
			tgt[key] *= *e.Mul
		}
		if e.Add != nil {
			tgt[key] += *e.Add
		}
	}
	if m.MS["energy"] > oldE {
		m.Energy += m.MS["energy"] - oldE
	}
	s.refreshMaxHP(m)
	s.event(m, map[string]any{"type": "upgrade", "id": u.ID})
}

// ---------- drops ----------

func (s *Sim) dropsTick(dt float64, z ZoneState) {
	c := s.C.Drops
	if s.dropIdx < len(c.Times) && s.T >= c.Times[s.dropIdx] {
		s.dropIdx++
		p, ok := s.randOpenPoint(z.Next, math.Max(z.NextR*c.Spread, 5))
		if !ok {
			p, ok = s.randOpenPoint(z.Next, z.NextR+30)
		}
		if ok {
			s.Drops = append(s.Drops, &Drop{ID: s.alloc("drop"), Pos: p, CallT: s.T, LandT: s.T + c.FallS, Holder: -1, Alive: true})
		}
	}
	for _, d := range s.Drops {
		if !d.Alive {
			continue
		}
		if !d.Landed {
			if s.T >= d.LandT {
				d.Landed = true
				s.sound("drop", d.Pos, -1)
			}
			continue
		}
		var in []*Mech
		for _, m := range s.Mechs {
			if m.Alive && m.Pos.HDist(d.Pos) <= c.Radius && math.Abs(m.Pos.Z-d.Pos.Z) < 6 {
				in = append(in, m)
			}
		}
		if len(in) == 0 {
			d.Holder = -1
			d.Progress = 0
		} else if len(in) == 1 {
			if d.Holder != in[0].ID {
				d.Holder = in[0].ID
				d.Progress = 0
			}
			d.Progress += dt
			if d.Progress >= c.CaptureS {
				m := in[0]
				s.giveCubes(m, c.Cubes)
				m.HP = m.MaxHP
				m.Energy = m.maxEnergy()
				d.Alive = false
				s.event(m, map[string]any{"type": "drop", "id": d.ID, "cubes": m.Cubes})
			}
		}
	}
}

// ---------- placement at end ----------

func (s *Sim) finalize() {
	var surv []*Mech
	for _, m := range s.Mechs {
		if m.Alive {
			surv = append(surv, m)
		}
	}
	sort.Slice(surv, func(i, j int) bool { return surv[i].HP > surv[j].HP })
	for _, m := range surv {
		p := 1
		for _, o := range surv {
			if o.HP > m.HP+1e-9 {
				p++
			}
		}
		m.Place = p
		m.DeathT = s.T
	}
}
