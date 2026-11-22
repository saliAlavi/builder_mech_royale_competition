package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
)

// Tree: trunk cylinder at (X,Y) radius TR from Z to Z+H; crown ellipsoid centered (CX,CY) between Z+CB and Z+H, radius CR.
type Tree struct {
	X, Y, Z, H, CR, CB, TR, CX, CY float64
}

type TreeIndex struct {
	Trees   []Tree
	x0, y0  float64
	bs      float64
	nx, ny  int
	buckets [][]int32
	Density float64
	BlockT  float64
}

func LoadTrees(dir string) *TreeIndex {
	b, err := os.ReadFile(filepath.Join(dir, "v2", "world.json"))
	if err != nil {
		return nil
	}
	var wj struct {
		Bounds struct {
			MinX, MaxX, MinY, MaxY float64
		} `json:"bounds"`
		Rules struct {
			CanopyDensity float64 `json:"canopyDensity"`
			CanopyBlockT  float64 `json:"canopyBlockT"`
		} `json:"rules"`
		Trees struct {
			Fields []string  `json:"fields"`
			Data   []float64 `json:"data"`
		} `json:"trees"`
	}
	if json.Unmarshal(b, &wj) != nil {
		return nil
	}
	idx := map[string]int{}
	for i, f := range wj.Trees.Fields {
		idx[f] = i
	}
	n := len(wj.Trees.Fields)
	if n == 0 {
		return nil
	}
	ti := &TreeIndex{x0: wj.Bounds.MinX - 20, y0: wj.Bounds.MinY - 20, bs: 10, Density: wj.Rules.CanopyDensity, BlockT: wj.Rules.CanopyBlockT}
	ti.nx = int((wj.Bounds.MaxX-ti.x0+20)/ti.bs) + 1
	ti.ny = int((wj.Bounds.MaxY-ti.y0+20)/ti.bs) + 1
	ti.buckets = make([][]int32, ti.nx*ti.ny)
	get := func(row []float64, k string) float64 {
		if i, ok := idx[k]; ok {
			return row[i]
		}
		return 0
	}
	for i := 0; i+n <= len(wj.Trees.Data); i += n {
		row := wj.Trees.Data[i : i+n]
		t := Tree{X: get(row, "x"), Y: get(row, "y"), Z: get(row, "z"), H: get(row, "height"), CR: get(row, "crownR"), CB: get(row, "crownBase"), TR: get(row, "trunkR"), CX: get(row, "crownX"), CY: get(row, "crownY")}
		if _, ok := idx["crownX"]; !ok {
			t.CX, t.CY = t.X, t.Y
		}
		id := int32(len(ti.Trees))
		ti.Trees = append(ti.Trees, t)
		r := math.Max(t.CR, t.TR) + 0.5
		minx := math.Min(t.X, t.CX) - r
		maxx := math.Max(t.X, t.CX) + r
		miny := math.Min(t.Y, t.CY) - r
		maxy := math.Max(t.Y, t.CY) + r
		for j := int((miny - ti.y0) / ti.bs); j <= int((maxy-ti.y0)/ti.bs); j++ {
			for k := int((minx - ti.x0) / ti.bs); k <= int((maxx-ti.x0)/ti.bs); k++ {
				if j >= 0 && k >= 0 && j < ti.ny && k < ti.nx {
					ti.buckets[j*ti.nx+k] = append(ti.buckets[j*ti.nx+k], id)
				}
			}
		}
	}
	return ti
}

type TreeScratch struct {
	stamp []uint32
	gen   uint32
}

// Blocks reports whether trees block sight along a->b (trunk hit, or crown chord too long).
func (ti *TreeIndex) Blocks(sc *TreeScratch, a, b Vec3) bool {
	if ti == nil {
		return false
	}
	if len(sc.stamp) != len(ti.Trees) {
		sc.stamp = make([]uint32, len(ti.Trees))
	}
	sc.gen++
	d := b.Sub(a)
	L := d.HLen()
	steps := int(L/(ti.bs*0.5)) + 1
	depth := 0.0
	maxDepth := -math.Log(math.Max(ti.BlockT, 1e-6)) / math.Max(ti.Density, 1e-6)
	lastB := -1
	for s := 0; s <= steps; s++ {
		f := float64(s) / float64(steps)
		px, py := a.X+d.X*f, a.Y+d.Y*f
		k := int((px - ti.x0) / ti.bs)
		j := int((py - ti.y0) / ti.bs)
		if k < 0 || j < 0 || k >= ti.nx || j >= ti.ny {
			continue
		}
		bi := j*ti.nx + k
		if bi == lastB {
			continue
		}
		lastB = bi
		for _, id := range ti.buckets[bi] {
			if sc.stamp[id] == sc.gen {
				continue
			}
			sc.stamp[id] = sc.gen
			t := &ti.Trees[id]
			// trunk
			if t.TR > 0 && segCyl(a, b, t.X, t.Y, t.TR, t.Z, t.Z+t.CB) >= 0 {
				return true
			}
			// crown ellipsoid chord
			hz := (t.H - t.CB) / 2
			if hz <= 0 || t.CR <= 0 {
				continue
			}
			cz := t.Z + t.CB + hz
			ox, oy, oz := (a.X-t.CX)/t.CR, (a.Y-t.CY)/t.CR, (a.Z-cz)/hz
			dx, dy, dz := d.X/t.CR, d.Y/t.CR, d.Z/hz
			A := dx*dx + dy*dy + dz*dz
			B := 2 * (ox*dx + oy*dy + oz*dz)
			C := ox*ox + oy*oy + oz*oz - 1
			disc := B*B - 4*A*C
			if disc <= 0 || A < 1e-12 {
				continue
			}
			sq := math.Sqrt(disc)
			t1 := math.Max(0, (-B-sq)/(2*A))
			t2 := math.Min(1, (-B+sq)/(2*A))
			if t2 > t1 {
				depth += (t2 - t1) * d.Len()
				if depth > maxDepth {
					return true
				}
			}
		}
	}
	return false
}
