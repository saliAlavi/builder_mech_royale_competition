package main

// Developer tools (not used in matches): `mechbot -mapstats <arenaDir> <out.png>`.

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"time"
)

var devHooks []func([]string) bool

func devMain(args []string) bool {
	for _, h := range devHooks {
		if h(args) {
			return true
		}
	}
	if len(args) >= 3 && args[0] == "-mapstats" {
		t0 := time.Now()
		w, err := loadWorld(args[1])
		if err != nil {
			logf("load: %v", err)
			return true
		}
		t1 := time.Now()
		w.Nav = buildNav(w)
		t2 := time.Now()
		n := w.Nav
		ok, main := 0, 0
		for i := range n.OK {
			if n.OK[i] {
				ok++
			}
			if n.InMain(i) {
				main++
			}
		}
		logf("load %v nav %v nodes %d ok %d main %d", t1.Sub(t0), t2.Sub(t1), len(n.OK), ok, main)
		img := image.NewRGBA(image.Rect(0, 0, n.NU, n.NV))
		for v := 0; v < n.NV; v++ {
			for u := 0; u < n.NU; u++ {
				id := v*n.NU + u
				h := n.H[id]
				g := uint8(80 + clampf(float64(h)*3, -60, 120))
				var c color.RGBA
				switch {
				case n.InMain(id):
					c = color.RGBA{g / 2, g, g / 2, 255}
				case n.OK[id]:
					c = color.RGBA{g, g / 2, g, 255}
				default:
					c = color.RGBA{g / 3, g / 3, g / 3, 255}
				}
				img.Set(u, n.NV-1-v, c)
			}
		}
		f, _ := os.Create(args[2])
		png.Encode(f, img)
		f.Close()
		return true
	}
	return false
}

func clampf(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func init() {
	devHooks = append(devHooks, func(args []string) bool {
		if len(args) < 2 || args[0] != "-astar" {
			return false
		}
		w, _ := loadWorld(args[1])
		w.Nav = buildNav(w)
		n := w.Nav
		pairs := [][4]float64{{-450, -300, 520, 350}, {-300, 0, 400, 0}, {0, -300, 0, 350}, {-470, 300, -290, 100}}
		for _, p := range pairs {
			s := n.nearestOK(p[0], p[1], 0, 20, true)
			g := n.nearestOK(p[2], p[3], 30, 20, false)
			t0 := time.Now()
			path := n.FindPath(s, g, 160000, nil)
			logf("path %v -> %v: len %d in %v (goal main=%v) s=%d g=%d", p[:2], p[2:], len(path), time.Since(t0), n.InMain(g), s, g)
		}
		return true
	})
}
