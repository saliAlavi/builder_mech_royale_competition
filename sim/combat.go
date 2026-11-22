package main

import (
	"math"
	"sort"
)

// segCyl returns the smallest t in [0,1] where segment a->b enters the vertical cylinder, or -1.
func segCyl(a, b Vec3, cx, cy, R, z0, z1 float64) float64 {
	dx, dy, dz := b.X-a.X, b.Y-a.Y, b.Z-a.Z
	ox, oy := a.X-cx, a.Y-cy
	lo, hi := 0.0, 1.0
	A := dx*dx + dy*dy
	C := ox*ox + oy*oy - R*R
	if A < 1e-12 {
		if C > 0 {
			return -1
		}
	} else {
		B := 2 * (ox*dx + oy*dy)
		disc := B*B - 4*A*C
		if disc < 0 {
			return -1
		}
		sq := math.Sqrt(disc)
		t1 := (-B - sq) / (2 * A)
		t2 := (-B + sq) / (2 * A)
		lo = math.Max(lo, t1)
		hi = math.Min(hi, t2)
	}
	if math.Abs(dz) < 1e-12 {
		if a.Z < z0 || a.Z > z1 {
			return -1
		}
	} else {
		t1 := (z0 - a.Z) / dz
		t2 := (z1 - a.Z) / dz
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		lo = math.Max(lo, t1)
		hi = math.Min(hi, t2)
	}
	if lo > hi {
		return -1
	}
	return lo
}

func (s *Sim) sameSquad(by int, t *Mech) bool {
	if by < 0 || by >= len(s.Mechs) {
		return false
	}
	a := s.Mechs[by]
	return a.Squad != 0 && a.Squad == t.Squad && a.ID != t.ID
}

// hurtMech applies weapon damage. src: world point the damage came from.
func (s *Sim) hurtMech(t *Mech, amount float64, weapon string, by int, src Vec3, effect string) float64 {
	if !t.Alive || s.sameSquad(by, t) {
		return 0
	}
	bearing := math.Atan2(src.Y-t.Pos.Y, src.X-t.Pos.X)
	rear := false
	if math.Abs(wrapAngle(bearing-t.Yaw)) > t.MS["front_arc_deg"]*math.Pi/360 {
		rear = true
		amount *= t.MS["rear_mult"]
	}
	if weapon != "zone" {
		amount *= math.Max(0, 1-t.MS["armor"])
	}
	t.HP -= amount
	t.DmgTaken += amount
	t.lastCombatT = s.T
	t.lastWeapon = weapon
	ev := map[string]any{"type": "damaged", "amount": r2(amount), "weapon": weapon, "bearing": r3(bearing), "by": by}
	if rear {
		ev["rear"] = true
	}
	s.event(t, ev)
	if by >= 0 && by < len(s.Mechs) {
		a := s.Mechs[by]
		a.lastCombatT = s.T
		a.DmgDealt += amount
		hv := map[string]any{"type": "hit", "target": t.ID, "amount": r2(amount), "weapon": weapon}
		if effect != "" {
			hv["effect"] = effect
		}
		if rear {
			hv["rear"] = true
		}
		s.event(a, hv)
	}
	if t.House != nil && by >= 0 {
		t.House.onDamaged(s, by, src)
	}
	if t.HP <= 0 {
		killer := -1
		if by >= 0 && by < len(s.Mechs) {
			killer = by
		}
		s.kill(t, killer, weapon)
	}
	return amount
}

func (s *Sim) hurtNPC(n *NPC, amount float64, weapon string, by int) {
	if !n.Alive {
		return
	}
	n.HP -= amount
	if by >= 0 && by < len(s.Mechs) {
		a := s.Mechs[by]
		a.lastCombatT = s.T
		n.Target = by
		s.event(a, map[string]any{"type": "hit", "target": n.ID, "amount": r2(amount), "weapon": weapon})
	}
	if n.HP <= 0 {
		n.Alive = false
		for i := 0; i < s.C.Npc.Cubes; i++ {
			s.addCube(n.Pos)
		}
		s.sound("death", n.Pos, -1)
	}
}

func (s *Sim) hurtCrate(c *Crate, amount float64, weapon string, by int) {
	if !c.Alive {
		return
	}
	c.HP -= amount
	if by >= 0 && by < len(s.Mechs) {
		s.event(s.Mechs[by], map[string]any{"type": "hit", "target": c.ID, "amount": r2(amount), "weapon": weapon})
	}
	if c.HP <= 0 {
		c.Alive = false
		for i := 0; i < s.C.Crates.Cubes; i++ {
			s.addCube(c.Pos)
		}
		if s.Rng.Float64() < s.C.Loot.CrateWeaponChance {
			s.addPickup(s.newWeapon(s.weightedKind(), nil), c.Pos)
		}
		s.sound("crate", c.Pos, -1)
	}
}

// shotOrigin returns muzzle point and the origin 3 m out (clipped by walls), plus whether the barrel is in a wall.
func (s *Sim) shotOrigin(m *Mech, dir Vec3, height float64) (Vec3, Vec3, bool) {
	mz := m.Pos.Add(V(0, 0, height))
	out := m.radius() + 0.5
	end := mz.Add(dir.Scale(out))
	if d := s.W.RayHit(mz, end); d >= 0 {
		return mz, mz.Add(dir.Scale(math.Max(0, d-0.05))), true
	}
	return mz, end, false
}

type target struct {
	t    float64
	mech *Mech
	npc  *NPC
	crt  *Crate
}

// rayTargets finds targets intersected by segment a->b (excluding owner), sorted by t.
func (s *Sim) rayTargets(a, b Vec3, owner int, prad float64, includeNPC bool) []target {
	var out []target
	for _, m := range s.Mechs {
		if !m.Alive || m.ID == owner || s.sameSquad(owner, m) {
			continue
		}
		if t := segCyl(a, b, m.Pos.X, m.Pos.Y, m.radius()+prad, m.Pos.Z, m.Pos.Z+m.height()); t >= 0 {
			out = append(out, target{t: t, mech: m})
		}
	}
	if includeNPC {
		for _, n := range s.NPCs {
			if !n.Alive || n.ID == owner {
				continue
			}
			if t := segCyl(a, b, n.Pos.X, n.Pos.Y, s.C.Npc.Radius+prad, n.Pos.Z, n.Pos.Z+s.C.Npc.Height); t >= 0 {
				out = append(out, target{t: t, npc: n})
			}
		}
		for _, c := range s.Crates {
			if !c.Alive {
				continue
			}
			if t := segCyl(a, b, c.Pos.X, c.Pos.Y, s.C.Crates.Radius+prad, c.Pos.Z, c.Pos.Z+s.C.Crates.Height); t >= 0 {
				out = append(out, target{t: t, crt: c})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].t < out[j].t })
	return out
}

func (s *Sim) weaponsTick(m *Mech, dt float64) {
	for i, w := range m.Weapons {
		if w == nil {
			continue
		}
		fire := m.cmd.Fire[i]
		ws := m.WS[w.Kind]
		switch w.Kind {
		case "gun":
			mag := int(ws["mag"])
			if w.Reload > 0 {
				w.Reload -= dt
				if w.Reload <= 1e-9 {
					w.Reload = 0
					w.Ammo = mag
				}
			}
			if m.cmd.Reload && w.Reload == 0 && w.Ammo < mag {
				w.Reload = ws["reload"]
				s.sound("reload", m.Pos, m.ID)
			}
			w.Cooldown -= dt
			if fire && w.Reload == 0 {
				for w.Cooldown <= 1e-9 && w.Ammo > 0 {
					s.fireGun(m, ws)
					w.Ammo--
					w.Cooldown += ws["cooldown"]
				}
				if w.Ammo <= 0 && w.Reload == 0 {
					w.Reload = ws["reload"]
					s.sound("reload", m.Pos, m.ID)
				}
			}
			if w.Cooldown < 0 {
				w.Cooldown = 0
			}
		case "lightning":
			w.Cooldown -= dt
			if fire && w.Cooldown <= 1e-9 && m.Energy >= ws["energy"] {
				s.fireLightning(m, ws)
				m.Energy -= ws["energy"]
				m.lastSpendT = s.T
				w.Cooldown += ws["cooldown"]
			}
			if w.Cooldown < 0 {
				w.Cooldown = 0
			}
		case "mortar":
			w.Cooldown -= dt
			if fire && w.Cooldown <= 1e-9 {
				s.fireMortar(m, ws)
				w.Cooldown = ws["cooldown"]
			}
			if w.Cooldown < 0 {
				w.Cooldown = 0
			}
		case "laser":
			w.Cooldown = math.Max(0, w.Cooldown-dt)
			if w.Charging {
				w.Charge = math.Min(w.Charge+dt, ws["charge"])
				if w.Charge >= ws["charge"]-1e-9 {
					w.FullHeld += dt
				}
				if !fire {
					w.Release = true
				}
				if s.T-m.chargeSndT >= 0.5 {
					m.chargeSndT = s.T
					s.sound("laser_charge", m.Pos, m.ID)
				}
				if (w.Release && w.Charge >= ws["min_charge"]-1e-9) || w.FullHeld >= ws["hold_s"] {
					s.fireLaser(m, ws, w.Charge)
					w.Charging, w.Charge, w.FullHeld, w.Release = false, 0, 0, false
					w.Cooldown = ws["cooldown"]
				}
			} else if fire && w.Cooldown <= 0 && s.T >= m.shortUntil {
				w.Charging = true
				w.Charge = 0
				w.Release = false
				w.FullHeld = 0
				m.chargeSndT = s.T
				s.sound("laser_charge", m.Pos, m.ID)
			}
		}
	}
}

func (s *Sim) fireGun(m *Mech, ws map[string]float64) {
	sp := ws["spread_deg"] * math.Pi / 180
	dir := dirFromAim(m.Yaw+s.gauss()*sp, m.Pitch+s.gauss()*sp)
	mz, org, _ := s.shotOrigin(m, dir, m.MS["muzzle_height"]*m.scale())
	s.projID++
	s.Projs = append(s.Projs, &Projectile{ID: s.projID, Kind: "gun", Pos: org, Vel: dir.Scale(ws["speed"]), Owner: m.ID, Range: ws["range"], Damage: ws["damage"] * s.dmgMult(m), Born: s.T, Alive: true})
	s.sound("gun", mz, m.ID)
}

func (s *Sim) fireMortar(m *Mech, ws map[string]float64) {
	dir := dirFromAim(m.Yaw, m.Pitch)
	mz, org, _ := s.shotOrigin(m, dir, m.height()+0.5)
	s.projID++
	s.Projs = append(s.Projs, &Projectile{ID: s.projID, Kind: "mortar", Pos: org, Vel: dir.Scale(ws["speed"]), Owner: m.ID, Range: 1e9, Damage: ws["damage"], Born: s.T, Alive: true, ws: ws, dm: s.dmgMult(m)})
	s.sound("mortar", mz, m.ID)
}

func (s *Sim) fireLaser(m *Mech, ws map[string]float64, charge float64) {
	dir := dirFromAim(m.Yaw, m.Pitch)
	mz, org, inWall := s.shotOrigin(m, dir, m.MS["muzzle_height"]*m.scale())
	frac := ws["min_frac"] + (1-ws["min_frac"])*math.Min(1, charge/ws["charge"])
	dmg := ws["damage"] * frac * s.dmgMult(m)
	end := org.Add(dir.Scale(ws["range"]))
	if inWall {
		end = org
	} else if d := s.W.RayHit(org, end); d >= 0 {
		end = org.Add(dir.Scale(d))
	}
	tg := s.rayTargets(org, end, m.ID, 0, true)
	n := 1 + int(ws["pierce"])
	for k := 0; k < len(tg) && k < n; k++ {
		x := tg[k]
		if x.mech != nil {
			d := dmg
			eff := ""
			if x.mech.holds("mortar") >= 0 {
				d *= 1 + ws["cookoff"]
				eff = "cookoff"
				s.event(x.mech, map[string]any{"type": "effect", "effect": "cookoff", "by": m.ID})
			}
			s.hurtMech(x.mech, d, "laser", m.ID, mz, eff)
		} else if x.npc != nil {
			s.hurtNPC(x.npc, dmg, "laser", m.ID)
		} else if x.crt != nil {
			s.hurtCrate(x.crt, dmg, "laser", m.ID)
		}
		if k == n-1 {
			end = org.Add(end.Sub(org).Scale(x.t))
		}
	}
	s.Beams = append(s.Beams, BeamEv{By: m.ID, Kind: "laser", Path: []Vec3{mz, end}, T: s.T})
	s.sound("laser", mz, m.ID)
}

type ltTarget struct {
	pos  Vec3
	mech *Mech
	npc  *NPC
	crt  *Crate
	id   int
}

func (s *Sim) fireLightning(m *Mech, ws map[string]float64) {
	mz := m.Pos.Add(V(0, 0, m.MS["muzzle_height"]*m.scale()))
	rng := ws["range"]
	cone := ws["cone_deg"] * math.Pi / 180
	var cands []ltTarget
	for _, o := range s.Mechs {
		if o.Alive && o.ID != m.ID && !s.sameSquad(m.ID, o) {
			cands = append(cands, ltTarget{pos: o.center(), mech: o, id: o.ID})
		}
	}
	for _, n := range s.NPCs {
		if n.Alive {
			cands = append(cands, ltTarget{pos: n.Pos.Add(V(0, 0, s.C.Npc.Height/2)), npc: n, id: n.ID})
		}
	}
	for _, c := range s.Crates {
		if c.Alive {
			cands = append(cands, ltTarget{pos: c.Pos.Add(V(0, 0, s.C.Crates.Height/2)), crt: c, id: c.ID})
		}
	}
	best := -1
	bd := 1e9
	for i, c := range cands {
		d := c.pos.Dist(mz)
		if d > rng || d >= bd {
			continue
		}
		b := math.Atan2(c.pos.Y-mz.Y, c.pos.X-mz.X)
		if math.Abs(wrapAngle(b-m.Yaw)) > cone {
			continue
		}
		if !s.W.LOS(mz, c.pos) {
			continue
		}
		best, bd = i, d
	}
	path := []Vec3{mz}
	if best < 0 {
		dir := dirFromAim(m.Yaw, m.Pitch)
		end := mz.Add(dir.Scale(rng))
		if d := s.W.RayHit(mz, end); d >= 0 {
			end = mz.Add(dir.Scale(d))
		}
		path = append(path, end)
	} else {
		hit := map[int]bool{}
		dmg := ws["damage"] * s.dmgMult(m)
		cur := best
		src := mz
		for jump := 0; jump <= int(ws["chain"]); jump++ {
			c := cands[cur]
			hit[c.id] = true
			path = append(path, c.pos)
			if c.mech != nil {
				eff := ""
				if li := c.mech.holds("laser"); li >= 0 {
					eff = "short"
					w := c.mech.Weapons[li]
					w.Charging, w.Charge, w.FullHeld, w.Release = false, 0, 0, false
					c.mech.shortUntil = s.T + ws["short_s"]
					s.event(c.mech, map[string]any{"type": "effect", "effect": "short", "by": m.ID})
				}
				s.hurtMech(c.mech, dmg, "lightning", m.ID, src, eff)
			} else if c.npc != nil {
				s.hurtNPC(c.npc, dmg, "lightning", m.ID)
			} else if c.crt != nil {
				s.hurtCrate(c.crt, dmg, "lightning", m.ID)
			}
			dmg *= ws["chain_falloff"]
			src = c.pos
			nxt := -1
			nd := 1e9
			for i, o := range cands {
				if hit[o.id] {
					continue
				}
				d := o.pos.Dist(c.pos)
				if d <= ws["chain_range"] && d < nd && s.W.LOS(c.pos, o.pos) {
					nxt, nd = i, d
				}
			}
			if nxt < 0 {
				break
			}
			cur = nxt
		}
	}
	s.Beams = append(s.Beams, BeamEv{By: m.ID, Kind: "lightning", Path: path, T: s.T})
	s.sound("lightning", mz, m.ID)
}

func (s *Sim) explode(p *Projectile, at Vec3, direct *target) {
	ws := p.ws
	owner := p.Owner
	var om *Mech
	if owner >= 0 && owner < len(s.Mechs) {
		om = s.Mechs[owner]
	}
	if direct != nil {
		d := ws["damage"] * p.dm
		if direct.mech != nil {
			s.hurtMech(direct.mech, d, "mortar", owner, at, "")
		} else if direct.npc != nil {
			s.hurtNPC(direct.npc, d, "mortar", owner)
		} else if direct.crt != nil {
			s.hurtCrate(direct.crt, d, "mortar", owner)
		}
	}
	sr := ws["splash_radius"]
	edge := ws["splash_edge"]
	fall := func(d float64) float64 { return 1 - (1-edge)*d/sr }
	for _, m := range s.Mechs {
		if !m.Alive {
			continue
		}
		hd := math.Max(0, m.Pos.HDist(at)-m.radius())
		vz := 0.0
		if at.Z < m.Pos.Z {
			vz = m.Pos.Z - at.Z
		} else if at.Z > m.Pos.Z+m.height() {
			vz = at.Z - m.Pos.Z - m.height()
		}
		d := math.Hypot(hd, vz)
		if d > sr {
			continue
		}
		if !s.W.LOS(at, m.center()) && !s.W.LOS(at, m.top()) {
			continue
		}
		if m.ID != owner && s.sameSquad(owner, m) {
			continue
		}
		f := fall(d)
		dmg := ws["splash"] * p.dm * f
		if m.ID == owner {
			dmg *= m.MS["self_damage_mult"]
		}
		// concussion
		away := m.center().Sub(at)
		away.Z = 0
		away = away.Norm()
		m.Vel = m.Vel.Add(away.Scale(ws["knock"] * f))
		m.Vel.Z += ws["knock_up"] * f
		m.Grounded = false
		m.Energy = math.Max(0, m.Energy-ws["drain"]*f)
		if m.ID != owner {
			s.event(m, map[string]any{"type": "effect", "effect": "concuss", "by": owner})
		}
		eff := ""
		if m.ID != owner {
			eff = "concuss"
		}
		s.hurtMech(m, dmg, "mortar", owner, at, eff)
	}
	for _, n := range s.NPCs {
		if n.Alive {
			d := math.Max(0, n.Pos.Add(V(0, 0, 1.5)).Dist(at)-s.C.Npc.Radius)
			if d <= sr && s.W.LOS(at, n.Pos.Add(V(0, 0, 1.5))) {
				s.hurtNPC(n, ws["splash"]*p.dm*fall(d), "mortar", owner)
			}
		}
	}
	for _, c := range s.Crates {
		if c.Alive {
			d := math.Max(0, c.Pos.Add(V(0, 0, 1.2)).Dist(at)-s.C.Crates.Radius)
			if d <= sr && s.W.LOS(at, c.Pos.Add(V(0, 0, 1.2))) {
				s.hurtCrate(c, ws["splash"]*p.dm*fall(d), "mortar", owner)
			}
		}
	}
	_ = om
	s.sound("explosion", at, -1)
}

func (s *Sim) projectilesTick(dt float64) {
	for _, p := range s.Projs {
		if !p.Alive {
			continue
		}
		a := p.Pos
		if p.Kind == "mortar" {
			p.Vel.Z -= p.ws["gravity"] * dt
			if s.T-p.Born > p.ws["ttl"] {
				p.Alive = false
				s.explode(p, a, nil)
				continue
			}
		}
		b := a.Add(p.Vel.Scale(dt))
		segLen := b.Sub(a).Len()
		endT := 1.0
		expire := false
		if p.Kind != "mortar" && p.Dist+segLen > p.Range {
			endT = (p.Range - p.Dist) / segLen
			expire = true
		}
		wallT := -1.0
		if d := s.W.RayHit(a, b); d >= 0 {
			wallT = d / segLen
		}
		prad := s.C.Projectile.GunRadius
		if p.Kind == "npc" {
			prad = s.C.Projectile.NpcRadius
		}
		if p.Kind == "mortar" {
			prad = 0.2
		}
		tg := s.rayTargets(a, b, p.Owner, prad, p.Kind != "npc")
		var hitT = -1.0
		var ht *target
		if len(tg) > 0 && tg[0].t <= endT && (wallT < 0 || tg[0].t <= wallT) {
			hitT = tg[0].t
			ht = &tg[0]
		}
		if ht != nil {
			at := a.Add(b.Sub(a).Scale(hitT))
			p.Alive = false
			if p.Kind == "mortar" {
				s.explode(p, at, ht)
				continue
			}
			wk := p.Kind
			src := a.Sub(p.Vel.Norm().Scale(50))
			if ht.mech != nil {
				s.hurtMech(ht.mech, p.Damage, wk, p.Owner, src, "")
			} else if ht.npc != nil {
				s.hurtNPC(ht.npc, p.Damage, wk, p.Owner)
			} else if ht.crt != nil {
				s.hurtCrate(ht.crt, p.Damage, wk, p.Owner)
			}
			continue
		}
		if wallT >= 0 && wallT <= endT {
			p.Alive = false
			if p.Kind == "mortar" {
				at := a.Add(b.Sub(a).Scale(math.Max(0, wallT-0.02)))
				s.explode(p, at, nil)
			}
			continue
		}
		if expire || !s.W.InBounds(b.X, b.Y, -50) || b.Z < -200 {
			p.Alive = false
			continue
		}
		p.Pos = b
		p.Dist += segLen
	}
	if s.Tick%20 == 0 {
		ps := s.Projs[:0]
		for _, p := range s.Projs {
			if p.Alive {
				ps = append(ps, p)
			}
		}
		s.Projs = ps
	}
}

// ---------- NPCs ----------

func (s *Sim) npcTick(dt float64) {
	nc := s.C.Npc
	for _, n := range s.NPCs {
		if !n.Alive {
			continue
		}
		n.Cooldown -= dt
		eye := n.Pos.Add(V(0, 0, nc.Height*0.8))
		if n.Target >= 0 {
			tm := s.Mechs[n.Target]
			if !tm.Alive || tm.Pos.Dist(n.Pos) > nc.DeaggroRange || n.Pos.HDist(n.Home) > nc.ChaseLeash {
				n.Target = -1
			}
		}
		n.scanT -= dt
		if n.Target < 0 && n.scanT <= 0 {
			n.scanT = 0.5
			if n.Pos.HDist(n.Home) <= nc.Leash {
				bd := nc.AggroRange
				for _, m := range s.Mechs {
					if !m.Alive {
						continue
					}
					d := m.Pos.Dist(n.Pos)
					if d < bd && s.W.LOS(eye, m.center()) {
						bd = d
						n.Target = m.ID
					}
				}
			}
		}
		var goal Vec3
		moving := true
		if n.Target >= 0 {
			tm := s.Mechs[n.Target]
			goal = tm.Pos
			if tm.Pos.HDist(n.Pos) < 20 {
				moving = false
			}
			n.Yaw = math.Atan2(tm.Pos.Y-n.Pos.Y, tm.Pos.X-n.Pos.X)
			if n.Cooldown <= 0 {
				tc := tm.center()
				if s.W.LOS(eye, tc) {
					n.Cooldown = nc.Cooldown
					d := tc.Sub(eye)
					yaw := math.Atan2(d.Y, d.X)
					pit := math.Atan2(d.Z, d.HLen())
					sp := nc.SpreadDeg * math.Pi / 180
					dir := dirFromAim(yaw+s.gauss()*sp, pit+s.gauss()*sp)
					s.projID++
					s.Projs = append(s.Projs, &Projectile{ID: s.projID, Kind: "npc", Pos: eye.Add(dir.Scale(nc.Radius + 0.3)), Vel: dir.Scale(nc.ProjectileSpeed), Owner: n.ID, Range: nc.DeaggroRange * 1.5, Damage: nc.Damage, Born: s.T, Alive: true})
					s.sound("npc_gun", eye, -1)
				}
			}
		} else {
			if n.Pos.HDist(n.Home) > nc.Leash {
				goal = n.Home
			} else {
				n.WanderT -= dt
				if n.WanderT <= 0 || n.Pos.HDist(n.Wander) < 1 {
					a := s.Rng.Float64() * 2 * math.Pi
					r := nc.Leash * math.Sqrt(s.Rng.Float64())
					n.Wander = n.Home.Add(V(r*math.Cos(a), r*math.Sin(a), 0))
					n.WanderT = 3 + s.Rng.Float64()*5
				}
				goal = n.Wander
			}
		}
		n.Vel = Vec3{}
		if moving {
			d := goal.Sub(n.Pos)
			d.Z = 0
			l := d.HLen()
			if l > 0.5 {
				st := math.Min(l, nc.Speed*dt)
				nx, ny := n.Pos.X+d.X/l*st, n.Pos.Y+d.Y/l*st
				g := s.W.Ground(nx, ny)
				if math.Abs(g-n.Pos.Z) < 1.0 && s.W.columnClear(nx, ny, nc.Radius, g+0.3, g+nc.Height) {
					n.Pos = V(nx, ny, g)
					n.Vel = V(d.X/l*nc.Speed, d.Y/l*nc.Speed, 0)
					if n.Target < 0 {
						n.Yaw = math.Atan2(d.Y, d.X)
					}
				} else {
					n.WanderT = 0
				}
			}
		}
	}
}
