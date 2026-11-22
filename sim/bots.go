package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type lineT struct {
	b  []byte
	at time.Time
}

type ExtBot struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan lineT
	logf   *os.File
	dead   bool
	Name   string
	BadLns int
	maxLen int
}

func StartBot(cmdline, logPath string, maxLine int) (*ExtBot, error) {
	lf, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("/bin/sh", "-c", cmdline)
	cmd.Stderr = lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		lf.Close()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		lf.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		lf.Close()
		return nil, err
	}
	b := &ExtBot{cmd: cmd, stdin: in, lines: make(chan lineT, 256), logf: lf, maxLen: maxLine}
	go func() {
		rd := bufio.NewReaderSize(out, 1<<17)
		for {
			line, err := rd.ReadBytes('\n')
			if len(line) > 0 {
				cp := make([]byte, len(line))
				copy(cp, line)
				b.lines <- lineT{cp, time.Now()}
			}
			if err != nil {
				close(b.lines)
				return
			}
		}
	}()
	return b, nil
}

func (b *ExtBot) Send(v any) {
	if b.dead {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	data = append(data, '\n')
	if _, err := b.stdin.Write(data); err != nil {
		b.dead = true
	}
}

func (b *ExtBot) SendRaw(data []byte) {
	if b.dead {
		return
	}
	if _, err := b.stdin.Write(data); err != nil {
		b.dead = true
	}
}

// WaitReady waits for {"type":"ready"}.
func (b *ExtBot) WaitReady(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case ln, ok := <-b.lines:
			if !ok {
				b.dead = true
				return false
			}
			line := ln.b
			var r struct {
				Type string `json:"type"`
				Name string `json:"name"`
			}
			if json.Unmarshal(line, &r) == nil && r.Type == "ready" {
				n := r.Name
				if len(n) > 32 {
					n = n[:32]
				}
				b.Name = n
				return true
			}
		case <-timer.C:
			return false
		}
	}
}

// Wait returns the action for tick, the latency, or nil on timeout.
func (b *ExtBot) Wait(tick int, start time.Time, deadline time.Time) (*ActionMsg, time.Duration) {
	if b.dead {
		return nil, 0
	}
	for {
		rem := time.Until(deadline)
		if rem <= 0 {
			// still accept lines that arrived before the deadline
			rem = time.Microsecond
		}
		timer := time.NewTimer(rem)
		select {
		case ln, ok := <-b.lines:
			timer.Stop()
			if !ok {
				b.dead = true
				return nil, 0
			}
			line := ln.b
			lat := ln.at.Sub(start)
			if ln.at.After(deadline) {
				return nil, 0
			}
			if len(line) > b.maxLen {
				b.BadLns++
				continue
			}
			var a ActionMsg
			if err := json.Unmarshal(line, &a); err != nil {
				b.BadLns++
				continue
			}
			if a.Tick == nil || *a.Tick != tick {
				continue
			}
			return &a, lat
		case <-timer.C:
			return nil, 0
		}
	}
}

func (b *ExtBot) Close() {
	if b.stdin != nil {
		b.stdin.Close()
	}
	done := make(chan struct{})
	go func() {
		// drain remaining output
		for range b.lines {
		}
		close(done)
	}()
	ch := make(chan error, 1)
	go func() { ch <- b.cmd.Wait() }()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		syscall.Kill(-b.cmd.Process.Pid, syscall.SIGKILL)
		<-ch
	}
	syscall.Kill(-b.cmd.Process.Pid, syscall.SIGKILL)
	<-done
	b.logf.Close()
}
