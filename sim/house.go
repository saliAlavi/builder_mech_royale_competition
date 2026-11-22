package main

import (
	"math"
)

// ---------- spawn candidate lists ----------

func (w *World) prepare() {
	for y := w.MinY + 12; y < w.MaxY-12; y += 2 {
		for x := w.MinX + 12; x < w.MaxX-12; x += 2 {
			if !w.OpenGround(x, y, 4, 10) {
				continue
			}
			g := w.Ground(x, y)
			if math.Abs(w.Ground(x+2, y)-g) > 1.2 || math.Abs(w.Ground(x, y+2)-g) > 1.2 || math.Abs(w.Ground(x-2, y)-g) > 1.2 || math.Abs(w.Ground(x, y-2)-g) > 1.2 {
				continue
			}
			w.openCells = append(w.openCells, [2]float64{x, y})
		}
	}
	for j := 0; j < w.gny; j += 2 {
		for i := 0; i < w.gnx; i += 2 {
			ivs := w.cellByIJ(i, j)
			if len(ivs) == 0 {
				continue
			}
			x := w.gx0 + float64(i) + 0.5
			y := w.gy0 + float64(j) + 0.5
			T := float64(ivs[len(ivs)-1].T)
			if T-w.Ground(x, y) < 3 {
				continue
			}
			ok := true
			for dj := -3; dj <= 3 && ok; dj++ {
				for di := -3; di <= 3 && ok; di++ {
					o := w.cellByIJ(i+di, j+dj)
					if len(o) == 0 || math.Abs(float64(o[len(o)-1].T)-T) > 0.3 {
						ok = false
					}
				}
			}
			if ok {
				w.roofCells = append(w.roofCells, [3]float64{x, y, T})
			}
		}
	}
}

// ---------- nav grid ----------

type NavGrid struct {
	x0, y0 float64
	cs     float64
	nx, ny int
	walk   []bool
	gh     []float32
}

func BuildNav(w *World) *NavGrid {
	n := &NavGrid{x0: w.MinX, y0: w.MinY, cs: 2}
	n.nx = int((w.MaxX-w.MinX)/n.cs) + 1
	n.ny = int((w.MaxY-w.MinY)/n.cs) + 1
	n.walk = make([]bool, n.nx*n.ny)
	n.gh = make([]float32, n.nx*n.ny)
	for j := 0; j < n.ny; j++ {
		for i := 0; i < n.nx; i++ {
			x, y := n.center(i, j)
			c := j*n.nx + i
			n.gh[c] = float32(w.Ground(x, y))
			if !w.InBounds(x, y, 4) {
				continue
			}
			n.walk[c] = w.OpenGround(x, y, 2.9, 9.5)
		}
	}
	// slope filter
	for j := 1; j < n.ny-1; j++ {
		for i := 1; i < n.nx-1; i++ {
			c := j*n.nx + i
			if !n.walk[c] {
				continue
			}
			g := n.gh[c]
			for _, d := range []int{1, -1, n.nx, -n.nx} {
				if math.Abs(float64(n.gh[c+d]-g)) > 2.0 {
					n.walk[c] = false
				}
			}
		}
	}
	return n
}

func (n *NavGrid) center(i, j int) (float64, float64) {
	return n.x0 + (float64(i)+0.5)*n.cs, n.y0 + (float64(j)+0.5)*n.cs
}

func (n *NavGrid) cellOf(x, y float64) (int, int) {
	return int((x - n.x0) / n.cs), int((y - n.y0) / n.cs)
}

func (n *NavGrid) ok(i, j int) bool {
	return i >= 0 && j >= 0 && i < n.nx && j < n.ny && n.walk[j*n.nx+i]
}

func (n *NavGrid) nearestWalkable(x, y float64, maxR int) (int, int, bool) {
	ci, cj := n.cellOf(x, y)
	if n.ok(ci, cj) {
		return ci, cj, true
	}
	for r := 1; r <= maxR; r++ {
		bi, bj, bd := -1, -1, 1e18
		for dj := -r; dj <= r; dj++ {
			for di := -r; di <= r; di++ {
				if absI(di) != r && absI(dj) != r {
					continue
				}
				if n.ok(ci+di, cj+dj) {
					d := float64(di*di + dj*dj)
					if d < bd {
						bi, bj, bd = ci+di, cj+dj, d
					}
				}
			}
		}
		if bi >= 0 {
			return bi, bj, true
		}
	}
	return 0, 0, false
}

func absI(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// LineClear: straight walk along nav grid cells.
func (n *NavGrid) LineClear(ax, ay, bx, by float64) bool {
	d := math.Hypot(bx-ax, by-ay)
	steps := int(d/1.0) + 1
	for k := 0; k <= steps; k++ {
		f := float64(k) / float64(steps)
		i, j := n.cellOf(ax+(bx-ax)*f, ay+(by-ay)*f)
		if !n.ok(i, j) {
			return false
		}
	}
	return true
}

type pqItem struct {
	f   float32
	idx int32
}
type pq []pqItem

func (p *pq) push(it pqItem) {
	h := append(*p, it)
	i := len(h) - 1
	for i > 0 {
		par := (i - 1) / 2
		if h[par].f <= h[i].f {
			break
		}
		h[par], h[i] = h[i], h[par]
		i = par
	}
	*p = h
}

func (p *pq) pop() pqItem {
	h := *p
	top := h[0]
	n := len(h) - 1
	h[0] = h[n]
	h = h[:n]
	i := 0
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		m := l
		if r := l + 1; r < n && h[r].f < h[l].f {
			m = r
		}
		if h[i].f <= h[m].f {
			break
		}
		h[i], h[m] = h[m], h[i]
		i = m
	}
	*p = h
	return top
}

type NavScratch struct {
	g    []float32
	par  []int32
	gen  []uint32
	cl   []uint32
	cur  uint32
	heap pq
}

func (n *NavGrid) NewScratch() *NavScratch {
	N := n.nx * n.ny
	return &NavScratch{g: make([]float32, N), par: make([]int32, N), gen: make([]uint32, N), cl: make([]uint32, N)}
}

// Path returns waypoints from start to goal (world coords) or nil.
func (n *NavGrid) Path(sc *NavScratch, sx, sy, gx, gy float64, maxExp int) [][2]float64 {
	si, sj, ok1 := n.nearestWalkable(sx, sy, 6)
	gi, gj, ok2 := n.nearestWalkable(gx, gy, 12)
	if !ok1 || !ok2 {
		return nil
	}
	sc.cur++
	gen := sc.cur
	start := int32(sj*n.nx + si)
	goal := int32(gj*n.nx + gi)
	h := func(c int32) float32 {
		i, j := int(c)%n.nx, int(c)/n.nx
		dx := float64(absI(i - gi))
		dy := float64(absI(j - gj))
		return float32((dx + dy) + (math.Sqrt2-2)*math.Min(dx, dy))
	}
	sc.heap = sc.heap[:0]
	sc.g[start] = 0
	sc.gen[start] = gen
	sc.par[start] = -1
	sc.heap.push(pqItem{h(start), start})
	exp := 0
	best := start
	bestH := h(start)
	found := false
	for len(sc.heap) > 0 {
		it := sc.heap.pop()
		c := it.idx
		if sc.cl[c] == gen {
			continue
		}
		sc.cl[c] = gen
		if c == goal {
			found = true
			break
		}
		if hh := h(c); hh < bestH {
			best, bestH = c, hh
		}
		exp++
		if exp > maxExp {
			break
		}
		ci, cj := int(c)%n.nx, int(c)/n.nx
		gc := sc.g[c]
		for dj := -1; dj <= 1; dj++ {
			for di := -1; di <= 1; di++ {
				if di == 0 && dj == 0 {
					continue
				}
				ni, nj := ci+di, cj+dj
				if !n.ok(ni, nj) {
					continue
				}
				if di != 0 && dj != 0 && (!n.ok(ci+di, cj) || !n.ok(ci, cj+dj)) {
					continue
				}
				nc := int32(nj*n.nx + ni)
				if sc.cl[nc] == gen {
					continue
				}
				if math.Abs(float64(n.gh[nc]-n.gh[c])) > 2.2 {
					continue
				}
				cost := float32(1)
				if di != 0 && dj != 0 {
					cost = math.Sqrt2
				}
				ng := gc + cost
				if sc.gen[nc] != gen || ng < sc.g[nc] {
					sc.gen[nc] = gen
					sc.g[nc] = ng
					sc.par[nc] = c
					sc.heap.push(pqItem{ng + h(nc), nc})
				}
			}
		}
	}
	end := goal
	if !found {
		end = best
	}
	var rev [][2]float64
	for c := end; c >= 0; c = sc.par[c] {
		x, y := n.center(int(c)%n.nx, int(c)/n.nx)
		rev = append(rev, [2]float64{x, y})
		if c == start {
			break
		}
	}
	out := make([][2]float64, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	if found {
		out = append(out, [2]float64{gx, gy})
	}
	return out
}

// ---------- house bot ----------

// HouseNoise is the aim noise sigma (deg) per difficulty (easy, medium, hard, expert).
var HouseNoise = [4]float64{7, 5, 3, 2}

// HouseFireRange is the max distance at which each difficulty opens fire.
var HouseFireRange = [4]float64{70, 100, 140, 180}

// HouseHunters is how many house bots converge on a target known only via squad vision.
var HouseHunters = 2

type HouseBot struct {
	duty       float64
	hunt       float64
	home       Vec3
	burstT     float64
	burstOn    bool
	Diff       int
	noise      float64 // rad
	react      float64
	lead       float64
	strafe     bool
	engage     float64
	path       [][2]float64
	pathIdx    int
	pathGoal   Vec3
	replanT    float64
	visSince   float64
	vis        bool
	strafeDir  float64
	strafeT    float64
	stuckT     float64
	unstickT   float64
	wanderGoal Vec3
	wanderPh   int
	nYaw, nPit float64
	noiseT     float64
	alertYaw   float64
	alertT     float64
	lastTgt    int
}

var diffNames = []string{"easy", "medium", "hard", "expert"}

func NewHouse(diff int) *HouseBot {
	h := &HouseBot{Diff: diff, visSince: -1, alertT: -100, lastTgt: -1, wanderPh: -1, strafeDir: 1}
	switch diff {
	case 0:
		h.noise, h.react, h.lead, h.strafe, h.engage = HouseNoise[0]*math.Pi/180, 1.2, 0.3, false, 45
		h.duty, h.hunt = 0.6, 150
	case 1:
		h.noise, h.react, h.lead, h.strafe, h.engage = HouseNoise[1]*math.Pi/180, 0.8, 0.6, false, 50
		h.duty, h.hunt = 0.75, 180
	case 2:
		h.noise, h.react, h.lead, h.strafe, h.engage = HouseNoise[2]*math.Pi/180, 0.5, 0.85, true, 55
		h.duty, h.hunt = 0.9, 220
	default:
		h.noise, h.react, h.lead, h.strafe, h.engage = HouseNoise[3]*math.Pi/180, 0.3, 1.0, true, 60
		h.duty, h.hunt = 1.0, 260
	}
	return h
}

func (h *HouseBot) onDamaged(s *Sim, by int, src Vec3) {
	if by < 0 || by >= len(s.Mechs) {
		return
	}
	h.alertYaw = 0
	h.alertT = s.T
	a := s.Mechs[by]
	if a.Squad == 0 {
		s.squadHeard = a.Pos.Add(V(s.gauss()*6, s.gauss()*6, 0))
		s.squadHeardT = s.T
	}
	_ = src
}

// squadPass updates shared knowledge (vision + hearing) once per decision.
func (s *Sim) squadPass() {
	var house []*Mech
	for _, m := range s.Mechs {
		if m.Alive && m.House != nil {
			house = append(house, m)
			m.House.vis = false
		}
	}
	if len(house) == 0 {
		return
	}
	for _, o := range s.Mechs {
		if !o.Alive || o.Squad != 0 {
			continue
		}
		for _, hm := range house {
			if s.inViewCone(hm, o.center()) && s.canSee(hm, o.center(), o.top()) {
				s.squadSeen[o.ID] = s.T
				if hm.House.lastTgt == o.ID {
					hm.House.vis = true
				} else {
					hm.House.vis = true
				}
			}
		}
	}
	for _, sd := range s.Sounds {
		if sd.Src < 0 || sd.Src >= len(s.Mechs) || s.Mechs[sd.Src].Squad != 0 {
			continue
		}
		rng := s.C.Sound.Range[sd.Kind]
		for _, hm := range house {
			d := hm.Pos.Dist(sd.Pos)
			if d <= rng {
				e := 4 + 0.08*d
				s.squadHeard = sd.Pos.Add(V(s.gauss()*e, s.gauss()*e, 0))
				s.squadHeardT = s.T
				break
			}
		}
	}
}

func (s *Sim) houseDecide(m *Mech) {
	h := m.House
	c := &m.cmd
	z := s.zone(s.T)
	*c = Cmd{Yaw: c.Yaw, Pitch: c.Pitch, Pick: -1, Take: -1, Drop: -1}
	if m.offer != nil {
		c.Pick = 0
	}
	// target: nearest known non-squad mech
	var tgt *Mech
	bd := 1e18
	for _, o := range s.Mechs {
		if !o.Alive || o.Squad == m.Squad && m.Squad != 0 || o == m {
			continue
		}
		t, ok := s.squadSeen[o.ID]
		if !ok || s.T-t > s.C.Vision.MemoryS {
			continue
		}
		d := o.Pos.Dist(m.Pos)
		if !h.vis {
			// known only through the squad: only the closest few within hunt radius converge
			if d > h.hunt {
				continue
			}
			rank := 0
			for _, q := range s.Mechs {
				if q != m && q.Alive && q.House != nil && q.Pos.Dist(o.Pos) < d {
					rank++
				}
			}
			if rank >= HouseHunters {
				continue
			}
		}
		if d < bd {
			tgt, bd = o, d
		}
	}
	myVis := false
	if tgt != nil {
		myVis = s.inViewCone(m, tgt.center()) && s.canSee(m, tgt.center(), tgt.top())
	}
	if myVis {
		if h.visSince < 0 || h.lastTgt != tgt.ID {
			h.visSince = s.T
		}
		h.lastTgt = tgt.ID
	} else {
		h.visSince = -1
	}
	// zone safety
	safeC, safeR := z.Center, z.R
	if s.T > z.ShrinkStart-25 {
		safeC, safeR = z.Next, z.NextR
	}
	dSafe := math.Hypot(m.Pos.X-safeC[0], m.Pos.Y-safeC[1])
	dCur := math.Hypot(m.Pos.X-z.Center[0], m.Pos.Y-z.Center[1])
	mustZone := dSafe > safeR-12 || dCur > z.R-5
	var goal Vec3
	haveGoal := false
	direct := false // standing combat movement
	boost := false
	if mustZone && !(myVis && bd < 70 && dCur < z.R-5) {
		goal = V(safeC[0], safeC[1], 0)
		haveGoal = true
		boost = dCur > z.R-10
	} else if tgt != nil {
		if myVis && bd < math.Min(h.engage+15, HouseFireRange[h.Diff]) {
			direct = true
		} else {
			goal = tgt.Pos
			haveGoal = true
			boost = bd > 120 && m.Energy > 50
		}
	} else if s.T-s.squadHeardT < 10 && s.squadHeard.HDist(m.Pos) > 10 && s.squadHeard.HDist(m.Pos) < h.hunt {
		goal = s.squadHeard
		haveGoal = true
	} else {
		// loot: lightning/laser
		var best *Pickup
		bdp := 45.0
		if s.T < 50 {
			bdp = 80
		}
		if m.Weapons[1] == nil || m.Weapons[1].Kind == "mortar" {
			for _, p := range s.Pickups {
				if !p.Alive || (p.W.Kind != "lightning" && p.W.Kind != "laser") {
					continue
				}
				if d := p.Pos.Dist(m.Pos); d < bdp && math.Abs(p.Pos.Z-m.Pos.Z) < 3 {
					best, bdp = p, d
				}
			}
		}
		if best != nil {
			goal = best.Pos
			haveGoal = true
			if bdp < s.C.Loot.PickupRadius-0.3 {
				c.Take = best.ID
				if m.Weapons[1] != nil {
					c.Drop = 1
				}
			}
		} else {
			ph := s.zonePhase()
			if h.home == (Vec3{}) {
				h.home = m.Pos
			}
			if s.T < s.C.Zone.Phases[0].HoldS-10 {
				// early game: loot around spawn
				if m.Pos.HDist(h.wanderGoal) < 6 || h.wanderGoal == (Vec3{}) {
					a := s.Rng.Float64() * 2 * math.Pi
					r := 60 * math.Sqrt(s.Rng.Float64())
					h.wanderGoal = V(h.home.X+r*math.Cos(a), h.home.Y+r*math.Sin(a), 0)
				}
			} else if h.wanderPh != ph || m.Pos.HDist(h.wanderGoal) < 8 {
				h.wanderPh = ph
				a := s.Rng.Float64() * 2 * math.Pi
				r := z.NextR * 0.7 * math.Sqrt(s.Rng.Float64())
				h.wanderGoal = V(z.Next[0]+r*math.Cos(a), z.Next[1]+r*math.Sin(a), 0)
			}
			goal = h.wanderGoal
			haveGoal = true
		}
	}
	mv := [2]float64{}
	if haveGoal {
		mv = s.navSteer(m, goal)
	}
	if direct && tgt != nil {
		dx, dy := tgt.Pos.X-m.Pos.X, tgt.Pos.Y-m.Pos.Y
		dl := math.Hypot(dx, dy) + 1e-9
		ux, uy := dx/dl, dy/dl
		approach := 0.0
		if dl > h.engage+5 {
			approach = 1
		} else if dl < h.engage-20 {
			approach = -0.7
		}
		if h.strafe {
			h.strafeT -= s.DT * 2
			if h.strafeT <= 0 {
				h.strafeT = 0.7 + s.Rng.Float64()*1.2
				if s.Rng.Float64() < 0.7 {
					h.strafeDir = -h.strafeDir
				}
			}
			mv = [2]float64{ux*approach - uy*h.strafeDir, uy*approach + ux*h.strafeDir}
		} else {
			mv = [2]float64{ux * approach, uy * approach}
		}
		// avoid walking into walls in combat: if blocked ahead, fall back to nav
		if l := math.Hypot(mv[0], mv[1]); l > 0 {
			mv[0] /= l
			mv[1] /= l
			if !s.Nav.LineClear(m.Pos.X, m.Pos.Y, m.Pos.X+mv[0]*4, m.Pos.Y+mv[1]*4) {
				h.strafeDir = -h.strafeDir
				mv = [2]float64{ux * approach, uy * approach}
			}
		}
		// zone check while fighting
		if dCur > z.R-8 {
			mv = s.navSteer(m, V(z.Center[0], z.Center[1], 0))
		}
	}
	c.Move = mv
	c.Boost = boost
	// stuck recovery
	ml := math.Hypot(mv[0], mv[1])
	if ml > 0.5 && m.Vel.HLen() < 1.5 {
		h.stuckT += s.DT * 2
	} else {
		h.stuckT = 0
	}
	if h.stuckT > 0.8 {
		h.unstickT = 1.2
		h.stuckT = 0
		h.replanT = 0
	}
	if h.unstickT > 0 {
		h.unstickT -= s.DT * 2
		c.Jump = m.Grounded
		c.Boost = true
	}
	// aim & fire
	if tgt != nil {
		tc := tgt.center()
		mz := m.Pos.Add(V(0, 0, m.MS["muzzle_height"]*m.scale()))
		dist := tc.Dist(mz)
		h.noiseT -= s.DT * 2
		if h.noiseT <= 0 {
			h.noiseT = 0.3
			h.nYaw, h.nPit = s.gauss()*h.noise, s.gauss()*h.noise*0.6
		}
		gs := m.WS["gun"]["speed"]
		tof := dist / gs
		pred := tc.Add(tgt.Vel.Scale(tof * h.lead))
		if !tgt.Grounded {
			pred.Z -= 0.5 * m.MS["gravity"] * tof * tof * h.lead * 0.5
		}
		d := pred.Sub(mz)
		yaw := math.Atan2(d.Y, d.X) + h.nYaw
		pit := math.Atan2(d.Z, d.HLen()) + h.nPit
		c.Yaw, c.Pitch = yaw, pit
		h.burstT -= s.DT * 2
		if h.burstT <= 0 {
			h.burstOn = !h.burstOn
			if h.burstOn {
				h.burstT = 1.5
			} else {
				h.burstT = 1.5 * (1 - h.duty) / h.duty
			}
		}
		if myVis && s.T-h.visSince >= h.react && (h.burstOn || h.duty >= 1) {
			yerr := math.Abs(wrapAngle(m.Yaw - yaw))
			for i, w := range m.Weapons {
				if w == nil {
					continue
				}
				switch w.Kind {
				case "gun":
					c.Fire[i] = yerr < 3*math.Pi/180 && dist < math.Min(m.WS["gun"]["range"]*0.95, HouseFireRange[h.Diff])
				case "lightning":
					c.Fire[i] = dist < m.WS["lightning"]["range"]*0.95 && yerr < 0.3
				case "laser":
					if w.Charging {
						full := w.Charge >= m.WS["laser"]["charge"]-1e-6
						c.Fire[i] = !(full && yerr < 1.2*math.Pi/180)
					} else {
						c.Fire[i] = dist < m.WS["laser"]["range"]*0.9 && yerr < 0.2
					}
				case "mortar":
					c.Fire[i] = false
				}
			}
		} else {
			// release laser if charging without target
			for i, w := range m.Weapons {
				if w != nil && w.Kind == "laser" && w.Charging {
					c.Fire[i] = true
					if !myVis {
						c.Fire[i] = false
					}
				}
			}
		}
	} else {
		// look where we're going, or toward the alert / heard sound
		if s.T-s.squadHeardT < 10 {
			c.Yaw = math.Atan2(s.squadHeard.Y-m.Pos.Y, s.squadHeard.X-m.Pos.X)
		} else if ml > 0.1 {
			c.Yaw = math.Atan2(mv[1], mv[0])
		} else {
			c.Yaw = m.Yaw + 0.6
		}
		c.Pitch = 0
	}
}

func (s *Sim) zonePhase() int {
	t0 := 0.0
	for i, p := range s.C.Zone.Phases {
		t0 += p.HoldS + p.ShrinkS
		if s.T < t0 {
			return i
		}
	}
	return len(s.C.Zone.Phases)
}

// navSteer returns a move direction toward goal using A* waypoints.
func (s *Sim) navSteer(m *Mech, goal Vec3) [2]float64 {
	h := m.House
	if s.Nav.LineClear(m.Pos.X, m.Pos.Y, goal.X, goal.Y) && m.Pos.HDist(goal) < 120 {
		dx, dy := goal.X-m.Pos.X, goal.Y-m.Pos.Y
		l := math.Hypot(dx, dy)
		if l < 1.5 {
			return [2]float64{}
		}
		h.path = nil
		return [2]float64{dx / l, dy / l}
	}
	if h.path == nil || s.T >= h.replanT || h.pathGoal.HDist(goal) > 30 || (h.pathIdx >= len(h.path)-1 && h.pathGoal.HDist(goal) > 5) {
		h.path = s.Nav.Path(s.navScratch, m.Pos.X, m.Pos.Y, goal.X, goal.Y, 12000)
		h.pathIdx = 0
		h.pathGoal = goal
		h.replanT = s.T + 3 + s.Rng.Float64()*1.5
	}
	if len(h.path) == 0 {
		dx, dy := goal.X-m.Pos.X, goal.Y-m.Pos.Y
		l := math.Hypot(dx, dy) + 1e-9
		return [2]float64{dx / l, dy / l}
	}
	// advance index to furthest visible waypoint (lookahead)
	best := h.pathIdx
	for k := h.pathIdx; k < len(h.path) && k < h.pathIdx+14; k++ {
		if s.Nav.LineClear(m.Pos.X, m.Pos.Y, h.path[k][0], h.path[k][1]) {
			best = k
		}
	}
	h.pathIdx = best
	wp := h.path[best]
	dx, dy := wp[0]-m.Pos.X, wp[1]-m.Pos.Y
	l := math.Hypot(dx, dy)
	if l < 1.0 {
		if best+1 < len(h.path) {
			h.pathIdx = best + 1
			wp = h.path[best+1]
			dx, dy = wp[0]-m.Pos.X, wp[1]-m.Pos.Y
			l = math.Hypot(dx, dy)
		} else {
			return [2]float64{}
		}
	}
	if l < 1e-6 {
		return [2]float64{}
	}
	return [2]float64{dx / l, dy / l}
}
