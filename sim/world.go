package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
)

// Interval is a solid vertical span [B,T] in one grid column.
type Interval struct{ B, T float32 }

type Prism struct {
	Kind  string      `json:"kind"`
	Base  float64     `json:"base"`
	Top   float64     `json:"top"`
	Ring  []float64   `json:"ring"`
	Holes [][]float64 `json:"holes"`
}

type worldJSON struct {
	Bounds struct {
		MinX float64 `json:"minX"`
		MaxX float64 `json:"maxX"`
		MinY float64 `json:"minY"`
		MaxY float64 `json:"maxY"`
	} `json:"bounds"`
	Terrain struct {
		MinX float64 `json:"minX"`
		MinY float64 `json:"minY"`
		Step float64 `json:"step"`
		Nx   int     `json:"nx"`
		Ny   int     `json:"ny"`
	} `json:"terrain"`
	Prisms []Prism `json:"prisms"`
}

// World is the static solid geometry: terrain heightmap + rasterized prisms on a 1 m grid.
type World struct {
	MinX, MaxX, MinY, MaxY float64
	// terrain
	tMinX, tMinY, tStep float64
	tNx, tNy            int
	th                  []float32
	// prism grid
	gx0, gy0 float64
	gnx, gny int
	cellOff  []int32 // len gnx*gny+1
	ivs      []Interval
	roofTop  []float32 // highest prism top per cell (or -inf)
	Prisms   []Prism

	Trees     *TreeIndex
	openCells [][2]float64
	roofCells [][3]float64
}

func LoadWorld(dir string) (*World, error) {
	b, err := os.ReadFile(filepath.Join(dir, "v2", "world.json"))
	if err != nil {
		return nil, err
	}
	var wj worldJSON
	if err := json.Unmarshal(b, &wj); err != nil {
		return nil, err
	}
	w := &World{MinX: wj.Bounds.MinX, MaxX: wj.Bounds.MaxX, MinY: wj.Bounds.MinY, MaxY: wj.Bounds.MaxY, Prisms: wj.Prisms}
	w.tMinX, w.tMinY, w.tStep, w.tNx, w.tNy = wj.Terrain.MinX, wj.Terrain.MinY, wj.Terrain.Step, wj.Terrain.Nx, wj.Terrain.Ny
	tb, err := os.ReadFile(filepath.Join(dir, "v2", "terrain.i16"))
	if err != nil {
		return nil, err
	}
	if len(tb) < 2*w.tNx*w.tNy {
		return nil, fmt.Errorf("terrain too short")
	}
	w.th = make([]float32, w.tNx*w.tNy)
	for i := range w.th {
		w.th[i] = float32(int16(binary.LittleEndian.Uint16(tb[2*i:]))) * 0.01
	}
	w.rasterize()
	w.prepare()
	w.Trees = LoadTrees(dir)
	return w, nil
}

func (w *World) rasterize() {
	w.gx0 = math.Floor(w.MinX)
	w.gy0 = math.Floor(w.MinY)
	w.gnx = int(math.Ceil(w.MaxX-w.gx0)) + 1
	w.gny = int(math.Ceil(w.MaxY-w.gy0)) + 1
	n := w.gnx * w.gny
	lists := make([][]Interval, n)
	mark := make([]int32, n) // last prism index+1 marked (dedupe)
	add := func(i, j, pi int, iv Interval) {
		if i < 0 || j < 0 || i >= w.gnx || j >= w.gny {
			return
		}
		c := j*w.gnx + i
		if mark[c] == int32(pi+1) {
			return
		}
		mark[c] = int32(pi + 1)
		lists[c] = append(lists[c], iv)
	}
	for pi, p := range w.Prisms {
		iv := Interval{float32(p.Base), float32(p.Top)}
		polys := [][]float64{p.Ring}
		polys = append(polys, p.Holes...)
		// bbox
		minY, maxY := math.Inf(1), math.Inf(-1)
		for k := 1; k < len(p.Ring); k += 2 {
			minY = math.Min(minY, p.Ring[k])
			maxY = math.Max(maxY, p.Ring[k])
		}
		j0 := int(math.Floor(minY - w.gy0))
		j1 := int(math.Ceil(maxY - w.gy0))
		xs := make([]float64, 0, 64)
		for j := j0; j <= j1; j++ {
			yc := w.gy0 + float64(j) + 0.5
			xs = xs[:0]
			for _, poly := range polys {
				m := len(poly) / 2
				for k := 0; k < m; k++ {
					x1, y1 := poly[2*k], poly[2*k+1]
					x2, y2 := poly[2*((k+1)%m)], poly[2*((k+1)%m)+1]
					if (y1 <= yc) != (y2 <= yc) {
						xs = append(xs, x1+(yc-y1)*(x2-x1)/(y2-y1))
					}
				}
			}
			sort.Float64s(xs)
			for k := 0; k+1 < len(xs); k += 2 {
				i0 := int(math.Ceil(xs[k] - w.gx0 - 0.5))
				i1 := int(math.Floor(xs[k+1] - w.gx0 - 0.5))
				for i := i0; i <= i1; i++ {
					add(i, j, pi, iv)
				}
			}
		}
		// conservative: mark cells the outline passes through (thin walls)
		for _, poly := range polys {
			m := len(poly) / 2
			for k := 0; k < m; k++ {
				x1, y1 := poly[2*k], poly[2*k+1]
				x2, y2 := poly[2*((k+1)%m)], poly[2*((k+1)%m)+1]
				L := math.Hypot(x2-x1, y2-y1)
				steps := int(L/0.3) + 1
				for s := 0; s <= steps; s++ {
					f := float64(s) / float64(steps)
					x := x1 + (x2-x1)*f
					y := y1 + (y2-y1)*f
					add(int(math.Floor(x-w.gx0)), int(math.Floor(y-w.gy0)), pi, iv)
				}
			}
		}
	}
	w.cellOff = make([]int32, n+1)
	w.roofTop = make([]float32, n)
	total := 0
	for c := 0; c < n; c++ {
		l := lists[c]
		sort.Slice(l, func(a, b int) bool { return l[a].B < l[b].B })
		// merge
		merged := l[:0]
		for _, iv := range l {
			if len(merged) > 0 && iv.B <= merged[len(merged)-1].T+0.01 {
				if iv.T > merged[len(merged)-1].T {
					merged[len(merged)-1].T = iv.T
				}
			} else {
				merged = append(merged, iv)
			}
		}
		lists[c] = merged
		w.cellOff[c] = int32(total)
		total += len(merged)
		w.roofTop[c] = float32(math.Inf(-1))
		if len(merged) > 0 {
			w.roofTop[c] = merged[len(merged)-1].T
		}
	}
	w.cellOff[n] = int32(total)
	w.ivs = make([]Interval, 0, total)
	for c := 0; c < n; c++ {
		w.ivs = append(w.ivs, lists[c]...)
	}
}

// Ground returns bilinear terrain height.
func (w *World) Ground(x, y float64) float64 {
	fx := (x - w.tMinX) / w.tStep
	fy := (y - w.tMinY) / w.tStep
	if fx < 0 {
		fx = 0
	}
	if fy < 0 {
		fy = 0
	}
	if fx > float64(w.tNx-1)-1e-6 {
		fx = float64(w.tNx-1) - 1e-6
	}
	if fy > float64(w.tNy-1)-1e-6 {
		fy = float64(w.tNy-1) - 1e-6
	}
	i := int(fx)
	j := int(fy)
	ax := fx - float64(i)
	ay := fy - float64(j)
	h00 := float64(w.th[j*w.tNx+i])
	h10 := float64(w.th[j*w.tNx+i+1])
	h01 := float64(w.th[(j+1)*w.tNx+i])
	h11 := float64(w.th[(j+1)*w.tNx+i+1])
	return (h00*(1-ax)+h10*ax)*(1-ay) + (h01*(1-ax)+h11*ax)*ay
}

func (w *World) cellIndex(x, y float64) int {
	i := int(math.Floor(x - w.gx0))
	j := int(math.Floor(y - w.gy0))
	if i < 0 || j < 0 || i >= w.gnx || j >= w.gny {
		return -1
	}
	return j*w.gnx + i
}

// Cell returns the intervals of the column containing (x,y).
func (w *World) Cell(x, y float64) []Interval {
	c := w.cellIndex(x, y)
	if c < 0 {
		return nil
	}
	return w.ivs[w.cellOff[c]:w.cellOff[c+1]]
}

func (w *World) cellByIJ(i, j int) []Interval {
	if i < 0 || j < 0 || i >= w.gnx || j >= w.gny {
		return nil
	}
	c := j*w.gnx + i
	return w.ivs[w.cellOff[c]:w.cellOff[c+1]]
}

func (w *World) InBounds(x, y, margin float64) bool {
	return x >= w.MinX+margin && x <= w.MaxX-margin && y >= w.MinY+margin && y <= w.MaxY-margin
}

// Solid reports whether point is inside terrain or a prism.
func (w *World) Solid(x, y, z float64) bool {
	if z < w.Ground(x, y) {
		return true
	}
	for _, iv := range w.Cell(x, y) {
		if z >= float64(iv.B) && z <= float64(iv.T) {
			return true
		}
	}
	return false
}

// RayHit returns the distance along the segment a->b (0..len) to first solid, or -1 if clear.
func (w *World) RayHit(a, b Vec3) float64 {
	d := b.Sub(a)
	L := d.Len()
	if L < 1e-9 {
		return -1
	}
	const step = 0.4
	n := int(L/step) + 1
	prev := 0.0
	for k := 1; k <= n; k++ {
		s := L * float64(k) / float64(n)
		p := a.Add(d.Scale(s / L))
		if w.Solid(p.X, p.Y, p.Z) {
			// refine
			lo, hi := prev, s
			for it := 0; it < 6; it++ {
				mid := (lo + hi) / 2
				q := a.Add(d.Scale(mid / L))
				if w.Solid(q.X, q.Y, q.Z) {
					hi = mid
				} else {
					lo = mid
				}
			}
			return lo
		}
		prev = s
	}
	return -1
}

// LOS reports whether the segment a->b is free of solids.
func (w *World) LOS(a, b Vec3) bool {
	d := b.Sub(a)
	L := d.Len()
	if L < 1e-9 {
		return true
	}
	const step = 0.5
	n := int(L/step) + 1
	for k := 1; k < n; k++ {
		f := float64(k) / float64(n)
		p := a.Add(d.Scale(f))
		if w.Solid(p.X, p.Y, p.Z) {
			return false
		}
	}
	return true
}

// SurfaceTop returns the highest standing surface at (x,y): max(ground, top prism).
func (w *World) SurfaceTop(x, y float64) float64 {
	g := w.Ground(x, y)
	c := w.cellIndex(x, y)
	if c >= 0 && float64(w.roofTop[c]) > g {
		return float64(w.roofTop[c])
	}
	return g
}

// OpenGround: no prism interval overlapping [g, g+h] within radius r around (x,y).
func (w *World) OpenGround(x, y, r, h float64) bool {
	g := w.Ground(x, y)
	return w.columnClear(x, y, r, g+0.3, g+h)
}

// columnClear: no prism interval overlaps [z0,z1] within radius r of (x,y).
func (w *World) columnClear(x, y, r, z0, z1 float64) bool {
	i0 := int(math.Floor(x - r - w.gx0))
	i1 := int(math.Floor(x + r - w.gx0))
	j0 := int(math.Floor(y - r - w.gy0))
	j1 := int(math.Floor(y + r - w.gy0))
	for j := j0; j <= j1; j++ {
		for i := i0; i <= i1; i++ {
			cx := w.gx0 + float64(i) + 0.5
			cy := w.gy0 + float64(j) + 0.5
			dx := math.Max(math.Abs(cx-x)-0.5, 0)
			dy := math.Max(math.Abs(cy-y)-0.5, 0)
			if dx*dx+dy*dy > r*r {
				continue
			}
			for _, iv := range w.cellByIJ(i, j) {
				if float64(iv.T) > z0 && float64(iv.B) < z1 {
					return false
				}
			}
		}
	}
	return true
}
