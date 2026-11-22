package main

import (
	"math"
)

func (s *Sim) slewAim(m *Mech, dt float64) {
	tr := m.MS["turn_rate"] * dt
	d := wrapAngle(m.cmd.Yaw - m.Yaw)
	if math.Abs(d) <= tr {
		m.Yaw = wrapAngle(m.cmd.Yaw)
	} else if d > 0 {
		m.Yaw = wrapAngle(m.Yaw + tr)
	} else {
		m.Yaw = wrapAngle(m.Yaw - tr)
	}
	tp := m.cmd.Pitch
	if tp < m.MS["min_pitch"] {
		tp = m.MS["min_pitch"]
	}
	if tp > m.MS["max_pitch"] {
		tp = m.MS["max_pitch"]
	}
	pr := m.MS["pitch_rate"] * dt
	dp := tp - m.Pitch
	if math.Abs(dp) <= pr {
		m.Pitch = tp
	} else if dp > 0 {
		m.Pitch += pr
	} else {
		m.Pitch -= pr
	}
}

type probeRes struct {
	blocked bool
	floor   float64
	ceil    float64
	nx, ny  float64 // push direction away from blocking cells
}

// probe tests a cylinder (feet z) at (x,y). stepAllow: how far above feet a top may be and still count as floor.
func (s *Sim) probe(x, y, z, r, h, stepAllow float64) probeRes {
	w := s.W
	res := probeRes{floor: math.Inf(-1), ceil: math.Inf(1)}
	if !w.InBounds(x, y, r) {
		res.blocked = true
		// push toward inside
		if x < w.MinX+r {
			res.nx = 1
		} else if x > w.MaxX-r {
			res.nx = -1
		}
		if y < w.MinY+r {
			res.ny = 1
		} else if y > w.MaxY-r {
			res.ny = -1
		}
		return res
	}
	g := w.Ground(x, y)
	res.floor = g
	if g > z+stepAllow {
		res.blocked = true
	}
	rr := r - 0.1 // conservative rasterization already fattens prisms ~0.5 m
	i0 := int(math.Floor(x - rr - w.gx0))
	i1 := int(math.Floor(x + rr - w.gx0))
	j0 := int(math.Floor(y - rr - w.gy0))
	j1 := int(math.Floor(y + rr - w.gy0))
	type blk struct{ cx, cy float64 }
	var blocks [32]blk
	nb := 0
	for j := j0; j <= j1; j++ {
		for i := i0; i <= i1; i++ {
			ivs := w.cellByIJ(i, j)
			if len(ivs) == 0 {
				continue
			}
			cx := w.gx0 + float64(i) + 0.5
			cy := w.gy0 + float64(j) + 0.5
			dx, dy := cx-x, cy-y
			if dx*dx+dy*dy > rr*rr {
				continue
			}
			cellBlocked := false
			for _, iv := range ivs {
				T, B := float64(iv.T), float64(iv.B)
				if T <= z+stepAllow {
					if T > res.floor {
						res.floor = T
					}
				} else if B >= z+h-0.05 {
					if B < res.ceil {
						res.ceil = B
					}
				} else {
					cellBlocked = true
				}
			}
			if cellBlocked {
				res.blocked = true
				if nb < len(blocks) {
					blocks[nb] = blk{cx, cy}
					nb++
				}
			}
		}
	}
	if !res.blocked && res.floor > z && res.floor+h > res.ceil {
		res.blocked = true
	}
	if nb > 0 {
		sx, sy := 0.0, 0.0
		for k := 0; k < nb; k++ {
			dx, dy := x-blocks[k].cx, y-blocks[k].cy
			l := math.Hypot(dx, dy) + 1e-6
			sx += dx / l
			sy += dy / l
		}
		l := math.Hypot(sx, sy)
		if l > 1e-6 {
			res.nx, res.ny = sx/l, sy/l
		}
	}
	return res
}

func (s *Sim) moveMech(m *Mech, dt float64) {
	ms := m.MS
	r := m.radius()
	h := m.height()
	mv := m.cmd.Move
	ml := math.Hypot(mv[0], mv[1])
	if ml > 1 {
		mv[0] /= ml
		mv[1] /= ml
		ml = 1
	}
	speed := ms["walk_speed"]
	m.boosting = false
	m.jetting = false
	if m.cmd.Boost && m.Grounded && ml > 0.05 && m.Energy > 0 {
		speed = ms["boost_speed"]
		m.Energy = math.Max(0, m.Energy-ms["boost_drain"]*dt)
		m.lastSpendT = s.T
		m.boosting = true
	}
	if m.charging() {
		speed *= ms["charge_move_mult"]
	}
	tvx, tvy := mv[0]*speed, mv[1]*speed
	acc := ms["ground_accel"]
	if !m.Grounded {
		acc = ms["air_accel"]
	}
	dvx, dvy := tvx-m.Vel.X, tvy-m.Vel.Y
	dl := math.Hypot(dvx, dvy)
	maxdv := acc * dt
	if dl > maxdv {
		dvx, dvy = dvx/dl*maxdv, dvy/dl*maxdv
	}
	if m.Grounded || ml > 0.05 || true {
		m.Vel.X += dvx
		m.Vel.Y += dvy
	}
	// vertical
	if m.cmd.Jump && m.Grounded {
		m.Vel.Z = ms["jump_speed"]
		m.Grounded = false
	}
	if !m.Grounded && m.cmd.Boost && m.Energy > 0 {
		if m.Vel.Z < ms["max_rise_speed"] {
			m.Vel.Z = math.Min(m.Vel.Z+ms["jet_accel"]*dt, ms["max_rise_speed"])
		}
		m.Energy = math.Max(0, m.Energy-ms["jet_drain"]*dt)
		m.lastSpendT = s.T
		m.jetting = true
	}
	if !m.Grounded {
		m.Vel.Z -= ms["gravity"] * dt
	}

	// horizontal move with sliding
	step := ms["step_up"]
	if !m.Grounded {
		step = 0.05
	}
	slopeTan := math.Tan(ms["max_slope_deg"] * math.Pi / 180)
	dx, dy := m.Vel.X*dt, m.Vel.Y*dt
	cur := m.Pos
	tryMove := func(dx, dy float64) (bool, float64, probeRes) {
		nx, ny := cur.X+dx, cur.Y+dy
		pr := s.probe(nx, ny, cur.Z, r, h, step)
		if pr.blocked {
			return false, 0, pr
		}
		// terrain slope check (walking uphill on terrain)
		g0 := s.W.Ground(cur.X, cur.Y)
		g1 := s.W.Ground(nx, ny)
		if g1 > cur.Z+0.02 && g1-g0 > slopeTan*math.Hypot(dx, dy)+0.02 && g1 >= pr.floor-0.01 {
			return false, 0, pr
		}
		nz := cur.Z
		if m.Grounded && pr.floor > nz {
			nz = pr.floor
		}
		return true, nz, pr
	}
	if dx != 0 || dy != 0 {
		ok, nz, pr := tryMove(dx, dy)
		if ok {
			m.Pos = V(cur.X+dx, cur.Y+dy, nz)
		} else {
			moved := false
			// slide along estimated wall normal
			if pr.nx != 0 || pr.ny != 0 {
				dn := dx*pr.nx + dy*pr.ny
				if dn < 0 {
					tx, ty := dx-dn*pr.nx, dy-dn*pr.ny
					if ok2, nz2, _ := tryMove(tx, ty); ok2 && (tx != 0 || ty != 0) {
						m.Pos = V(cur.X+tx, cur.Y+ty, nz2)
						vn := m.Vel.X*pr.nx + m.Vel.Y*pr.ny
						if vn < 0 {
							m.Vel.X -= vn * pr.nx
							m.Vel.Y -= vn * pr.ny
						}
						moved = true
					}
				}
			}
			if !moved {
				if ok2, nz2, _ := tryMove(dx, 0); ok2 && dx != 0 {
					m.Pos = V(cur.X+dx, cur.Y, nz2)
					m.Vel.Y = 0
					moved = true
				} else if ok3, nz3, _ := tryMove(0, dy); ok3 && dy != 0 {
					m.Pos = V(cur.X, cur.Y+dy, nz3)
					m.Vel.X = 0
					moved = true
				}
			}
			if !moved {
				m.Vel.X, m.Vel.Y = 0, 0
				// if we are currently inside geometry, push out along the normal
				cp := s.probe(cur.X, cur.Y, cur.Z, r, h, step)
				if cp.blocked && (cp.nx != 0 || cp.ny != 0) {
					px, py := cur.X+cp.nx*0.3, cur.Y+cp.ny*0.3
					if !s.probe(px, py, cur.Z, r, h, step).blocked {
						m.Pos.X, m.Pos.Y = px, py
					}
				}
			}
		}
	}
	// vertical integration
	pr := s.probe(m.Pos.X, m.Pos.Y, m.Pos.Z, r, h, 0.05)
	floor := pr.floor
	if math.IsInf(floor, -1) {
		floor = s.W.Ground(m.Pos.X, m.Pos.Y)
	}
	nz := m.Pos.Z + m.Vel.Z*dt
	if m.Vel.Z <= 0 {
		if nz <= floor {
			nz = floor
			m.Vel.Z = 0
			m.Grounded = true
		} else if m.Grounded && m.Vel.Z <= 0 && m.Pos.Z-floor < 0.6 {
			// stick to ground going downhill / small steps down
			nz = floor
			m.Vel.Z = 0
		} else {
			m.Grounded = false
		}
	} else {
		m.Grounded = false
		if nz+h > pr.ceil {
			nz = pr.ceil - h
			if nz < m.Pos.Z {
				nz = m.Pos.Z
			}
			m.Vel.Z = 0
		}
	}
	m.Pos.Z = nz
	if m.Grounded && m.Pos.Z < floor {
		m.Pos.Z = floor
	}
	// sounds
	hs := m.Vel.HLen()
	if m.Grounded && hs > s.C.Sound.StepSpeed {
		m.stepTimer += dt
		if m.stepTimer >= s.C.Sound.StepEvery {
			m.stepTimer = 0
			if m.boosting {
				s.sound("boost", m.Pos, m.ID)
			} else {
				s.sound("step", m.Pos, m.ID)
			}
		}
	}
	if m.jetting {
		m.jetTimer += dt
		if m.jetTimer >= 0.3 {
			m.jetTimer = 0
			s.sound("jet", m.Pos, m.ID)
		}
	}
}

func (s *Sim) pushApart() {
	ms := s.Mechs
	for i := 0; i < len(ms); i++ {
		a := ms[i]
		if !a.Alive {
			continue
		}
		for j := i + 1; j < len(ms); j++ {
			b := ms[j]
			if !b.Alive {
				continue
			}
			ra, rb := a.radius(), b.radius()
			dx, dy := b.Pos.X-a.Pos.X, b.Pos.Y-a.Pos.Y
			d := math.Hypot(dx, dy)
			if d >= ra+rb {
				continue
			}
			if a.Pos.Z+a.height() < b.Pos.Z || b.Pos.Z+b.height() < a.Pos.Z {
				continue
			}
			if d < 1e-6 {
				dx, dy, d = 1, 0, 1
			}
			ov := (ra + rb - d) / 2
			if ov > 0.5 {
				ov = 0.5
			}
			ux, uy := dx/d, dy/d
			pa := s.probe(a.Pos.X-ux*ov, a.Pos.Y-uy*ov, a.Pos.Z, ra, a.height(), 0.05)
			if !pa.blocked {
				a.Pos.X -= ux * ov
				a.Pos.Y -= uy * ov
			}
			pb := s.probe(b.Pos.X+ux*ov, b.Pos.Y+uy*ov, b.Pos.Z, rb, b.height(), 0.05)
			if !pb.blocked {
				b.Pos.X += ux * ov
				b.Pos.Y += uy * ov
			}
		}
	}
}
