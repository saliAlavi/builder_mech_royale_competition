package main

import (
	"math"
	"sort"
)

// ---------------------------------------------------------------- movement decision

func (b *Bot) walkSpeed() float64 { return b.mechf("walk_speed", 11.7) }

func (b *Bot) quietMag() float64 {
	ss := 5.0
	return math.Min(1, (ss-0.4)/b.walkSpeed())
}

func (b *Bot) hpFrac() float64 { return b.me.HP / math.Max(1, b.me.MaxHP) }

func (b *Bot) decideMove(act *Action, tgt *Track) {
	o := b.o
	z := o.Zone
	nc, nr := z.NextCenter, z.NextRadius
	distNext := math.Hypot(b.pos[0]-nc[0], b.pos[1]-nc[1])
	margin := clampf(0.25*nr, 4, 40)
	need := math.Max(0, distNext-(nr-margin))
	tLeft := math.Max(0.5, z.ShrinkEnd-b.t)
	reqSpeed := need * 1.3 / tLeft
	// circle right now / soon
	cNow, rNow := b.zoneAt(b.t + 3)
	outNow := math.Hypot(b.pos[0]-cNow[0], b.pos[1]-cNow[1]) > rNow-2 || !b.me.InZone

	// threats
	nThreat := 0
	var cx, cy float64
	for _, t := range b.enemies {
		if t.InObs && hdist(t.Pos, b.pos) < 170 {
			nThreat++
			cx += t.Pos[0]
			cy += t.Pos[1]
		}
	}
	if nThreat > 0 {
		cx /= float64(nThreat)
		cy /= float64(nThreat)
	}
	// evade: outnumbered by recently-known enemies (squads converge) -> break contact
	nKnown := 0
	var kx, ky float64
	for _, t := range b.enemies {
		if b.t-t.LastSeen < 4 && hdist(t.Pos, b.pos) < 230 {
			nKnown++
			kx += t.Pos[0]
			ky += t.Pos[1]
		}
	}
	evN := P["evade_n"]
	if b.me.Weapons[0] == nil || b.me.Weapons[1] == nil {
		evN = math.Min(evN, P["evade_n_1w"])
	}
	evade := float64(nKnown) >= evN
	if evade && nThreat == 0 {
		cx, cy = kx/float64(nKnown), ky/float64(nKnown)
		nThreat = nKnown
	}
	hp := b.hpFrac()
	if b.retreat {
		if hp > P["retreat_exit"] || (nThreat == 0 && b.t-b.lastHurtT > 8 && hp > 0.6) {
			b.retreat = false
		}
	} else if nThreat > 0 && (hp < P["retreat_hp"] || (nThreat >= 3 && hp < P["retreat_hp3"]) || (nThreat >= 2 && hp < P["retreat_hp2"])) {
		b.retreat = true
		b.coverOK = false
	}

	speed := 1.0
	var dir Vec2
	b.mode = "goal"
	b.jetting = false

	// 1) mortar dodge (highest priority)
	if dd, ok := b.mortarDodge(); ok {
		b.mode = "dodge"
		act.Move = dd
		if b.me.Energy > 20 && b.me.Grounded {
			act.Boost = true
		}
		return
	}

	switch {
	case outNow || reqSpeed > 0.85*b.walkSpeed():
		b.mode = "zone"
		g := b.zoneGoal()
		dir = b.navTo(g, act)
		if outNow || reqSpeed > b.walkSpeed() {
			if b.me.Energy > 15 {
				act.Boost = true
			}
		}
	case (b.retreat && nThreat > 0) || evade:
		b.mode = "retreat"
		if !b.coverOK || (b.t-b.coverT > 1.5 && !b.busy()) {
			b.cover, b.coverOK = b.findCover(cx, cy)
			b.coverT = b.t
		}
		if b.coverOK {
			dir = b.navTo(b.cover, act)
		} else {
			dir = norm2(b.pos[0]-cx, b.pos[1]-cy)
			dir = b.steerClear(dir)
		}
		// weave: unpredictable lateral component so pursuers can't lead us
		if zz := P["retreat_zz"]; zz > 0 && dir != (Vec2{}) && !b.jetting {
			if b.t > b.strafeUntil {
				b.strafe = -b.strafe
				b.strafeUntil = b.t + 0.35 + b.rng.Float64()*0.4
			}
			w := norm2(dir[0]-dir[1]*zz*b.strafe, dir[1]+dir[0]*zz*b.strafe)
			dir = b.steerClear(w)
		}
		if b.me.Energy > 30 && hdist(b.pos, b.cover) > 6 {
			act.Boost = true
		}
	case tgt != nil && b.engage(tgt) && (tgt.LOS || b.aggressive(tgt, nThreat)):
		b.mode = "fight"
		dir = b.combatMove(tgt, nThreat)
	case tgt != nil:
		// known but not shootable / not worth a fight: keep to cover goal, facing it
		b.mode = "wary"
		dir = b.navTo(b.strategicGoal(), act)
	case P["regen_hold"] > 0 && hp < P["regen_hold_hp"] && b.t-b.lastHurtT > 2 && reqSpeed < 0.3*b.walkSpeed() && !b.enemySeenWithin(4):
		b.mode = "regen"
		if !b.hideOK || b.t-b.hideT > 3 {
			b.hide, b.hideOK = b.hideSpot()
			b.hideT = b.t
		}
		if b.hideOK {
			dir = b.navTo(b.hide, act)
			if hdist(b.hide, b.pos) < 4 {
				speed = 0
			}
		}
	default:
		if g, ok := b.lootGoal(); ok {
			b.mode = "loot"
			dir = b.navTo(g, act)
		} else {
			g := b.strategicGoal()
			dir = b.navTo(g, act)
			if P["quiet"] > 0 && !b.recentDanger() && reqSpeed < 0.35*b.walkSpeed() && hdist(g, b.pos) < 60 {
				speed = b.quietMag()
			}
		}
	}
	// laser charging halves speed anyway; don't bother
	act.Move = Vec2{dir[0] * speed, dir[1] * speed}
	if b.mode == "fight" || b.mode == "wary" || b.mode == "retreat" || b.mode == "goal" || b.mode == "loot" {
		if dg := b.bulletDodge(); math.Hypot(dg[0], dg[1]) > 0.3 {
			d := norm2(dg[0], dg[1])
			nd := b.steerClear(norm2(act.Move[0]+2*d[0], act.Move[1]+2*d[1]))
			act.Move = nd
		}
	}
	if b.mode != "fight" {
		b.antiStuck(act)
	} else {
		b.hist = b.hist[:0]
		b.histMv = b.histMv[:0]
	}
	b.laserJuke(act)
	// boost in the air is a jet (flies up): only when deliberately climbing / unsticking
	if act.Boost && !b.me.Grounded && !b.jetting && b.t >= b.unstuckT {
		act.Boost = false
	}
}

func (b *Bot) enemySeenWithin(s float64) bool {
	for _, t := range b.enemies {
		if b.t-t.LastSeen < s {
			return true
		}
	}
	return false
}

// hideSpot: the most sheltered reachable spot within ~25 m (for regenerating).
func (b *Bot) hideSpot() (Vec3, bool) {
	bestS := math.Inf(-1)
	var best Vec3
	ok := false
	for _, r := range []float64{0, 8, 16, 24} {
		n := 10
		if r == 0 {
			n = 1
		}
		for k := 0; k < n; k++ {
			a := float64(k) / float64(n) * 2 * math.Pi
			id := b.nav.nearestOK(b.pos[0]+math.Cos(a)*r, b.pos[1]+math.Sin(a)*r, b.pos[2], 3, true)
			if id < 0 {
				continue
			}
			p := b.nav.pos(id)
			if !b.insideAt(p, b.t+20, 8) {
				continue
			}
			s := float64(b.nav.coverScore(id)) - r/12
			if s > bestS {
				bestS, best, ok = s, p, true
			}
		}
	}
	return best, ok
}

func (b *Bot) recentDanger() bool {
	if b.t-b.lastHurtT < 6 {
		return true
	}
	for _, t := range b.enemies {
		if b.t-t.LastSeen < 6 && hdist(t.Pos, b.pos) < 200 {
			return true
		}
	}
	return false
}

// zoneGoal: nearest good point inside the next circle (or current if final).
func (b *Bot) zoneGoal() Vec3 {
	z := b.o.Zone
	c, r := z.NextCenter, z.NextRadius
	m := clampf(0.3*r, 3, 40)
	dx, dy := b.pos[0]-c[0], b.pos[1]-c[1]
	d := math.Hypot(dx, dy)
	if r-m <= 0 || d < 1e-6 {
		return Vec3{c[0], c[1], 0}
	}
	if d <= r-m {
		// already inside the next circle: head for the strategic goal
		return b.strategicGoal()
	}
	k := (r - m) / d
	return Vec3{c[0] + dx*k, c[1] + dy*k, 0}
}

// strategicGoal: a covered spot inside the next circle, away from enemies; re-chosen periodically.
func (b *Bot) strategicGoal() Vec3 {
	z := b.o.Zone
	c, r := z.NextCenter, z.NextRadius
	m := clampf(0.3*r, 3, 45)
	R := r - m
	if R < 2 {
		R = math.Max(r*0.5, 0)
	}
	valid := func(p Vec3) bool { return math.Hypot(p[0]-c[0], p[1]-c[1]) <= R+1 }
	if b.goalOK && valid(b.goal) && (b.t-b.goalT < 6 || b.busy()) && b.goalNext == c {
		return b.goal
	}
	score := func(id int) float64 {
		p := b.nav.pos(id)
		s := -P["w_center"] * math.Hypot(p[0]-c[0], p[1]-c[1]) / math.Max(r, 30)
		s -= P["w_travel"] * hdist(p, b.pos) / 250
		s += P["w_cover"] * float64(b.nav.coverScore(id)) / 16
		for _, t := range b.enemies {
			if b.t-t.LastSeen < 30 {
				dd := hdist(p, t.Pos)
				s -= P["w_enemy"] * math.Max(0, 1-dd/140)
			}
		}
		return s
	}
	bestID := -1
	bestS := math.Inf(-1)
	if b.goalOK && valid(b.goal) && b.goalNext == c {
		if id := b.nav.nearestOK(b.goal[0], b.goal[1], b.goal[2], 4, true); id >= 0 {
			bestID, bestS = id, score(id)+0.25
		}
	}
	for k := 0; k < 70; k++ {
		a := b.rng.Float64() * 2 * math.Pi
		rr := math.Sqrt(b.rng.Float64()) * R
		x, y := c[0]+math.Cos(a)*rr, c[1]+math.Sin(a)*rr
		id := b.nav.nearestOK(x, y, 0, 6, true)
		if id < 0 && k >= 40 && bestID < 0 {
			id = b.nav.nearestOK(x, y, 0, 6, false)
		}
		if id < 0 {
			continue
		}
		p := b.nav.pos(id)
		if !valid(p) {
			continue
		}
		if s := score(id); s > bestS {
			bestS, bestID = s, id
		}
	}
	b.goalT = b.t
	b.goalNext = c
	if bestID < 0 {
		b.goalOK = false
		return Vec3{c[0], c[1], 0}
	}
	b.goal = b.nav.pos(bestID)
	b.goalOK = true
	return b.goal
}

// lootGoal: worthwhile weapon / cube / supply drop nearby.
func (b *Bot) lootGoal() (Vec3, bool) {
	bestV := 0.0
	var best Vec3
	maxD := P["loot_m"]
	if b.t < 75 {
		maxD = P["loot_early_m"]
	} else if b.me.Weapons[0] == nil || b.me.Weapons[1] == nil {
		maxD = math.Max(maxD, 120) // still missing a second weapon
	}
	for _, p := range b.pickups {
		g, _ := b.lootGain(p.Kind)
		if g <= 0 {
			continue
		}
		if ft, ok := b.failTake[p.ID]; ok && b.t-ft < 3 {
			continue
		}
		d := hdist(p.Pos, b.pos)
		lim := maxD
		if p.Kind == "laser" && P["laser_seek_m"] > lim {
			if l, _ := b.weapon("laser"); l == nil {
				lim = P["laser_seek_m"]
			}
		}
		if d > lim || math.Abs(p.Pos[2]-b.pos[2]) > 25 {
			continue
		}
		if !b.insideAt(p.Pos, b.t+d/10+5, 5) {
			continue
		}
		v := g / (1 + d/40)
		if v > bestV {
			bestV, best = v, p.Pos
		}
	}
	cr := 60.0
	for _, c := range b.cubes {
		d := hdist(c.Pos, b.pos)
		if d > cr || math.Abs(c.Pos[2]-b.pos[2]) > 20 {
			continue
		}
		if !b.insideAt(c.Pos, b.t+d/10+5, 5) {
			continue
		}
		v := 6 / (1 + d/40)
		if v > bestV {
			bestV, best = v, c.Pos
		}
	}
	// supply drops: worth a lot if uncontested
	for _, dr := range b.o.Drops {
		if P["drops"] == 0 {
			break
		}
		d := hdist(dr.Pos, b.pos)
		if d > 160 || dr.LandsIn > d/9+3 {
			continue
		}
		if dr.Holder != nil && int(*dr.Holder) != b.me.ID {
			continue
		}
		contested := false
		for _, t := range b.enemies {
			if b.t-t.LastSeen < 10 && hdist(t.Pos, dr.Pos) < 90 {
				contested = true
			}
		}
		if contested || !b.insideAt(dr.Pos, b.t+d/10+8, 3) {
			continue
		}
		v := 9 / (1 + d/60)
		if b.hpFrac() < 0.6 {
			v *= 1.5
		}
		if v > bestV {
			bestV, best = v, dr.Pos
		}
	}
	return best, bestV > 0
}

// engage: worth starting/continuing a fight with tgt?
func (b *Bot) engage(tgt *Track) bool {
	d := hdist(tgt.Pos, b.pos)
	lim := P["engage_m"]
	if b.me.Weapons[0] == nil || b.me.Weapons[1] == nil {
		lim = math.Min(lim, P["engage_m_1w"])
	}
	return d <= lim || b.t-tgt.HitUsT < 6 || b.t-b.lastHurtT < 3 || b.t-b.lastHitT < 3 ||
		tgt.HP < 0.35*math.Max(tgt.MaxHP, 1)
}

func (b *Bot) aggressive(tgt *Track, nThreat int) bool {
	return b.hpFrac() > P["aggr_hp"] && nThreat <= 2 && tgt.HP <= b.me.HP*1.3
}

// combatMove: hold a preferred range and strafe unpredictably.
func (b *Bot) combatMove(tgt *Track, nThreat int) Vec2 {
	d := hdist(tgt.Pos, b.pos)
	lit, _ := b.weapon("lightning")
	las, _ := b.weapon("laser")
	pref := 70.0
	aggressive := b.aggressive(tgt, nThreat)
	pref = P["pref_gun"]
	if lit != nil && aggressive && b.me.Energy > 25 {
		pref = P["pref_lightning"]
	} else if las != nil {
		pref = P["pref_laser"]
	} else if lit != nil {
		pref = 55
	}
	if !tgt.LOS {
		// can't shoot it: if aggressive, approach via path; else hold
		if aggressive {
			return b.navTo(tgt.Pos, nil)
		}
		pref = d
	}
	if P["fight_pos"] > 0 {
		if dir, ok := b.cornerSpot(tgt); ok {
			return dir
		}
	}
	if b.t > b.strafeUntil {
		if b.rng.Float64() < 0.6 {
			b.strafe = -b.strafe
		}
		b.strafeUntil = b.t + 0.5 + b.rng.Float64()*1.0
	}
	ux, uy := (tgt.Pos[0]-b.pos[0])/math.Max(d, 1e-6), (tgt.Pos[1]-b.pos[1])/math.Max(d, 1e-6)
	radial := clampf((d-pref)/20, -1, 1)
	mx := ux*radial*0.8 - uy*b.strafe
	my := uy*radial*0.8 + ux*b.strafe
	// keep inside zone
	c, r := b.zoneAt(b.t + 4)
	zd := math.Hypot(b.pos[0]-c[0], b.pos[1]-c[1])
	if zd > r-15 && zd > 1e-6 {
		k := clampf((zd-(r-15))/10, 0, 2)
		mx += (c[0] - b.pos[0]) / zd * k
		my += (c[1] - b.pos[1]) / zd * k
	}
	dir := norm2(mx, my)
	nd := b.steerClear(dir)
	if nd != dir && (nd[0]*dir[0]+nd[1]*dir[1]) < 0.3 {
		b.strafe = -b.strafe
	}
	return nd
}

// cornerSpot: with 2+ enemies able to see us, find a nearby spot that keeps a shot on tgt but
// hides us from the others (fight one at a time). Re-evaluated every 0.8 s.
func (b *Bot) cornerSpot(tgt *Track) (Vec2, bool) {
	sc := b.scaleOf(b.me.Cubes)
	var others []Vec3
	for _, t := range b.enemies {
		if !t.InObs || t.ID == tgt.ID || hdist(t.Pos, b.pos) > 220 {
			continue
		}
		others = append(others, Vec3{t.Pos[0], t.Pos[1], t.Pos[2] + 8*b.scaleOf(t.Cubes)})
	}
	if len(others) > 6 {
		sort.Slice(others, func(i, j int) bool { return hdist(others[i], b.pos) < hdist(others[j], b.pos) })
		others = others[:6]
	}
	if len(others) == 0 {
		b.cornerOK = false
		return Vec2{}, false
	}
	seenBy := func(p Vec3) int {
		n := 0
		for _, e := range others {
			if b.w.SightLOS(e, Vec3{p[0], p[1], p[2] + 4.5*sc}) || b.w.SightLOS(e, Vec3{p[0], p[1], p[2] + 9*sc}) {
				n++
			}
		}
		return n
	}
	tc := Vec3{tgt.Pos[0], tgt.Pos[1], tgt.Pos[2] + 4.5*b.scaleOf(tgt.Cubes)}
	if b.t-b.cornerT > 0.8 && !b.busy() {
		b.cornerT = b.t
		cur := seenBy(b.pos)
		if cur == 0 {
			b.cornerOK = false
			return Vec2{}, false
		}
		bestS := float64(cur)*10 + 0.5
		found := false
		for _, r := range []float64{6, 11, 16} {
			for k := 0; k < 12; k++ {
				a := float64(k)/12*2*math.Pi + b.rng.Float64()*0.2
				x, y := b.pos[0]+math.Cos(a)*r, b.pos[1]+math.Sin(a)*r
				if !b.nav.lineClear(b.pos[0], b.pos[1], b.pos[2], x, y) {
					continue
				}
				p := Vec3{x, y, b.w.SurfAt(x, y)}
				if !b.insideAt(p, b.t+5, 4) {
					continue
				}
				if !b.w.ShotLOS(Vec3{p[0], p[1], p[2] + b.mzH}, tc) {
					continue
				}
				sc := float64(seenBy(p))*10 + r/6
				if sc < bestS {
					bestS, b.corner, found = sc, p, true
				}
			}
		}
		b.cornerOK = found
	}
	if !b.cornerOK || hdist(b.corner, b.pos) < 1.5 {
		return Vec2{}, false
	}
	return norm2(b.corner[0]-b.pos[0], b.corner[1]-b.pos[1]), true
}

// steerClear rotates dir until the next few metres are walkable.
func (b *Bot) steerClear(dir Vec2) Vec2 {
	if dir == (Vec2{}) {
		return dir
	}
	look := 5.0
	for _, da := range []float64{0, 0.4, -0.4, 0.8, -0.8, 1.2, -1.2, 1.6, -1.6, 2.2, -2.2, math.Pi} {
		a := math.Atan2(dir[1], dir[0]) + da
		ex, ey := b.pos[0]+math.Cos(a)*look, b.pos[1]+math.Sin(a)*look
		if b.nav.lineClear(b.pos[0], b.pos[1], b.pos[2], ex, ey) {
			return Vec2{math.Cos(a), math.Sin(a)}
		}
	}
	return dir
}

// findCover: nearby reachable spot not visible from the threat centroid / known enemies.
func (b *Bot) findCover(cx, cy float64) (Vec3, bool) {
	var eyes []Vec3
	for _, t := range b.enemies {
		if b.t-t.LastSeen < 8 && hdist(t.Pos, b.pos) < 250 {
			eyes = append(eyes, Vec3{t.Pos[0], t.Pos[1], t.Pos[2] + 8*b.scaleOf(t.Cubes)})
		}
	}
	bestS := math.Inf(1)
	var best Vec3
	ok := false
	maxRing := 50.0
	if P["evade_n"] < 50 {
		maxRing = P["cover_max_m"]
	}
	for ring := 10.0; ring <= maxRing; ring += 8 {
		for k := 0; k < 14; k++ {
			a := float64(k)/14*2*math.Pi + b.rng.Float64()*0.3
			x, y := b.pos[0]+math.Cos(a)*ring, b.pos[1]+math.Sin(a)*ring
			id := b.nav.nearestOK(x, y, b.pos[2], 3, true)
			if id < 0 {
				continue
			}
			p := b.nav.pos(id)
			if !b.insideAt(p, b.t+8, 6) {
				continue
			}
			seen := 0
			for _, e := range eyes {
				if b.w.SightLOS(e, Vec3{p[0], p[1], p[2] + 6}) {
					seen++
				}
			}
			s := ring + 40*float64(seen)
			// prefer away from threats
			s -= 0.3 * math.Hypot(p[0]-cx, p[1]-cy)
			if s < bestS {
				bestS, best, ok = s, p, true
			}
		}
	}
	return best, ok
}

// mortarDodge: if an incoming shell will land near us, move away.
func (b *Bot) mortarDodge() (Vec2, bool) {
	g := b.wcfg("mortar", "gravity", 22)
	sr := b.wcfg("mortar", "splash_radius", 7)
	for _, p := range b.o.Projectiles {
		if p.Kind != "mortar" {
			continue
		}
		x, y, z := p.Pos[0], p.Pos[1], p.Pos[2]
		vx, vy, vz := p.Vel[0], p.Vel[1], p.Vel[2]
		dt := 0.05
		for k := 0; k < 160; k++ {
			x += vx * dt
			y += vy * dt
			z += vz * dt
			vz -= g * dt
			if z <= b.w.SurfAt(x, y)+0.2 || b.w.solidAt(x, y, z) {
				tImp := float64(k+1) * dt
				// where will we be? assume we hold position
				dd := math.Hypot(x-b.pos[0], y-b.pos[1])
				if dd < sr+4 && tImp < 4 {
					away := norm2(b.pos[0]-x, b.pos[1]-y)
					if away == (Vec2{}) {
						away = Vec2{1, 0}
					}
					return b.steerClear(away), true
				}
				break
			}
		}
	}
	return Vec2{}, false
}

// bulletDodge: sum of sidestep directions for incoming bullets that would hit us.
func (b *Bot) bulletDodge() Vec2 {
	sc := b.scaleOf(b.me.Cubes)
	R := 2.5*sc + 0.8
	cz := b.pos[2] + 4.5*sc
	var dx, dy float64
	for _, p := range b.o.Projectiles {
		if p.Kind == "mortar" {
			continue
		}
		rx, ry, rz := b.pos[0]-p.Pos[0], b.pos[1]-p.Pos[1], cz-p.Pos[2]
		v2 := p.Vel[0]*p.Vel[0] + p.Vel[1]*p.Vel[1] + p.Vel[2]*p.Vel[2]
		if v2 < 1 {
			continue
		}
		tca := (rx*p.Vel[0] + ry*p.Vel[1] + rz*p.Vel[2]) / v2
		if tca < 0.05 || tca > 1.5 {
			continue
		}
		mx, my, mz := rx-p.Vel[0]*tca, ry-p.Vel[1]*tca, rz-p.Vel[2]*tca
		if math.Abs(mz) > 4.5*sc+0.5 || math.Hypot(mx, my) > R {
			continue
		}
		// sidestep perpendicular to the bullet, toward the side we're already on
		px, py := -p.Vel[1], p.Vel[0]
		l := math.Hypot(px, py)
		if l < 1e-6 {
			continue
		}
		px, py = px/l, py/l
		side := mx*px + my*py
		if math.Abs(side) < 0.3 {
			side = b.strafe
		}
		if side < 0 {
			px, py = -px, -py
		}
		w := 1 / (tca + 0.15)
		dx += px * w
		dy += py * w
	}
	return Vec2{dx, dy}
}

// laserJuke: an enemy charging a laser while aiming at us -> sidestep harder.
func (b *Bot) laserJuke(act *Action) {
	for _, t := range b.enemies {
		if !t.InObs || !t.Charging {
			continue
		}
		br := bearing(t.Pos, b.pos)
		d := hdist(t.Pos, b.pos)
		if math.Abs(wrap(t.Aim[0]-br)) < math.Atan2(6, math.Max(d, 1)) {
			if b.t > b.strafeUntil-0.2 {
				b.strafe = -b.strafe
				b.strafeUntil = b.t + 0.35 + b.rng.Float64()*0.4
			}
			px, py := -math.Sin(br)*b.strafe, math.Cos(br)*b.strafe
			dir := b.steerClear(Vec2{px, py})
			act.Move = dir
			if b.me.Energy > 25 {
				act.Boost = true
			}
			return
		}
	}
}

// ---------------------------------------------------------------- path following

// navTo returns a unit direction toward goal using A* on the nav grid. act may be nil.
func (b *Bot) navTo(goal Vec3, act *Action) Vec2 {
	if hdist(goal, b.pos) < 2.0 {
		return Vec2{}
	}
	replan := b.path == nil || hdist(goal, b.pathGoal) > 6 || b.t-b.pathT > 2.0
	if replan && b.path != nil && len(b.path) == 0 && b.t-b.pathT < 1.0 && hdist(goal, b.pathGoal) < 15 {
		replan = false // last search failed recently: don't burn time
	}
	if replan && len(b.path) > 0 && b.busy() {
		replan = false // keep following the old path this tick
	}
	if !replan && b.pathIdx < len(b.path) {
		p := b.nav.pos(b.path[b.pathIdx])
		if hdist(p, b.pos) > 8 {
			replan = true
		}
	}
	if replan {
		s := b.nav.nearestOK(b.pos[0], b.pos[1], b.pos[2], 8, false)
		g := b.nav.nearestOK(goal[0], goal[1], goal[2], 6, true)
		if g < 0 {
			g = b.nav.nearestOK(goal[0], goal[1], goal[2], 14, false)
		}
		if g < 0 {
			g = b.nav.nearestOK(goal[0], goal[1], goal[2], 30, true)
		}
		b.nav.JetMax = math.Min(40, (b.me.MaxEnergy-8)/1.6+2.5)
		if b.recentDanger() && P["jet_in_danger"] == 0 {
			b.nav.JetMax = 0 // hovering up a wall next to enemies is suicide
		}
		b.nav.NonMainMult = float32(P["nav_nonmain"])
		b.nav.JetRiseCost = float32(P["nav_jet_rise"])
		b.nav.JetFixedCost = float32(P["nav_jet_fixed"])
		b.path = nil
		if s >= 0 && g >= 0 {
			b.path = b.nav.FindPath(s, g, 60000, nil)
		}
		b.pathIdx = 0
		b.pathGoal = goal
		b.pathT = b.t
		if b.path == nil {
			b.path = []int{}
		}
	}
	if len(b.path) == 0 {
		return b.steerClear(norm2(goal[0]-b.pos[0], goal[1]-b.pos[1]))
	}
	// advance to closest node among the next few
	bestI, bestD := b.pathIdx, math.Inf(1)
	for i := b.pathIdx; i < len(b.path) && i < b.pathIdx+12; i++ {
		d := hdist(b.nav.pos(b.path[i]), b.pos)
		if d < bestD {
			bestD, bestI = d, i
		}
	}
	b.pathIdx = bestI
	b.jetting = false
	if bestI+1 < len(b.path) && b.nav.IsJet(b.path[bestI], b.path[bestI+1]) {
		top := b.nav.pos(b.path[bestI+1])
		base := b.nav.pos(b.path[bestI])
		rise := top[2] - base[2]
		dir := norm2(top[0]-b.pos[0], top[1]-b.pos[1])
		if act != nil {
			b.jetting = true
			if b.pos[2] < top[2]+0.8 {
				if b.me.Grounded {
					if b.me.Energy < rise*1.6+8 && hdist(base, b.pos) < 6 {
						return Vec2{} // wait for energy at the foot of the climb
					}
					if hdist(top, b.pos) < 9 {
						act.Jump = true
					}
				} else {
					act.Boost = true
				}
			}
		}
		return dir
	}
	tgtI := bestI + 1
	if tgtI >= len(b.path) {
		tgtI = len(b.path) - 1
	}
	for j := bestI + 2; j < len(b.path) && j <= bestI+10; j++ {
		p := b.nav.pos(b.path[j])
		if b.nav.lineClear(b.pos[0], b.pos[1], b.pos[2], p[0], p[1]) {
			tgtI = j
		} else {
			break
		}
	}
	tp := b.nav.pos(b.path[tgtI])
	if tgtI == len(b.path)-1 && hdist(tp, b.pos) < 2.0 {
		return b.steerClear(norm2(goal[0]-b.pos[0], goal[1]-b.pos[1]))
	}
	return norm2(tp[0]-b.pos[0], tp[1]-b.pos[1])
}

// antiStuck: detect no progress while commanding movement; jump/jet and jiggle.
func (b *Bot) antiStuck(act *Action) {
	if b.jetting {
		b.hist = b.hist[:0]
		b.histMv = b.histMv[:0]
		return
	}
	if b.t < b.unstuckT {
		act.Move = b.unstuckD
		act.Jump = b.me.Grounded
		if b.me.Energy > 10 && !b.recentDanger() {
			act.Boost = true
		}
		return
	}
	n := len(b.hist)
	if n < 12 {
		return
	}
	mv := 0.0
	for _, m := range b.histMv[n-12:] {
		mv += m
	}
	mv /= 12
	if mv < 0.3 {
		return
	}
	moved := hdist(b.hist[n-1], b.hist[n-12])
	expect := mv * b.walkSpeed() * 1.1 * 0.35
	if moved < math.Max(1.5, expect*0.2) {
		b.stuckN++
		b.stuckTimes = append(b.stuckTimes, b.t)
		recent := 0
		for _, st := range b.stuckTimes {
			if b.t-st < 12 {
				recent++
			}
		}
		if recent >= 3 && P["stuck_esc"] > 0 {
			// repeatedly stuck: head for the nearest ground-network node (drop off whatever we're on)
			b.stuckTimes = b.stuckTimes[:0]
			if id := b.nav.nearestOK(b.pos[0], b.pos[1], 1e4, 40, true); id >= 0 {
				p := b.nav.pos(id)
				if hdist(p, b.pos) > 1 {
					b.unstuckD = norm2(p[0]-b.pos[0], p[1]-b.pos[1])
					b.unstuckT = b.t + 2.5
					for r := 0.0; r <= 8; r += 2 {
						u, v := b.nav.nodeAt(b.pos[0]+b.unstuckD[0]*r, b.pos[1]+b.unstuckD[1]*r)
						if id2 := b.nav.id(u, v); id2 >= 0 {
							b.nav.Pen[id2] += 4
						}
					}
					b.path = nil
					b.hist = b.hist[:0]
					b.histMv = b.histMv[:0]
					act.Move = b.unstuckD
					act.Jump = b.me.Grounded
					return
				}
			}
		}
		// penalize nodes ahead
		d := norm2(act.Move[0], act.Move[1])
		for r := 2.0; r <= 6; r += 2 {
			u, v := b.nav.nodeAt(b.pos[0]+d[0]*r, b.pos[1]+d[1]*r)
			if id := b.nav.id(u, v); id >= 0 {
				b.nav.Pen[id] += 3
			}
		}
		b.path = nil
		a := math.Atan2(d[1], d[0]) + (b.rng.Float64()-0.5)*2.4
		if b.stuckN%3 == 0 {
			a += math.Pi
		}
		b.unstuckD = Vec2{math.Cos(a), math.Sin(a)}
		b.unstuckT = b.t + 0.8
		b.hist = b.hist[:0]
		b.histMv = b.histMv[:0]
		act.Move = b.unstuckD
		act.Jump = b.me.Grounded
		act.Boost = b.me.Energy > 10 && !b.recentDanger()
	}
}
