package main

// mechfight/1 bot: stdin/stdout JSON-lines loop. Logs go to stderr only.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

const botName = "ohmech"

func logf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

var out = bufio.NewWriterSize(os.Stdout, 1<<16)

func sendRaw(b []byte) {
	out.Write(b)
	out.WriteByte('\n')
	out.Flush()
}

func finite(v float64, def float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return def
	}
	return v
}

func sanitize(a *Action) {
	a.Move[0] = finite(a.Move[0], 0)
	a.Move[1] = finite(a.Move[1], 0)
	l := math.Hypot(a.Move[0], a.Move[1])
	if l > 1 {
		a.Move[0] /= l
		a.Move[1] /= l
	}
	a.Aim[0] = finite(a.Aim[0], 0)
	a.Aim[1] = finite(a.Aim[1], 0)
}

func sendAction(a *Action) {
	sanitize(a)
	b, err := json.Marshal(a)
	if err != nil {
		b = []byte(fmt.Sprintf(`{"tick":%v}`, a.Tick))
	}
	sendRaw(b)
}

func main() {
	if devMain(os.Args[1:]) {
		return
	}
	// The match container has 1 CPU (CFS quota) and a 128-thread limit, but the runtime would size
	// itself to the host's cores: pin it to one P so GC can't get throttled or spawn too many threads.
	runtime.GOMAXPROCS(1)
	debug.SetGCPercent(400)
	lines := make(chan []byte, 1024)
	go func() {
		r := bufio.NewReaderSize(os.Stdin, 1<<20)
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 1 {
				lines <- line
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()

	var bot *Bot
	var last Action
	errors := 0
	for {
		first, ok := <-lines
		if !ok {
			return
		}
		batch := [][]byte{first}
		closed := false
	drain:
		for {
			select {
			case l, ok2 := <-lines:
				if !ok2 {
					closed = true
					break drain
				}
				batch = append(batch, l)
			default:
				break drain
			}
		}
		var latest []byte
		for _, raw := range batch {
			var head struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(raw, &head); err != nil {
				continue
			}
			switch head.Type {
			case "hello":
				var h Hello
				if err := json.Unmarshal(raw, &h); err != nil {
					logf("bad hello: %v", err)
				}
				t0 := time.Now()
				func() {
					defer func() {
						if r := recover(); r != nil {
							logf("init panic: %v\n%s", r, debug.Stack())
						}
					}()
					bot = NewBot(&h)
				}()
				logf("init took %v", time.Since(t0))
				sendRaw([]byte(fmt.Sprintf(`{"type":"ready","name":%q}`, botName)))
			case "obs":
				latest = raw
			case "end":
				return
			}
		}
		if latest != nil {
			var o Obs
			if err := json.Unmarshal(latest, &o); err != nil {
				logf("bad obs: %v", err)
				continue
			}
			var act *Action
			func() {
				defer func() {
					if r := recover(); r != nil {
						errors++
						if errors <= 5 || errors%100 == 0 {
							logf("act panic at tick %v (%d): %v\n%s", o.Tick, errors, r, debug.Stack())
						}
						act = nil
					}
				}()
				if bot != nil {
					ts := time.Now()
					act = bot.Act(&o)
					if el := time.Since(ts); el > 15*time.Millisecond {
						logf("slow act tick %v: %v mode=%s", o.Tick, el, bot.mode)
					}
				}
			}()
			if act == nil {
				fb := last
				fb.Fire = [2]bool{}
				fb.Jump = false
				fb.Reload = false
				fb.Pick, fb.Take, fb.Drop = nil, nil, nil
				act = &fb
			}
			act.Tick = o.Tick
			sendAction(act)
			last = *act
		}
		if closed {
			return
		}
	}
}
