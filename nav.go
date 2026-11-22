package main

// 2 m navigation grid over the standable surface. Node (u,v) sits on 1 m vertex (2u, 2v).
// A node is OK if a mech (plus margin) standing there touches no wall: no surface within
// clearR that rises more than step_up above it (allowing for walkable slope).
// Edges go up at most step_up per metre; any drop is allowed (falling off roofs is free).

import (
	"container/heap"
	"math"
	"time"
)

const (
	navStride = 2
	clearR    = 3.2
	stepUp    = 1.2
	slopeTan  = 1.19 // tan(50 deg)
)

type Nav struct {
	w        *World
	NU, NV   int
	H        []float32
	OK       []bool
	Comp     []int32
	MainComp int32
	Pen      []float32 // dynamic extra cost (stuck spots, danger)
	Cover    []uint8   // lazily computed cover score+1 (0 = unknown)
	g        []float32
	par      []int32
	stamp    []uint32
	closed   []uint32
	gen      uint32
	Jet      map[int32][]jetEdge // jump/jet climbs onto higher surfaces
	JetMax   float64             // max rise allowed in the current search (set by caller)
}

type jetEdge struct {
	to   int32
	rise float32
	dist float32
}

func buildNav(w *World) *Nav {
	nu := (w.NX + navStride - 1) / navStride
	nv := (w.NY + navStride - 1) / navStride
	n := nu * nv
	nav := &Nav{w: w, NU: nu, NV: nv, H: make([]float32, n), OK: make([]bool, n), Comp: make([]int32, n),
		Pen: make([]float32, n), Cover: make([]uint8, n), g: make([]float32, n), par: make([]int32, n),
		stamp: make([]uint32, n), closed: make([]uint32, n)}
	R := int(math.Ceil(clearR))
	type off struct {
		di, dj int
		d      float64
	}
	var disk []off
	for dj := -R; dj <= R; dj++ {
		for di := -R; di <= R; di++ {
			d := math.Hypot(float64(di), float64(dj))
			if d <= clearR && d > 0 {
				disk = append(disk, off{di, dj, d})
			}
		}
	}
	margin := 4.0
	for v := 0; v < nv; v++ {
		for u := 0; u < nu; u++ {
			id := v*nu + u
			i, j := u*navStride, v*navStride
			if i >= w.NX || j >= w.NY {
				continue
			}
			c := j*w.NX + i
			h := w.Surf[c]
			nav.H[id] = h
			x := w.MinX + float64(i)*w.Step
			y := w.MinY + float64(j)*w.Step
			if x < w.BMinX+margin || x > w.BMaxX-margin || y < w.BMinY+margin || y > w.BMaxY-margin {
				continue
			}
			ok := true
			for _, o := range disk {
				ii, jj := i+o.di, j+o.dj
				if ii < 0 || jj < 0 || ii >= w.NX || jj >= w.NY {
					ok = false
					break
				}
				s := w.Surf[jj*w.NX+ii]
				if float64(s-h)-slopeTan*o.d > stepUp {
					ok = false
					break
				}
			}
			nav.OK[id] = ok
		}
	}
	// connected components over symmetric edges
	for i := range nav.Comp {
		nav.Comp[i] = -1
	}
	var best, bestN int32 = -1, 0
	var comp int32
	queue := make([]int32, 0, 1024)
	for s := 0; s < n; s++ {
		if !nav.OK[s] || nav.Comp[s] >= 0 {
			continue
		}
		queue = queue[:0]
		queue = append(queue, int32(s))
		nav.Comp[s] = comp
		cnt := int32(0)
		for len(queue) > 0 {
			a := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			cnt++
			au, av := int(a)%nu, int(a)/nu
			for k := 0; k < 8; k++ {
				bu, bv := au+dU[k], av+dV[k]
				if bu < 0 || bv < 0 || bu >= nu || bv >= nv {
					continue
				}
				b := int32(bv*nu + bu)
				if nav.Comp[b] >= 0 || !nav.OK[b] {
					continue
				}
				if nav.canStep(au, av, bu, bv, k) && nav.canStep(bu, bv, au, av, (k+4)%8) {
					nav.Comp[b] = comp
					queue = append(queue, b)
				}
			}
		}
		if cnt > bestN {
			best, bestN = comp, cnt
		}
		comp++
	}
	nav.MainComp = best
	nav.buildJets()
	nav.JetMax = 30
	return nav
}

// buildJets: from each OK node, look up to 10 m in 8 directions for the first OK node on a
// surface more than step_up higher with nothing taller in between: a jump+jet climb.
func (n *Nav) buildJets() {
	n.Jet = map[int32][]jetEdge{}
	w := n.w
	for a := 0; a < len(n.OK); a++ {
		if !n.OK[a] {
			continue
		}
		au, av := a%n.NU, a/n.NU
		ha := n.H[a]
		for k := 0; k < 8; k++ {
			// only bother if the direct neighbour isn't a normal step
			for st := 2; st <= 5; st++ {
				bu, bv := au+dU[k]*st, av+dV[k]*st
				b := n.id(bu, bv)
				if b < 0 {
					break
				}
				if !n.OK[b] {
					continue
				}
				hb := n.H[b]
				rise := hb - ha
				if rise <= stepUp+0.05 || rise > 45 {
					break
				}
				// nothing taller than the destination along the way
				clear := true
				ax, ay := float64(au*navStride), float64(av*navStride)
				bx, by := float64(bu*navStride), float64(bv*navStride)
				steps := st * navStride
				for q := 1; q < steps; q++ {
					f := float64(q) / float64(steps)
					i := int(ax + (bx-ax)*f + 0.5)
					j := int(ay + (by-ay)*f + 0.5)
					if i < 0 || j < 0 || i >= w.NX || j >= w.NY || w.Surf[j*w.NX+i] > hb+0.6 {
						clear = false
						break
					}
				}
				if clear {
					d := float32(math.Hypot(bx-ax, by-ay))
					n.Jet[int32(a)] = append(n.Jet[int32(a)], jetEdge{int32(b), rise, d})
				}
				break
			}
		}
	}
}

// IsJet: is the step a->b a jet climb (non-adjacent nodes)?
func (n *Nav) IsJet(a, b int) bool {
	du := a%n.NU - b%n.NU
	dv := a/n.NU - b/n.NU
	return du > 1 || du < -1 || dv > 1 || dv < -1
}

// 8 directions; k and (k+4)%8 are opposite.
var dU = [8]int{1, 1, 0, -1, -1, -1, 0, 1}
var dV = [8]int{0, 1, 1, 1, 0, -1, -1, -1}

func (n *Nav) canStep(au, av, bu, bv, k int) bool {
	w := n.w
	ha := n.H[av*n.NU+au]
	hb := n.H[bv*n.NU+bu]
	mi := au*navStride + dU[k]
	mj := av*navStride + dV[k]
	if mi < 0 || mj < 0 || mi >= w.NX || mj >= w.NY {
		return false
	}
	hm := w.Surf[mj*w.NX+mi]
	if hm-ha > stepUp+0.05 || hb-hm > stepUp+0.05 {
		return false
	}
	if k%2 == 1 { // diagonal: no corner cutting
		if !n.OK[av*n.NU+bu] || !n.OK[bv*n.NU+au] {
			return false
		}
	}
	return true
}

func (n *Nav) nodeAt(x, y float64) (int, int) {
	u := int(math.Floor((x-n.w.MinX)/(n.w.Step*navStride) + 0.5))
	v := int(math.Floor((y-n.w.MinY)/(n.w.Step*navStride) + 0.5))
	return u, v
}

func (n *Nav) id(u, v int) int {
	if u < 0 || v < 0 || u >= n.NU || v >= n.NV {
		return -1
	}
	return v*n.NU + u
}

func (n *Nav) pos(id int) Vec3 {
	u, v := id%n.NU, id/n.NU
	return Vec3{n.w.MinX + float64(u*navStride)*n.w.Step, n.w.MinY + float64(v*navStride)*n.w.Step, float64(n.H[id])}
}

func (n *Nav) Walkable(x, y float64) bool {
	u, v := n.nodeAt(x, y)
	id := n.id(u, v)
	return id >= 0 && n.OK[id]
}

func (n *Nav) InMain(id int) bool {
	return id >= 0 && n.OK[id] && n.Comp[id] == n.MainComp
}

// nearestOK finds the closest OK node to (x,y,z) within radius r (m), preferring a surface near z.
// If main is true only main-component nodes count.
func (n *Nav) nearestOK(x, y, z float64, r float64, main bool) int {
	u0, v0 := n.nodeAt(x, y)
	R := int(math.Ceil(r / navStride))
	best := -1
	bestD := math.Inf(1)
	for dv := -R; dv <= R; dv++ {
		for du := -R; du <= R; du++ {
			id := n.id(u0+du, v0+dv)
			if id < 0 || !n.OK[id] {
				continue
			}
			if main && n.Comp[id] != n.MainComp {
				continue
			}
			p := n.pos(id)
			d := math.Hypot(p[0]-x, p[1]-y)
			dz := float64(n.H[id]) - z
			if dz > stepUp {
				d += (dz - stepUp) * 3 // above us: hard to reach
			}
			if d < bestD {
				bestD, best = d, id
			}
		}
	}
	return best
}

type pqItem struct {
	id int32
	f  float32
}
type pq []pqItem

func (p pq) Len() int            { return len(p) }
func (p pq) Less(i, j int) bool  { return p[i].f < p[j].f }
func (p pq) Swap(i, j int)       { p[i], p[j] = p[j], p[i] }
func (p *pq) Push(x interface{}) { *p = append(*p, x.(pqItem)) }
func (p *pq) Pop() interface{} {
	old := *p
	it := old[len(old)-1]
	*p = old[:len(old)-1]
	return it
}

var stepCost = [8]float32{1, 1.41421, 1, 1.41421, 1, 1.41421, 1, 1.41421}

// FindPath runs A* from node s to node t. costFn (optional) adds per-node extra cost multiplier.
// Returns node ids from s to t (inclusive) or nil.
func (n *Nav) FindPath(s, t int, maxExp int, extra func(id int) float32) []int {
	if s < 0 || t < 0 {
		return nil
	}
	n.gen++
	if n.gen == 0 {
		for i := range n.stamp {
			n.stamp[i], n.closed[i] = 0, 0
		}
		n.gen = 1
	}
	gen := n.gen
	tu, tv := t%n.NU, t/n.NU
	hfun := func(id int) float32 {
		du := math.Abs(float64(id%n.NU - tu))
		dv := math.Abs(float64(id/n.NU - tv))
		mn := math.Min(du, dv)
		return float32(1.4 * navStride * (du + dv - (2-math.Sqrt2)*mn))
	}
	open := &pq{}
	t0 := time.Now()
	n.g[s] = 0
	n.par[s] = -1
	n.stamp[s] = gen
	heap.Push(open, pqItem{int32(s), hfun(s)})
	exp := 0
	for open.Len() > 0 {
		it := heap.Pop(open).(pqItem)
		a := int(it.id)
		if n.closed[a] == gen {
			continue
		}
		n.closed[a] = gen
		if a == t {
			var path []int
			for c := a; c >= 0; c = int(n.par[c]) {
				path = append(path, c)
				if c == s {
					break
				}
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path
		}
		exp++
		if exp > maxExp {
			return nil
		}
		if exp&1023 == 0 && time.Since(t0) > 12*time.Millisecond {
			return nil
		}
		au, av := a%n.NU, a/n.NU
		ga := n.g[a]
		for k := 0; k < 8; k++ {
			bu, bv := au+dU[k], av+dV[k]
			if bu < 0 || bv < 0 || bu >= n.NU || bv >= n.NV {
				continue
			}
			b := bv*n.NU + bu
			if !n.OK[b] || n.closed[b] == gen {
				continue
			}
			if !n.canStep(au, av, bu, bv, k) {
				continue
			}
			c := stepCost[k] * navStride * (1 + n.Pen[b])
			if n.Comp[b] != n.MainComp {
				c *= 2.5 // roofs / stadium stands: only when needed
			}
			if extra != nil {
				c *= 1 + extra(b)
			}
			// discourage big drops slightly (no damage, but loses position)
			if dh := n.H[a] - n.H[b]; dh > 3 {
				c += dh * 0.3
			}
			ng := ga + c
			if n.stamp[b] != gen || ng < n.g[b] {
				n.stamp[b] = gen
				n.g[b] = ng
				n.par[b] = int32(a)
				heap.Push(open, pqItem{int32(b), ng + hfun(b)})
			}
		}
		for _, e := range n.Jet[int32(a)] {
			if float64(e.rise) > n.JetMax {
				continue
			}
			b := int(e.to)
			if n.closed[b] == gen {
				continue
			}
			// jets are loud and slow: cost distance + 3 per metre climbed + fixed 25
			ng := ga + e.dist + 7*e.rise + 70 + n.Pen[b]*navStride
			if n.stamp[b] != gen || ng < n.g[b] {
				n.stamp[b] = gen
				n.g[b] = ng
				n.par[b] = int32(a)
				heap.Push(open, pqItem{int32(b), ng + hfun(b)})
			}
		}
	}
	return nil
}

// lineClear: can a mech walk straight from a to b (both world xy)? Samples every metre.
func (n *Nav) lineClear(ax, ay, az, bx, by float64) bool {
	d := math.Hypot(bx-ax, by-ay)
	steps := int(d) + 1
	prevH := az
	for k := 1; k <= steps; k++ {
		f := float64(k) / float64(steps)
		x, y := ax+(bx-ax)*f, ay+(by-ay)*f
		u, v := n.nodeAt(x, y)
		id := n.id(u, v)
		if id < 0 || !n.OK[id] {
			return false
		}
		h := n.w.SurfAt(x, y)
		if h-prevH > stepUp+0.1 {
			return false
		}
		prevH = h
	}
	return true
}

// coverScore: fraction of 16 directions with a tall solid within 18 m (0..16), cached.
func (n *Nav) coverScore(id int) int {
	if n.Cover[id] != 0 {
		return int(n.Cover[id]) - 1
	}
	p := n.pos(id)
	h := float64(n.H[id])
	cnt := 0
	for k := 0; k < 16; k++ {
		a := float64(k) * math.Pi / 8
		cx, cy := math.Cos(a), math.Sin(a)
		for r := 3.0; r <= 18; r += 1.5 {
			s := n.w.SurfAt(p[0]+cx*r, p[1]+cy*r)
			if s-h > 6 {
				cnt++
				break
			}
		}
	}
	n.Cover[id] = uint8(cnt + 1)
	return cnt
}
