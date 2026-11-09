package game

import "fmt"

const (
	WinScore        = 300
	captureTicks    = 3 * TickRate
	roundBreakTicks = 12 * TickRate
)

type ControlZone struct {
	Position Vec2
	Radius   float64
}

type ZoneState struct {
	Owner     Team    `json:"owner"`
	Capturing Team    `json:"capturing"`
	Progress  float64 `json:"progress"`
	Contested bool    `json:"contested"`
}

type RoundStats struct {
	Kills           int `json:"kills"`
	Deaths          int `json:"deaths"`
	Captures        int `json:"captures"`
	InspiredAttacks int `json:"inspiredAttacks"`
}

type RoundState struct {
	Number      uint64 `json:"number"`
	Blue        int    `json:"blue"`
	Red         int    `json:"red"`
	Winner      Team   `json:"winner"`
	RestartTick uint64 `json:"restartTick"`
}

type controlPoint struct {
	ControlZone
	ZoneState
	ticks int
}

func (w *World) setControlZones(zones []ControlZone) error {
	for i, zone := range zones {
		p, r := zone.Position, zone.Radius
		if !finite(r) || r <= 0 || !w.canSpawn(p) ||
			p.X-r < w.bounds.Min.X || p.X+r > w.bounds.Max.X ||
			p.Z-r < w.bounds.Min.Z || p.Z+r > w.bounds.Max.Z {
			return fmt.Errorf("invalid control zone %d", i)
		}
		w.zones = append(w.zones, controlPoint{ControlZone: zone})
	}
	if len(w.zones) > 0 {
		w.round.Number = 1
		w.scoreTick = TickRate
	}
	return nil
}

func (w *World) Round() RoundState { return w.round }

func (w *World) Zones() []ZoneState {
	states := make([]ZoneState, len(w.zones))
	for i, zone := range w.zones {
		states[i] = zone.ZoneState
	}
	return states
}

func (w *World) stepControl() {
	for i := range w.zones {
		zone := &w.zones[i]
		var blue, red bool
		for _, p := range w.players {
			if p.HP == 0 || p.InvisibleUntil > w.tick ||
				botDistance(p.Position, zone.Position) > zone.Radius {
				continue
			}
			blue = blue || p.Team == Blue
			red = red || p.Team == Red
		}
		zone.Contested = blue && red
		if zone.Contested {
			continue
		}
		var team Team
		if blue {
			team = Blue
		} else if red {
			team = Red
		}
		if team == "" || team == zone.Owner {
			zone.Capturing, zone.ticks, zone.Progress = "", 0, 0
			continue
		}
		if zone.Capturing != team {
			zone.Capturing, zone.ticks = team, 0
		}
		zone.ticks++
		zone.Progress = float64(zone.ticks) / captureTicks
		if zone.ticks == captureTicks {
			for _, p := range w.players {
				if p.Team == team && p.HP > 0 && p.InvisibleUntil <= w.tick &&
					botDistance(p.Position, zone.Position) <= zone.Radius {
					p.Stats.Captures++
				}
			}
			zone.Owner, zone.Capturing, zone.ticks, zone.Progress = team, "", 0, 0
		}
	}
	if len(w.zones) == 0 || w.tick < w.scoreTick {
		return
	}
	w.scoreTick += TickRate
	for _, zone := range w.zones {
		if zone.Owner == Blue {
			w.round.Blue++
		} else if zone.Owner == Red {
			w.round.Red++
		}
	}
	if w.round.Blue < WinScore && w.round.Red < WinScore {
		return
	}
	switch {
	case w.round.Blue > w.round.Red:
		w.round.Winner = Blue
	case w.round.Red > w.round.Blue:
		w.round.Winner = Red
	}
	w.round.Blue = min(w.round.Blue, WinScore)
	w.round.Red = min(w.round.Red, WinScore)
	w.round.RestartTick = w.tick + roundBreakTicks
	w.projectiles, w.meteors = nil, nil
}

func (w *World) restartRound() {
	w.projectileEnds = nil
	w.round = RoundState{Number: w.round.Number + 1}
	w.scoreTick = w.tick + TickRate
	for i := range w.zones {
		w.zones[i].ZoneState = ZoneState{}
		w.zones[i].ticks = 0
	}
	for _, p := range w.players {
		p.Player = Player{
			ID: p.ID, Team: p.Team, Hero: p.Hero, Bot: p.Bot,
			Position: p.spawn, HP: p.MaxHP, MaxHP: p.MaxHP,
		}
		p.input = Input{}
		p.pending = false
		p.inspirationOwnerID = ""
		p.regenTicks = 0
		p.history = [hitRewindTicks + 1]pastPosition{}
	}
}
