package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"time"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ";") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

type Opts struct {
	MapDir    string
	RawCfg    json.RawMessage
	Cfg       *Config
	Mode      string
	Bots      []string
	Fill      int
	Matches   int
	Jobs      int
	Seed      uint64
	Out       string
	Replay    bool
	TimeoutMs int
	LateMs    int
	Verbose   bool
}

type SlotResult struct {
	Slot        int          `json:"slot"`
	Cmd         string       `json:"cmd"`
	CmdIdx      int          `json:"cmd_idx"`
	Name        string       `json:"name"`
	Difficulty  string       `json:"difficulty,omitempty"`
	Place       int          `json:"place"`
	Score       int          `json:"score"`
	Kills       int          `json:"kills"`
	SurvivalS   float64      `json:"survival_s"`
	Cause       string       `json:"cause"`
	ZoneDamage  float64      `json:"zone_damage"`
	DmgDealt    float64      `json:"damage_dealt"`
	DmgTaken    float64      `json:"damage_taken"`
	Cubes       int          `json:"cubes"`
	Upgrades    []string     `json:"upgrades"`
	Weapons     []string     `json:"weapons"`
	Answers     int          `json:"answers"`
	Late        int          `json:"late"`
	Missing     int          `json:"missing"`
	BadLines    int          `json:"bad_lines"`
	MeanRTms    float64      `json:"mean_rt_ms"`
	MaxRTms     float64      `json:"max_rt_ms"`
	StuckSec    float64      `json:"stuck_s"`
	StuckSpots  [][2]float64 `json:"stuck_spots"`
	FinalPos    [3]float64   `json:"final_pos"`
	InsideTicks int          `json:"inside_geometry_ticks"`
}

type MatchResult struct {
	Idx      int          `json:"match"`
	Seed     uint64       `json:"seed"`
	EndT     float64      `json:"end_t"`
	WallS    float64      `json:"wall_s"`
	Slots    []SlotResult `json:"slots"`
	Error    string       `json:"error,omitempty"`
	External []int        `json:"external_slots"`
}

func main() {
	var o Opts
	var bots multiFlag
	mapDir := flag.String("map", "../kit/arena", "map directory (contains v2/world.json)")
	cfgFrom := flag.String("config-from", "../kit/check/stream.jsonl.gz", "file whose first line is a hello message (jsonl or .gz)")
	flag.StringVar(&o.Mode, "mode", "practice", "practice | ffa")
	flag.Var(&bots, "bot", "external bot command (repeatable)")
	flag.IntVar(&o.Fill, "fill", 16, "ffa: number of slots filled with external commands (round-robin); the rest are house bots")
	flag.IntVar(&o.Matches, "matches", 10, "number of matches")
	flag.IntVar(&o.Jobs, "jobs", 4, "parallel matches")
	seed := flag.Uint64("seed", 1, "base seed")
	flag.StringVar(&o.Out, "out", "simout", "output directory")
	flag.BoolVar(&o.Replay, "replay", false, "write replay_match<N>.jsonl")
	flag.IntVar(&o.TimeoutMs, "timeout-ms", 200, "answer timeout per decision")
	flag.IntVar(&o.LateMs, "late-ms", 50, "answers slower than this count as late")
	flag.BoolVar(&o.Verbose, "v", false, "print per-match lines")
	cpuprof := flag.String("cpuprofile", "", "write cpu profile")
	noiseMult := flag.Float64("house-noise", 1.2, "multiplier on house-bot aim noise (higher = weaker house bots)")
	flag.IntVar(&HouseHunters, "house-hunters", HouseHunters, "house bots that converge on a target known only via squad vision")
	flag.Parse()
	for i := range HouseNoise {
		HouseNoise[i] *= *noiseMult
	}
	if *cpuprof != "" {
		f, _ := os.Create(*cpuprof)
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	o.Seed = *seed
	o.Bots = bots
	if len(o.Bots) == 0 {
		fmt.Fprintln(os.Stderr, "need at least one --bot")
		os.Exit(2)
	}
	absMap, _ := filepath.Abs(*mapDir)
	o.MapDir = absMap
	raw, cfg, err := LoadConfig(*cfgFrom)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	o.RawCfg, o.Cfg = raw, cfg
	t0 := time.Now()
	w, err := LoadWorld(absMap)
	if err != nil {
		fmt.Fprintln(os.Stderr, "world:", err)
		os.Exit(1)
	}
	nav := BuildNav(w)
	fmt.Fprintf(os.Stderr, "world loaded in %.2fs (%d prism intervals, %d open cells, %d roof cells)\n", time.Since(t0).Seconds(), len(w.ivs), len(w.openCells), len(w.roofCells))
	os.MkdirAll(filepath.Join(o.Out, "logs"), 0o755)

	results := make([]*MatchResult, o.Matches)
	var wg sync.WaitGroup
	ch := make(chan int)
	var mu sync.Mutex
	done := 0
	tStart := time.Now()
	for j := 0; j < o.Jobs; j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range ch {
				r := runMatch(&o, w, nav, idx)
				mu.Lock()
				results[idx] = r
				done++
				if o.Verbose || true {
					var parts []string
					for _, sr := range r.Slots {
						if sr.CmdIdx >= 0 {
							parts = append(parts, fmt.Sprintf("slot%d[%d] place %d kills %d t=%.0f cause=%s", sr.Slot, sr.CmdIdx, sr.Place, sr.Kills, sr.SurvivalS, sr.Cause))
						}
					}
					if len(parts) > 4 {
						parts = append(parts[:4], "...")
					}
					fmt.Fprintf(os.Stderr, "[%d/%d] match %d (%.1fs wall, end t=%.0f) %s %s\n", done, o.Matches, idx, r.WallS, r.EndT, strings.Join(parts, "; "), r.Error)
				}
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < o.Matches; i++ {
		ch <- i
	}
	close(ch)
	wg.Wait()
	wall := time.Since(tStart).Seconds()
	rb, _ := json.MarshalIndent(results, "", " ")
	os.WriteFile(filepath.Join(o.Out, "results.json"), rb, 0o644)
	summarize(&o, results, wall)
}

func runMatch(o *Opts, w *World, nav *NavGrid, idx int) (res *MatchResult) {
	t0 := time.Now()
	seed := o.Seed*1000003 + uint64(idx)*7919 + 17
	res = &MatchResult{Idx: idx, Seed: seed}
	c := o.Cfg
	s := NewSim(w, nav, c, o.Mode, seed)
	n := c.Match.Mechs
	s.Setup(n)
	rng := rand.New(rand.NewPCG(seed^0xabcdef, 99))
	// assign slots
	cmdOf := make([]int, n)
	for i := range cmdOf {
		cmdOf[i] = -1
	}
	if o.Mode == "practice" {
		ext := rng.IntN(n)
		cmdOf[ext] = idx % len(o.Bots)
		diffs := []int{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3}
		rng.Shuffle(len(diffs), func(i, j int) { diffs[i], diffs[j] = diffs[j], diffs[i] })
		k := 0
		for i := 0; i < n; i++ {
			if cmdOf[i] < 0 {
				m := s.Mechs[i]
				m.House = NewHouse(diffs[k%len(diffs)])
				m.Squad = 1
				m.Name = "house-" + diffNames[m.House.Diff]
				k++
			}
		}
	} else {
		perm := rng.Perm(n)
		fill := o.Fill
		if fill > n {
			fill = n
		}
		for k := 0; k < fill; k++ {
			cmdOf[perm[k]] = (k + idx) % len(o.Bots)
		}
		for i := 0; i < n; i++ {
			if cmdOf[i] < 0 {
				m := s.Mechs[i]
				m.House = NewHouse(rng.IntN(4))
				m.Squad = 1
				m.Name = "house-" + diffNames[m.House.Diff]
			}
		}
	}
	// start external bots
	var exts []*Mech
	for i := 0; i < n; i++ {
		if cmdOf[i] < 0 {
			continue
		}
		m := s.Mechs[i]
		m.ExtIdx = cmdOf[i]
		lp := filepath.Join(o.Out, "logs", fmt.Sprintf("match%d_slot%d.log", idx, i))
		b, err := StartBot(o.Bots[cmdOf[i]], lp, c.Bot.MaxLineBytes)
		if err != nil {
			res.Error = "start: " + err.Error()
			continue
		}
		m.Ext = b
		exts = append(exts, m)
		res.External = append(res.External, i)
	}
	defer func() {
		for _, m := range exts {
			m.Ext.Close()
		}
	}()
	for _, m := range exts {
		hello := struct {
			Type     string          `json:"type"`
			Protocol string          `json:"protocol"`
			Slot     int             `json:"slot"`
			Seed     uint64          `json:"seed"`
			Map      any             `json:"map"`
			Config   json.RawMessage `json:"config"`
		}{"hello", "mechfight/1", m.ID, uint64(rng.Uint32()), map[string]any{"dir": o.MapDir, "files": []string{"v2/world.json", "v2/terrain.i16"}}, o.RawCfg}
		m.Ext.Send(hello)
	}
	var wg sync.WaitGroup
	for _, m := range exts {
		wg.Add(1)
		go func(m *Mech) {
			defer wg.Done()
			if !m.Ext.WaitReady(time.Duration(c.Bot.HelloBudgetMs) * time.Millisecond) {
				fmt.Fprintf(os.Stderr, "match %d slot %d: no ready\n", idx, m.ID)
			}
			m.Name = m.Ext.Name
		}(m)
	}
	wg.Wait()

	var rep *bufio.Writer
	if o.Replay {
		f, err := os.Create(filepath.Join(o.Out, fmt.Sprintf("replay_match%d.jsonl", idx)))
		if err == nil {
			defer f.Close()
			rep = bufio.NewWriter(f)
			defer rep.Flush()
			meta := map[string]any{"type": "meta", "match": idx, "seed": seed, "external": res.External, "mode": o.Mode}
			names := map[int]string{}
			for _, m := range s.Mechs {
				names[m.ID] = m.Name
			}
			meta["names"] = names
			b, _ := json.Marshal(meta)
			rep.Write(append(b, '\n'))
		}
	}

	timeout := time.Duration(o.TimeoutMs) * time.Millisecond
	late := float64(o.LateMs)
	decideEvery := c.Match.DecideEvery
	maxTicks := int(c.Match.DurationS * float64(c.Match.TickHz))
	for s.Tick < maxTicks {
		if s.Tick%decideEvery == 0 {
			s.squadPass()
			type sent struct {
				m     *Mech
				start time.Time
			}
			var sends []sent
			for _, m := range exts {
				if !m.Alive {
					continue
				}
				ob := s.buildObs(m)
				data, _ := json.Marshal(ob)
				data = append(data, '\n')
				st := time.Now()
				m.Ext.SendRaw(data)
				sends = append(sends, sent{m, st})
			}
			for _, m := range s.Mechs {
				if m.Alive && m.House != nil {
					s.houseDecide(m)
				}
			}
			s.Sounds = s.Sounds[:0]
			s.Beams = s.Beams[:0]
			for _, sd := range sends {
				m := sd.m
				a, lat := m.Ext.Wait(s.Tick, sd.start, sd.start.Add(timeout))
				if a != nil {
					ms := float64(lat.Microseconds()) / 1000
					m.Answers++
					m.SumRTms += ms
					if ms > m.MaxRTms {
						m.MaxRTms = ms
					}
					if ms > late {
						m.Late++
					}
					s.applyAction(m, a)
				} else {
					m.Missing++
					s.coast(m)
				}
			}
			s.oneShots()
		}
		if rep != nil && s.Tick%10 == 0 {
			writeFrame(rep, s)
		}
		s.step()
		// one-shot jump only on the first tick of a decision
		for _, m := range s.Mechs {
			m.cmd.Jump = false
		}
		if len(exts) > 0 {
			anyExt := false
			for _, m := range exts {
				if m.Alive {
					anyExt = true
				}
			}
			if !anyExt {
				break
			}
		}
		if s.aliveCount() <= 1 {
			break
		}
	}
	if rep != nil {
		writeFrame(rep, s)
	}
	s.finalize()
	res.EndT = s.T
	for _, m := range exts {
		m.Ext.Send(map[string]any{"type": "end", "place": m.Place, "kills": m.Kills})
	}
	for _, m := range s.Mechs {
		sr := SlotResult{Slot: m.ID, CmdIdx: m.ExtIdx, Name: m.Name, Place: m.Place, Score: c.Match.Mechs - m.Place, Kills: m.Kills,
			SurvivalS: r2(m.DeathT), Cause: m.Cause, ZoneDamage: r1(m.ZoneDamage), DmgDealt: r1(m.DmgDealt), DmgTaken: r1(m.DmgTaken),
			Cubes: m.Cubes, Upgrades: m.Upgrades, Answers: m.Answers, Late: m.Late, Missing: m.Missing, StuckSec: r2(m.StuckSec), StuckSpots: m.StuckSpots, FinalPos: v3r(m.Pos), MaxRTms: r2(m.MaxRTms), InsideTicks: m.InsideTicks}
		if m.Alive {
			sr.Cause = "survived"
		}
		if m.Answers > 0 {
			sr.MeanRTms = r2(m.SumRTms / float64(m.Answers))
		}
		if m.ExtIdx >= 0 {
			sr.Cmd = o.Bots[m.ExtIdx]
			if m.Ext != nil {
				sr.BadLines = m.Ext.BadLns
			}
		}
		if m.House != nil {
			sr.Difficulty = diffNames[m.House.Diff]
		}
		for _, w := range m.Weapons {
			if w != nil {
				sr.Weapons = append(sr.Weapons, w.Kind)
			}
		}
		res.Slots = append(res.Slots, sr)
	}
	res.WallS = r2(time.Since(t0).Seconds())
	return res
}

func writeFrame(w *bufio.Writer, s *Sim) {
	z := s.zone(s.T)
	type mf struct {
		ID      int        `json:"id"`
		Pos     [3]float64 `json:"pos"`
		HP      float64    `json:"hp"`
		Alive   bool       `json:"alive"`
		Weapons []string   `json:"weapons"`
		Ext     bool       `json:"ext,omitempty"`
	}
	fr := struct {
		T     float64        `json:"t"`
		Zone  map[string]any `json:"zone"`
		Mechs []mf           `json:"mechs"`
	}{T: r2(s.T), Zone: map[string]any{"center": [2]float64{r2(z.Center[0]), r2(z.Center[1])}, "radius": r2(z.R)}}
	for _, m := range s.Mechs {
		ws := []string{}
		for _, wp := range m.Weapons {
			if wp != nil {
				ws = append(ws, wp.Kind)
			}
		}
		fr.Mechs = append(fr.Mechs, mf{m.ID, v3r(m.Pos), r1(m.HP), m.Alive, ws, m.Ext != nil})
	}
	b, _ := json.Marshal(fr)
	w.Write(append(b, '\n'))
}

func summarize(o *Opts, res []*MatchResult, wall float64) {
	type agg struct {
		n                                               int
		pts, surv, kills, zone, stuck, dealt, rt, maxrt float64
		late, missing, answers, bad                     int
		causes                                          map[string]int
		places                                          [17]int
		stuckMatches                                    int
	}
	aggs := make([]*agg, len(o.Bots))
	for i := range aggs {
		aggs[i] = &agg{causes: map[string]int{}}
	}
	house := map[string][2]float64{}
	wallSum := 0.0
	for _, r := range res {
		if r == nil {
			continue
		}
		wallSum += r.WallS
		for _, sr := range r.Slots {
			if sr.CmdIdx < 0 {
				h := house[sr.Difficulty]
				h[0] += float64(sr.Kills)
				h[1]++
				house[sr.Difficulty] = h
				continue
			}
			a := aggs[sr.CmdIdx]
			a.n++
			a.pts += float64(sr.Score)
			a.surv += sr.SurvivalS
			a.kills += float64(sr.Kills)
			a.zone += sr.ZoneDamage
			a.stuck += sr.StuckSec
			a.dealt += sr.DmgDealt
			a.late += sr.Late
			a.missing += sr.Missing
			a.answers += sr.Answers
			a.bad += sr.BadLines
			a.rt += sr.MeanRTms * float64(sr.Answers)
			a.maxrt = math.Max(a.maxrt, sr.MaxRTms)
			a.causes[sr.Cause]++
			if sr.Place >= 1 && sr.Place <= 16 {
				a.places[sr.Place]++
			}
			if sr.StuckSec > 0 {
				a.stuckMatches++
			}
		}
	}
	fmt.Printf("\n=== %s: %d matches, %.1fs wall total (%.2fs/match avg wall per match, %d jobs) ===\n", o.Mode, len(res), wall, wallSum/float64(len(res)), o.Jobs)
	for i, a := range aggs {
		if a.n == 0 {
			continue
		}
		N := float64(a.n)
		fmt.Printf("\n[%d] %s\n", i, o.Bots[i])
		fmt.Printf("  slots played     %d\n", a.n)
		fmt.Printf("  mean points      %.2f  (16 - place)\n", a.pts/N)
		var ph []string
		for p := 1; p <= 16; p++ {
			if a.places[p] > 0 {
				ph = append(ph, fmt.Sprintf("%d:%d", p, a.places[p]))
			}
		}
		fmt.Printf("  place histogram  %s\n", strings.Join(ph, " "))
		fmt.Printf("  mean survival    %.1f s\n", a.surv/N)
		fmt.Printf("  mean kills       %.2f\n", a.kills/N)
		fmt.Printf("  mean dmg dealt   %.0f\n", a.dealt/N)
		fmt.Printf("  mean zone dmg    %.1f\n", a.zone/N)
		var cs []string
		for _, k := range []string{"zone", "gun", "laser", "lightning", "mortar", "npc", "survived"} {
			if a.causes[k] > 0 {
				cs = append(cs, fmt.Sprintf("%s=%d", k, a.causes[k]))
			}
		}
		for k, v := range a.causes {
			found := false
			for _, kk := range []string{"zone", "gun", "laser", "lightning", "mortar", "npc", "survived"} {
				if k == kk {
					found = true
				}
			}
			if !found {
				cs = append(cs, fmt.Sprintf("%s=%d", k, v))
			}
		}
		sort.Strings(cs)
		fmt.Printf("  death causes     %s\n", strings.Join(cs, " "))
		fmt.Printf("  answers          %d  late(>%dms)=%d  missing=%d  bad_lines=%d  mean_rt=%.2fms max_rt=%.1fms\n", a.answers, o.LateMs, a.late, a.missing, a.bad, a.rt/math.Max(1, float64(a.answers)), a.maxrt)
		fmt.Printf("  stuck seconds    %.1f total, %.1f mean/match, in %d matches\n", a.stuck, a.stuck/N, a.stuckMatches)
	}
	if len(house) > 0 {
		var ks []string
		for k, v := range house {
			ks = append(ks, fmt.Sprintf("%s=%.2f", k, v[0]/math.Max(1, v[1])))
		}
		sort.Strings(ks)
		fmt.Printf("\nhouse kills/slot: %s\n", strings.Join(ks, " "))
	}
	fmt.Printf("results: %s\n", filepath.Join(o.Out, "results.json"))
}
