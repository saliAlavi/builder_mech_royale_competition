package main

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
)

type omap struct {
	keys []string
	vals map[string]float64
}

func (o omap) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for _, k := range o.keys {
		v, ok := o.vals[k]
		if !ok {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, _ := json.Marshal(math.Round(v*10000) / 10000)
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

type WeaponObs struct {
	Kind     string   `json:"kind"`
	Cooldown float64  `json:"cooldown"`
	Stats    omap     `json:"stats"`
	Ammo     *int     `json:"ammo,omitempty"`
	Reload   *float64 `json:"reload,omitempty"`
	Charge   *float64 `json:"charge,omitempty"`
}

type SelfObs struct {
	ID        int        `json:"id"`
	Pos       [3]float64 `json:"pos"`
	Vel       [3]float64 `json:"vel"`
	Aim       [2]float64 `json:"aim"`
	HP        float64    `json:"hp"`
	MaxHP     float64    `json:"max_hp"`
	Energy    float64    `json:"energy"`
	MaxEnergy float64    `json:"max_energy"`
	Grounded  bool       `json:"grounded"`
	Cubes     int        `json:"cubes"`
	Upgrades  []string   `json:"upgrades"`
	Weapons   []any      `json:"weapons"`
	InZone    bool       `json:"in_zone"`
	Stats     omap       `json:"stats"`
}

type ActorObs struct {
	ID       int        `json:"id"`
	Kind     string     `json:"kind"`
	Pos      [3]float64 `json:"pos"`
	Vel      [3]float64 `json:"vel"`
	Aim      [2]float64 `json:"aim"`
	HP       *float64   `json:"hp,omitempty"`
	MaxHP    float64    `json:"max_hp"`
	Cubes    *int       `json:"cubes,omitempty"`
	Weapons  *[]string  `json:"weapons,omitempty"`
	Charging *bool      `json:"charging,omitempty"`
}

type ProjObs struct {
	ID   int        `json:"id"`
	Kind string     `json:"kind"`
	Pos  [3]float64 `json:"pos"`
	Vel  [3]float64 `json:"vel"`
}

type CrateObs struct {
	ID  int        `json:"id"`
	Pos [3]float64 `json:"pos"`
	HP  float64    `json:"hp"`
}
type CubeObs struct {
	ID  int        `json:"id"`
	Pos [3]float64 `json:"pos"`
}
type PickupObs struct {
	ID   int        `json:"id"`
	Kind string     `json:"kind"`
	Pos  [3]float64 `json:"pos"`
}
type SoundObs struct {
	Kind    string  `json:"kind"`
	Bearing float64 `json:"bearing"`
	Dist    string  `json:"dist"`
	Age     float64 `json:"age"`
}
type BeamObs struct {
	By   int          `json:"by"`
	Kind string       `json:"kind"`
	Path [][3]float64 `json:"path"`
	Age  float64      `json:"age"`
}
type ZoneObs struct {
	Center      [2]float64 `json:"center"`
	Radius      float64    `json:"radius"`
	NextCenter  [2]float64 `json:"next_center"`
	NextRadius  float64    `json:"next_radius"`
	ShrinkStart float64    `json:"shrink_start"`
	ShrinkEnd   float64    `json:"shrink_end"`
	Dps         float64    `json:"dps"`
}
type OfferChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Desc string `json:"desc"`
}
type OfferObs struct {
	ID      int           `json:"id"`
	Expires float64       `json:"expires"`
	Choices []OfferChoice `json:"choices"`
}
type DropObs struct {
	ID       int        `json:"id"`
	Pos      [3]float64 `json:"pos"`
	Radius   float64    `json:"radius"`
	LandsIn  float64    `json:"lands_in"`
	CaptureS float64    `json:"capture_s"`
	Progress float64    `json:"progress"`
	Holder   *int       `json:"holder"`
}

type ObsMsg struct {
	Type        string           `json:"type"`
	Tick        int              `json:"tick"`
	T           float64          `json:"t"`
	Alive       int              `json:"alive"`
	Self        SelfObs          `json:"self"`
	Mechs       []ActorObs       `json:"mechs"`
	Projectiles []ProjObs        `json:"projectiles"`
	Crates      []CrateObs       `json:"crates"`
	Cubes       []CubeObs        `json:"cubes"`
	Pickups     []PickupObs      `json:"pickups"`
	Sounds      []SoundObs       `json:"sounds"`
	Beams       []BeamObs        `json:"beams"`
	Events      []map[string]any `json:"events"`
	Zone        ZoneObs          `json:"zone"`
	Offer       *OfferObs        `json:"offer"`
	Drops       []DropObs        `json:"drops"`
}

// canSee: vision rule for mech o looking at any of pts.
func (s *Sim) canSee(o *Mech, pts ...Vec3) bool {
	eye := o.eye()
	near := s.C.Vision.NearRange + o.MS["near_range_bonus"]
	rng := s.C.Vision.Range + o.MS["vision_bonus"]
	half := s.C.Vision.FovDeg * math.Pi / 360
	for _, p := range pts {
		d := p.Dist(eye)
		if d <= near {
			if s.W.LOS(eye, p) {
				return true
			}
		} else if d <= rng {
			b := math.Atan2(p.Y-eye.Y, p.X-eye.X)
			if math.Abs(wrapAngle(b-o.Yaw)) <= half && s.W.LOS(eye, p) {
				if !s.C.Vision.CanopyBlocks || !s.W.Trees.Blocks(&s.treeSc, eye, p) {
					return true
				}
			}
		}
	}
	return false
}

// quick range/fov prefilter (no LOS)
func (s *Sim) inViewCone(o *Mech, p Vec3) bool {
	eye := o.eye()
	d := p.Dist(eye)
	if d <= s.C.Vision.NearRange+o.MS["near_range_bonus"] {
		return true
	}
	if d > s.C.Vision.Range+o.MS["vision_bonus"] {
		return false
	}
	b := math.Atan2(p.Y-eye.Y, p.X-eye.X)
	return math.Abs(wrapAngle(b-o.Yaw)) <= s.C.Vision.FovDeg*math.Pi/360
}

func (s *Sim) selfStats(m *Mech) omap {
	v := copyStats(m.MS)
	sc := m.scale()
	v["radius"] *= sc
	v["height"] *= sc
	v["eye_height"] *= sc
	v["muzzle_height"] *= sc
	return omap{keys: mechStatOrder, vals: v}
}

func (s *Sim) weaponObs(m *Mech, w *Weapon) any {
	if w == nil {
		return nil
	}
	wo := &WeaponObs{Kind: w.Kind, Cooldown: r2(w.Cooldown), Stats: omap{keys: weaponStatOrder[w.Kind], vals: m.WS[w.Kind]}}
	switch w.Kind {
	case "gun":
		a := w.Ammo
		r := r2(math.Max(0, w.Reload))
		wo.Ammo, wo.Reload = &a, &r
	case "laser":
		c := r2(w.Charge)
		wo.Charge = &c
	}
	return wo
}

func (s *Sim) zoneObs(z ZoneState) ZoneObs {
	return ZoneObs{Center: [2]float64{r2(z.Center[0]), r2(z.Center[1])}, Radius: r2(z.R), NextCenter: [2]float64{r2(z.Next[0]), r2(z.Next[1])}, NextRadius: r2(z.NextR), ShrinkStart: r2(z.ShrinkStart), ShrinkEnd: r2(z.ShrinkEnd), Dps: z.Dps}
}

func (s *Sim) buildObs(m *Mech) *ObsMsg {
	z := s.zone(s.T)
	o := &ObsMsg{Type: "obs", Tick: s.Tick, T: r2(s.T), Alive: s.aliveCount(),
		Mechs: []ActorObs{}, Projectiles: []ProjObs{}, Crates: []CrateObs{}, Cubes: []CubeObs{}, Pickups: []PickupObs{},
		Sounds: []SoundObs{}, Beams: []BeamObs{}, Events: []map[string]any{}, Drops: []DropObs{}}
	o.Self = SelfObs{ID: m.ID, Pos: v3r(m.Pos), Vel: v3r(m.Vel), Aim: [2]float64{r3(m.Yaw), r3(m.Pitch)}, HP: r2(m.HP), MaxHP: r2(m.MaxHP),
		Energy: r1(m.Energy), MaxEnergy: r2(m.maxEnergy()), Grounded: m.Grounded, Cubes: m.Cubes, Upgrades: m.Upgrades,
		Weapons: []any{s.weaponObs(m, m.Weapons[0]), s.weaponObs(m, m.Weapons[1])}, InZone: z.inside(m.Pos), Stats: s.selfStats(m)}
	mem := s.C.Vision.MemoryS
	for _, x := range s.Mechs {
		if x == m || !x.Alive {
			continue
		}
		vis := s.inViewCone(m, x.center()) || s.inViewCone(m, x.top())
		if vis {
			vis = s.canSee(m, x.center(), x.top())
		}
		if vis {
			m.seen[x.ID] = s.T
		}
		if t, ok := m.seen[x.ID]; vis || (ok && s.T-t <= mem) {
			a := ActorObs{ID: x.ID, Kind: "mech", Pos: v3r(x.Pos), Vel: v3r(x.Vel), Aim: [2]float64{r3(x.Yaw), r3(x.Pitch)}, MaxHP: r2(x.MaxHP)}
			if s.C.Vision.ShowEnemyHp {
				h := r2(x.HP)
				a.HP = &h
			}
			c := x.Cubes
			a.Cubes = &c
			ws := []string{}
			for _, w := range x.Weapons {
				if w != nil {
					ws = append(ws, w.Kind)
				}
			}
			a.Weapons = &ws
			ch := x.charging()
			a.Charging = &ch
			o.Mechs = append(o.Mechs, a)
		}
	}
	for _, n := range s.NPCs {
		if !n.Alive {
			continue
		}
		c := n.Pos.Add(V(0, 0, s.C.Npc.Height/2))
		tp := n.Pos.Add(V(0, 0, s.C.Npc.Height))
		vis := s.inViewCone(m, c) && s.canSee(m, c, tp)
		if vis {
			m.seen[n.ID] = s.T
		}
		if t, ok := m.seen[n.ID]; vis || (ok && s.T-t <= mem) {
			h := r2(n.HP)
			o.Mechs = append(o.Mechs, ActorObs{ID: n.ID, Kind: "npc", Pos: v3r(n.Pos), Vel: v3r(n.Vel), Aim: [2]float64{r3(n.Yaw), 0}, HP: &h, MaxHP: s.C.Npc.Hp})
		}
	}
	for _, p := range s.Projs {
		if !p.Alive || p.Owner == m.ID {
			continue
		}
		if s.inViewCone(m, p.Pos) && s.canSee(m, p.Pos) {
			o.Projectiles = append(o.Projectiles, ProjObs{ID: p.ID, Kind: p.Kind, Pos: v3r(p.Pos), Vel: v3r(p.Vel)})
		}
	}
	for _, c := range s.Crates {
		if c.Alive {
			cc := c.Pos.Add(V(0, 0, s.C.Crates.Height/2))
			if s.inViewCone(m, cc) && s.canSee(m, cc, c.Pos.Add(V(0, 0, s.C.Crates.Height))) {
				o.Crates = append(o.Crates, CrateObs{ID: c.ID, Pos: v3r(c.Pos), HP: r2(c.HP)})
			}
		}
	}
	for _, c := range s.Cubes {
		if c.Alive {
			cc := c.Pos.Add(V(0, 0, 0.5))
			if s.inViewCone(m, cc) && s.canSee(m, cc) {
				o.Cubes = append(o.Cubes, CubeObs{ID: c.ID, Pos: v3r(c.Pos)})
			}
		}
	}
	for _, p := range s.Pickups {
		if p.Alive {
			cc := p.Pos.Add(V(0, 0, 0.5))
			if s.inViewCone(m, cc) && s.canSee(m, cc, p.Pos.Add(V(0, 0, 1.2))) {
				o.Pickups = append(o.Pickups, PickupObs{ID: p.ID, Kind: p.W.Kind, Pos: v3r(p.Pos)})
			}
		}
	}
	// sounds
	noise := s.C.Sound.BearingNoiseDeg * m.MS["hearing"] * math.Pi / 180
	for _, sd := range s.Sounds {
		if sd.Src == m.ID {
			continue
		}
		d := sd.Pos.Dist(m.Pos)
		if d > s.C.Sound.Range[sd.Kind] {
			continue
		}
		cat := "far"
		if d < s.C.Sound.Near {
			cat = "near"
		} else if d < s.C.Sound.Mid {
			cat = "mid"
		}
		b := wrapAngle(math.Atan2(sd.Pos.Y-m.Pos.Y, sd.Pos.X-m.Pos.X) + s.gauss()*noise)
		o.Sounds = append(o.Sounds, SoundObs{Kind: sd.Kind, Bearing: r3(b), Dist: cat, Age: r2(s.T - sd.T)})
	}
	for _, bm := range s.Beams {
		if bm.By == m.ID {
			continue
		}
		pts := append([]Vec3{}, bm.Path...)
		for k := 0; k+1 < len(bm.Path); k++ {
			pts = append(pts, bm.Path[k].Add(bm.Path[k+1]).Scale(0.5))
		}
		if s.canSee(m, pts...) {
			pa := make([][3]float64, len(bm.Path))
			for k, p := range bm.Path {
				pa[k] = v3r(p)
			}
			o.Beams = append(o.Beams, BeamObs{By: bm.By, Kind: bm.Kind, Path: pa, Age: r2(s.T - bm.T)})
		}
	}
	if m.zoneAccum > 0 {
		m.events = append(m.events, map[string]any{"type": "damaged", "amount": r2(m.zoneAccum), "weapon": "zone", "bearing": nil, "by": nil})
		m.zoneAccum = 0
	}
	o.Events = append(o.Events, m.events...)
	m.events = m.events[:0]
	o.Zone = s.zoneObs(z)
	if m.offer != nil {
		of := &OfferObs{ID: m.offer.ID, Expires: r2(m.offer.Expires)}
		for _, c := range m.offer.Choices {
			of.Choices = append(of.Choices, OfferChoice{c.ID, c.Name, c.Desc})
		}
		o.Offer = of
	}
	for _, d := range s.Drops {
		if !d.Alive {
			continue
		}
		do := DropObs{ID: d.ID, Pos: v3r(d.Pos), Radius: s.C.Drops.Radius, LandsIn: r2(math.Max(0, d.LandT-s.T)), CaptureS: s.C.Drops.CaptureS, Progress: r2(d.Progress)}
		if d.Holder >= 0 {
			h := d.Holder
			do.Holder = &h
		}
		o.Drops = append(o.Drops, do)
	}
	return o
}

// ---------- actions ----------

type ActionMsg struct {
	Tick   *int        `json:"tick"`
	Move   *[2]float64 `json:"move"`
	Boost  *bool       `json:"boost"`
	Jump   *bool       `json:"jump"`
	Aim    *[2]float64 `json:"aim"`
	Fire   *[2]bool    `json:"fire"`
	Reload *bool       `json:"reload"`
	Pick   *int        `json:"pick"`
	Take   *int        `json:"take"`
	Drop   *int        `json:"drop"`
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// applyAction sets the mech's command from a valid action.
func (s *Sim) applyAction(m *Mech, a *ActionMsg) {
	c := &m.cmd
	c.Move = [2]float64{}
	if a.Move != nil && finite(a.Move[0]) && finite(a.Move[1]) {
		c.Move = *a.Move
	}
	c.Boost = a.Boost != nil && *a.Boost
	c.Jump = a.Jump != nil && *a.Jump
	if a.Aim != nil && finite(a.Aim[0]) && finite(a.Aim[1]) {
		c.Yaw, c.Pitch = wrapAngle(a.Aim[0]), a.Aim[1]
	}
	c.Fire = [2]bool{}
	if a.Fire != nil {
		c.Fire = *a.Fire
	}
	c.Reload = a.Reload != nil && *a.Reload
	c.Pick, c.Take, c.Drop = -1, -1, -1
	if a.Pick != nil {
		c.Pick = *a.Pick
	}
	if a.Take != nil {
		c.Take = *a.Take
	}
	if a.Drop != nil {
		c.Drop = *a.Drop
	}
	m.lastValidT = s.T
}

// coast: no valid answer this decision.
func (s *Sim) coast(m *Mech) {
	c := &m.cmd
	c.Jump, c.Reload, c.Fire = false, false, [2]bool{}
	c.Pick, c.Take, c.Drop = -1, -1, -1
	if s.T-m.lastValidT > s.C.Bot.CoastS {
		c.Move = [2]float64{}
		c.Boost = false
		c.Yaw, c.Pitch = m.Yaw, m.Pitch
	}
}

type takeReq struct {
	m  *Mech
	pk *Pickup
	d  float64
}

// oneShots processes pick/take/drop for all mechs (once per decision).
func (s *Sim) oneShots() {
	var reqs []takeReq
	for _, m := range s.Mechs {
		if !m.Alive {
			continue
		}
		if m.cmd.Pick >= 0 {
			s.pick(m, m.cmd.Pick)
		}
		if m.cmd.Take >= 0 {
			var pk *Pickup
			for _, p := range s.Pickups {
				if p.ID == m.cmd.Take && p.Alive {
					pk = p
					break
				}
			}
			if pk == nil {
				s.event(m, map[string]any{"type": "take_failed", "id": m.cmd.Take, "reason": "gone"})
			} else {
				reqs = append(reqs, takeReq{m, pk, m.Pos.HDist(pk.Pos)})
			}
		} else if m.cmd.Drop == 0 || m.cmd.Drop == 1 {
			w := m.Weapons[m.cmd.Drop]
			if w != nil && !(w.Kind == "laser" && w.Charging) {
				m.Weapons[m.cmd.Drop] = nil
				pk := s.addPickup(w, m.Pos)
				s.event(m, map[string]any{"type": "dropped", "id": pk.ID, "kind": w.Kind, "slot": m.cmd.Drop})
			}
		}
	}
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].d < reqs[j].d })
	for _, r := range reqs {
		m, pk := r.m, r.pk
		fail := func(reason string) {
			s.event(m, map[string]any{"type": "take_failed", "id": pk.ID, "reason": reason})
		}
		if !pk.Alive {
			fail("gone")
			continue
		}
		if r.d > s.C.Loot.PickupRadius || math.Abs(m.Pos.Z-pk.Pos.Z) > 4 {
			fail("range")
			continue
		}
		slot := -1
		if m.cmd.Drop == 0 || m.cmd.Drop == 1 {
			slot = m.cmd.Drop
		}
		if h := m.holds(pk.W.Kind); h >= 0 && h != slot {
			fail("have")
			continue
		}
		if slot < 0 {
			for i, w := range m.Weapons {
				if w == nil {
					slot = i
					break
				}
			}
			if slot < 0 {
				fail("full")
				continue
			}
		}
		old := m.Weapons[slot]
		if old != nil && old.Kind == "laser" && old.Charging {
			fail("busy")
			continue
		}
		pk.Alive = false
		w := pk.W
		w.Cooldown = math.Max(w.Cooldown, math.Max(s.C.Loot.EquipS, m.WS[w.Kind]["cooldown"]))
		m.Weapons[slot] = w
		s.event(m, map[string]any{"type": "took", "id": pk.ID, "kind": w.Kind, "slot": slot})
		if old != nil {
			np := s.addPickup(old, m.Pos)
			s.event(m, map[string]any{"type": "dropped", "id": np.ID, "kind": old.Kind, "slot": slot})
		}
	}
}
