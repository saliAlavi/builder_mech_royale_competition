package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"math"
	"os"
	"testing"
)

// loadStream returns the recorded hello and observations from the kit's conformance stream.
func loadStream(t *testing.T) (*Hello, []*Obs) {
	f, err := os.Open("kit/check/stream.jsonl.gz")
	if err != nil {
		t.Skip("no stream")
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(gz)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var h Hello
	var obs []*Obs
	for sc.Scan() {
		var head struct {
			Type string `json:"type"`
		}
		json.Unmarshal(sc.Bytes(), &head)
		switch head.Type {
		case "hello":
			json.Unmarshal(sc.Bytes(), &h)
		case "obs":
			o := &Obs{}
			json.Unmarshal(sc.Bytes(), o)
			obs = append(obs, o)
		}
	}
	h.Map.Dir = "kit/arena"
	return &h, obs
}

// A mortar shell about to land on us must trigger a dodge away from the impact point.
func TestMortarDodge(t *testing.T) {
	h, obs := loadStream(t)
	b := NewBot(h)
	o := obs[10]
	me := o.Self.Pos
	// shell 1.5 s from landing ~2 m east of us: start above, falling ballistic
	g := 22.0
	tf := 1.5
	land := Vec3{me[0] + 2, me[1], b.w.SurfAt(me[0]+2, me[1])}
	vx, vy := -10.0, 0.0
	start := Vec3{land[0] - vx*tf, land[1] - vy*tf, 0}
	vz0 := 5.0
	start[2] = land[2] - vz0*tf + 0.5*g*tf*tf
	o.Projectiles = []Proj{{ID: 1, Kind: "mortar", Pos: start, Vel: Vec3{vx, vy, vz0}}}
	o.Mechs = nil
	a := b.Act(o)
	if b.mode != "dodge" {
		t.Fatalf("mode %q, want dodge", b.mode)
	}
	// should move away from the impact (westward, negative x component)
	if a.Move[0] > 0.2 {
		t.Fatalf("move %v not away from impact east of us", a.Move)
	}
	if math.IsNaN(a.Move[0]) || math.IsNaN(a.Aim[0]) {
		t.Fatal("NaN in action")
	}
}

// Every recorded observation must produce a finite action quickly, without panicking.
func TestStreamActs(t *testing.T) {
	h, obs := loadStream(t)
	b := NewBot(h)
	for _, o := range obs {
		a := b.Act(o)
		for _, v := range []float64{a.Move[0], a.Move[1], a.Aim[0], a.Aim[1]} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("non-finite action at tick %v: %+v", o.Tick, a)
			}
		}
	}
}
