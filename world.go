package main

// Static world model built from v2/world.json + v2/terrain.i16 (protocol §11).
// 1 m grid aligned with the terrain vertices. Per cell: terrain height, prism intervals
// (solid [base, top] ranges, used for line of sight), the standable surface reached by
// climbing up from the terrain (used for navigation), and tree canopy density for vision.

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
)

type prismJSON struct {
	Kind  string          `json:"kind"`
	Base  float64         `json:"base"`
	Top   float64         `json:"top"`
	Ring  []float64       `json:"ring"`
	Holes json.RawMessage `json:"holes"`
}

type worldJSON struct {
	Bounds struct {
		MinX float64 `json:"minX"`
		MaxX float64 `json:"maxX"`
		MinY float64 `json:"minY"`
		MaxY float64 `json:"maxY"`
	} `json:"bounds"`
	Terrain struct {
		File  string  `json:"file"`
		MinX  float64 `json:"minX"`
		MinY  float64 `json:"minY"`
		Step  float64 `json:"step"`
		NX    int     `json:"nx"`
		NY    int     `json:"ny"`
		Scale float64 `json:"scale"`
	} `json:"terrain"`
	Prisms []prismJSON `json:"prisms"`
	Trees  struct {
		Fields []string  `json:"fields"`
		Data   []float64 `json:"data"`
	} `json:"trees"`
}

type World struct {
	NX, NY                     int
	MinX, MinY, Step           float64
	BMinX, BMaxX, BMinY, BMaxY float64
	Terr                       []float32
	Surf                       []float32 // standable surface climbing from terrain
	Top                        []float32 // max prism top in cell (-1e9 none)
	Base                       []float32 // min prism base in cell
	ivOff                      []int32
	iv                         []float32
	CanopyLo, CanopyHi         []float32 // tree crown vertical extent (0,0 none)
	Nav                        *Nav
}

const noSolid = -1e9

func loadWorld(dir string) (*World, error) {
	f, err := os.ReadFile(filepath.Join(dir, "v2", "world.json"))
	if err != nil {
		return nil, err
	}
	var wj worldJSON
	if err := json.Unmarshal(f, &wj); err != nil {
		return nil, err
	}
	tf := wj.Terrain.File
	if tf == "" {
		tf = "terrain.i16"
	}
	raw, err := os.ReadFile(filepath.Join(dir, "v2", tf))
	if err != nil {
		return nil, err
	}
	w := &World{NX: wj.Terrain.NX, NY: wj.Terrain.NY, MinX: wj.Terrain.MinX, MinY: wj.Terrain.MinY, Step: wj.Terrain.Step,
		BMinX: wj.Bounds.MinX, BMaxX: wj.Bounds.MaxX, BMinY: wj.Bounds.MinY, BMaxY: wj.Bounds.MaxY}
	if w.Step <= 0 {
		w.Step = 1
	}
	scale := wj.Terrain.Scale
	if scale == 0 {
		scale = 0.01
	}
	n := w.NX * w.NY
	w.Terr = make([]float32, n)
	for i := 0; i < n && 2*i+1 < len(raw); i++ {
		w.Terr[i] = float32(float64(int16(binary.LittleEndian.Uint16(raw[2*i:]))) * scale)
	}
	// rasterize prisms
	cellIv := make(map[int32][]float32)
	for _, p := range wj.Prisms {
		if len(p.Ring) < 6 {
			continue
		}
		var holes [][]float64
		if len(p.Holes) > 0 {
			_ = json.Unmarshal(p.Holes, &holes)
		}
		minx, maxx, miny, maxy := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
		for k := 0; k+1 < len(p.Ring); k += 2 {
			minx = math.Min(minx, p.Ring[k])
			maxx = math.Max(maxx, p.Ring[k])
			miny = math.Min(miny, p.Ring[k+1])
			maxy = math.Max(maxy, p.Ring[k+1])
		}
		i0 := int(math.Floor((minx - w.MinX) / w.Step))
		i1 := int(math.Ceil((maxx - w.MinX) / w.Step))
		j0 := int(math.Floor((miny - w.MinY) / w.Step))
		j1 := int(math.Ceil((maxy - w.MinY) / w.Step))
		for j := j0; j <= j1; j++ {
			if j < 0 || j >= w.NY {
				continue
			}
			y := w.MinY + float64(j)*w.Step
			for i := i0; i <= i1; i++ {
				if i < 0 || i >= w.NX {
					continue
				}
				x := w.MinX + float64(i)*w.Step
				if !pip(p.Ring, x, y) {
					continue
				}
				inHole := false
				for _, h := range holes {
					if len(h) >= 6 && pip(h, x, y) {
						inHole = true
						break
					}
				}
				if inHole {
					continue
				}
				c := int32(j*w.NX + i)
				cellIv[c] = append(cellIv[c], float32(p.Base), float32(p.Top))
			}
		}
	}
	w.Top = make([]float32, n)
	w.Base = make([]float32, n)
	w.Surf = make([]float32, n)
	w.ivOff = make([]int32, n+1)
	total := 0
	for c := 0; c < n; c++ {
		w.ivOff[c] = int32(total)
		total += len(cellIv[int32(c)])
	}
	w.ivOff[n] = int32(total)
	w.iv = make([]float32, total)
	const clearH = 10.0 // a little more than mech height: be conservative about walking under things
	for c := 0; c < n; c++ {
		ivs := cellIv[int32(c)]
		copy(w.iv[w.ivOff[c]:], ivs)
		w.Top[c] = noSolid
		w.Base[c] = -noSolid
		s := w.Terr[c]
		if len(ivs) > 0 {
			type pr struct{ b, t float32 }
			ps := make([]pr, 0, len(ivs)/2)
			for k := 0; k+1 < len(ivs); k += 2 {
				ps = append(ps, pr{ivs[k], ivs[k+1]})
				if ivs[k+1] > w.Top[c] {
					w.Top[c] = ivs[k+1]
				}
				if ivs[k] < w.Base[c] {
					w.Base[c] = ivs[k]
				}
			}
			sort.Slice(ps, func(a, b int) bool { return ps[a].b < ps[b].b })
			for _, q := range ps {
				if q.b < s+clearH && q.t > s {
					s = q.t
				}
			}
		}
		w.Surf[c] = s
	}
	// trees: crown extent for vision blocking (dense crowns block, protocol §8)
	w.CanopyLo = make([]float32, n)
	w.CanopyHi = make([]float32, n)
	fi := map[string]int{}
	for k, name := range wj.Trees.Fields {
		fi[name] = k
	}
	nf := len(wj.Trees.Fields)
	if nf > 0 {
		ix, iy, iz, ih, icr, icb := fi["x"], fi["y"], fi["z"], fi["height"], fi["crownR"], fi["crownBase"]
		icx, okx := fi["crownX"]
		icy, oky := fi["crownY"]
		for k := 0; k+nf <= len(wj.Trees.Data); k += nf {
			d := wj.Trees.Data[k : k+nf]
			cx, cy := d[ix], d[iy]
			if okx && oky {
				cx, cy = d[icx], d[icy]
			}
			z0 := d[iz]
			lo := z0 + d[icb]
			hi := z0 + d[ih]
			r := d[icr] * 0.8 // only the dense core blocks
			if r <= 0.5 {
				continue
			}
			for jj := int((cy - r - w.MinY) / w.Step); jj <= int((cy+r-w.MinY)/w.Step)+1; jj++ {
				for ii := int((cx - r - w.MinX) / w.Step); ii <= int((cx+r-w.MinX)/w.Step)+1; ii++ {
					if ii < 0 || jj < 0 || ii >= w.NX || jj >= w.NY {
						continue
					}
					x := w.MinX + float64(ii)*w.Step
					y := w.MinY + float64(jj)*w.Step
					if (x-cx)*(x-cx)+(y-cy)*(y-cy) > r*r {
						continue
					}
					c := jj*w.NX + ii
					if w.CanopyHi[c] == 0 && w.CanopyLo[c] == 0 {
						w.CanopyLo[c] = float32(lo)
						w.CanopyHi[c] = float32(hi)
					} else {
						w.CanopyLo[c] = float32(math.Min(float64(w.CanopyLo[c]), lo))
						w.CanopyHi[c] = float32(math.Max(float64(w.CanopyHi[c]), hi))
					}
				}
			}
		}
	}
	return w, nil
}

func pip(r []float64, x, y float64) bool {
	in := false
	n := len(r) / 2
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		xi, yi := r[2*i], r[2*i+1]
		xj, yj := r[2*j], r[2*j+1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

func (w *World) cell(x, y float64) int {
	i := int(math.Floor((x-w.MinX)/w.Step + 0.5))
	j := int(math.Floor((y-w.MinY)/w.Step + 0.5))
	if i < 0 || j < 0 || i >= w.NX || j >= w.NY {
		return -1
	}
	return j*w.NX + i
}

func (w *World) TerrainAt(x, y float64) float64 {
	fx := (x - w.MinX) / w.Step
	fy := (y - w.MinY) / w.Step
	i := int(math.Floor(fx))
	j := int(math.Floor(fy))
	if i < 0 {
		i = 0
	}
	if j < 0 {
		j = 0
	}
	if i > w.NX-2 {
		i = w.NX - 2
	}
	if j > w.NY-2 {
		j = w.NY - 2
	}
	ax := fx - float64(i)
	ay := fy - float64(j)
	ax = math.Max(0, math.Min(1, ax))
	ay = math.Max(0, math.Min(1, ay))
	c := j*w.NX + i
	h00 := float64(w.Terr[c])
	h10 := float64(w.Terr[c+1])
	h01 := float64(w.Terr[c+w.NX])
	h11 := float64(w.Terr[c+w.NX+1])
	return (h00*(1-ax)+h10*ax)*(1-ay) + (h01*(1-ax)+h11*ax)*ay
}

func (w *World) SurfAt(x, y float64) float64 {
	c := w.cell(x, y)
	if c < 0 {
		return 1e9
	}
	return float64(w.Surf[c])
}

// solidAt: is point inside terrain or a prism?
func (w *World) solidAt(x, y, z float64) bool {
	c := w.cell(x, y)
	if c < 0 {
		return true
	}
	if z < float64(w.Terr[c])-0.2 {
		return true
	}
	if z > float64(w.Top[c]) || z < float64(w.Base[c]) {
		return false
	}
	for k := w.ivOff[c]; k < w.ivOff[c+1]; k += 2 {
		if z >= float64(w.iv[k]) && z <= float64(w.iv[k+1]) {
			return true
		}
	}
	return false
}

// LOS between two points for shots (buildings + terrain; trees ignored).
func (w *World) ShotLOS(a, b Vec3) bool {
	return w.los(a, b, false)
}

// SightLOS also treats dense tree crowns as blocking (vision).
func (w *World) SightLOS(a, b Vec3) bool {
	return w.los(a, b, true)
}

func (w *World) los(a, b Vec3, trees bool) bool {
	dx, dy, dz := b[0]-a[0], b[1]-a[1], b[2]-a[2]
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return true
	}
	step := 0.8
	n := int(d / step)
	canopy := 0.0
	for k := 1; k < n; k++ {
		f := float64(k) / float64(n)
		x, y, z := a[0]+dx*f, a[1]+dy*f, a[2]+dz*f
		c := w.cell(x, y)
		if c < 0 {
			return false
		}
		if z < float64(w.Terr[c])-0.3 {
			return false
		}
		if z <= float64(w.Top[c]) && z >= float64(w.Base[c]) {
			for q := w.ivOff[c]; q < w.ivOff[c+1]; q += 2 {
				if z >= float64(w.iv[q]) && z <= float64(w.iv[q+1]) {
					return false
				}
			}
		}
		if trees && w.CanopyHi[c] > 0 && z >= float64(w.CanopyLo[c]) && z <= float64(w.CanopyHi[c]) {
			canopy += step
			if canopy > 9 {
				return false
			}
		}
	}
	return true
}
