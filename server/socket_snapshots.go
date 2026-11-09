package main

import (
	"math"
	"slices"
	"strings"
	"time"

	"arena-backend/game"
	"arena-backend/messages"

	"github.com/gorilla/websocket"
)

// Position precision on the wire is one thousandth of a world unit.
func wirePosition(value float64) float64 {
	return math.Round(value*1000) / 1000
}

type sentSnapshot struct {
	tick        uint64
	restartTick uint64
}

// Filter at write time so replacing a queued snapshot cannot discard its events.
func (sent sentSnapshot) compact(snapshot messages.Snapshot) messages.Snapshot {
	first := 0
	for first < len(snapshot.ProjectileEnds) && snapshot.ProjectileEnds[first].Tick <= sent.tick {
		first++
	}
	snapshot.ProjectileEnds = snapshot.ProjectileEnds[first:]
	if snapshot.Round.RestartTick == sent.restartTick {
		snapshot.RoundResults = nil
	}
	return snapshot
}

func (s *Server) writeSnapshots(conn *websocket.Conn, c *client, done <-chan struct{}) {
	defer conn.Close()
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	var sent sentSnapshot
	sendPong := func(id uint64) error {
		pong := struct {
			Type string `json:"type"`
			ID   uint64 `json:"id"`
		}{Type: "pong", ID: id}
		return writeJSON(conn, pong)
	}
	for {
		// Give a pending pong priority over the next snapshot.
		select {
		case id := <-c.pongs:
			if sendPong(id) != nil {
				return
			}
		default:
		}
		select {
		case <-done:
			return
		case <-s.arena.Done():
			return
		case id := <-c.pongs:
			if sendPong(id) != nil {
				return
			}
		case snapshot := <-c.snapshots:
			if c.compactSnapshots {
				snapshot = sent.compact(snapshot)
			}
			if writeJSON(conn, snapshot) != nil {
				return
			}
			sent = sentSnapshot{tick: snapshot.ServerTick, restartTick: snapshot.Round.RestartTick}
		case <-ping.C:
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeTimeout)) != nil {
				return
			}
		}
	}
}

func (s *Server) broadcast() {
	for snapshot := range s.arena.Snapshots() {
		s.mu.Lock()
		players := make([]messages.Player, 0, len(snapshot.Players))
		hidden := make(map[string]messages.Player)
		var roundResults []messages.RoundResultPlayer
		counts := map[string]int{"blue": 0, "red": 0}
		for _, p := range snapshot.Players {
			if c := s.clients[p.ID]; c != nil || p.Bot {
				nick := "Бот " + strings.TrimPrefix(p.ID, "bot-")
				if c != nil {
					nick = c.nick
				}
				counts[string(p.Team)]++
				if snapshot.Round.RestartTick != 0 {
					roundResults = append(roundResults, messages.RoundResultPlayer{
						ID: p.ID, Nick: nick, Team: p.Team, Hero: p.Hero, Bot: p.Bot,
						RoundStats: p.Stats,
					})
				}
				wire := messages.Player{
					ID: p.ID, Team: string(p.Team), Nick: nick, Bot: p.Bot, Hero: string(p.Hero), X: wirePosition(p.Position.X), Z: wirePosition(p.Position.Z), Aim: p.Aim,
					HP: p.HP, MaxHP: p.MaxHP, RespawnTick: p.RespawnTick,
					Attack: wireAttack(p.Attack), SecondaryAttack: wireAttack(p.SecondaryAttack),
					InspirationCharges: p.InspirationCharges, InspirationSpeedUntil: p.InspirationSpeedUntil,
					PrimaryReadyTick: p.PrimaryReadyTick, SecondaryReadyTick: p.SecondaryReadyTick, InvisibleUntil: p.InvisibleUntil,
				}
				if p.InvisibleUntil > snapshot.ServerTick {
					hidden[p.ID] = wire
				} else {
					players = append(players, wire)
				}
			}
		}
		projectiles := make([]messages.Projectile, 0, len(snapshot.Projectiles))
		for _, p := range snapshot.Projectiles {
			projectiles = append(projectiles, messages.Projectile{
				ID: p.ID, ActionID: p.ActionID, Team: string(p.Team), OwnerID: p.OwnerID, Hero: string(p.Hero), Aim: p.Aim, X: wirePosition(p.Position.X), Z: wirePosition(p.Position.Z),
				LaunchTick: p.LaunchTick, LaunchX: wirePosition(p.LaunchPosition.X), LaunchZ: wirePosition(p.LaunchPosition.Z),
			})
		}
		meteors := make([]messages.Meteor, 0, len(snapshot.Meteors))
		for _, m := range snapshot.Meteors {
			meteors = append(meteors, messages.Meteor{
				ID: m.ID, Team: string(m.Team), OwnerID: m.OwnerID, X: m.Position.X, Z: m.Position.Z,
				ImpactTick: m.ImpactTick, Radius: m.Radius,
			})
		}
		for _, p := range snapshot.Players {
			c := s.clients[p.ID]
			if c == nil {
				continue
			}
			select {
			case <-c.snapshots:
			default:
			}
			visible := players
			if own, ok := hidden[p.ID]; ok {
				visible = slices.Concat(players, []messages.Player{own})
			}
			c.snapshots <- messages.Snapshot{
				Type: "snapshot", ServerTick: snapshot.ServerTick,
				LastInputSeq: p.LastInputSeq, Players: visible, Projectiles: projectiles, Meteors: meteors,
				ProjectileEnds: snapshot.ProjectileEnds, RoundResults: roundResults,
				TeamCounts: counts, Round: snapshot.Round, Zones: snapshot.Zones,
			}
		}
		s.mu.Unlock()
	}
}

func wireAttack(attack game.Attack) *messages.Attack {
	if attack.ID == 0 {
		return nil
	}
	wire := &messages.Attack{
		ID: attack.ID, StartTick: attack.StartTick, HitTick: attack.HitTick, Aim: attack.Aim,
		WindupMoveMultiplier: attack.WindupMoveMultiplier,
	}
	if dash := attack.Dash; dash != nil {
		wire.Dash = &messages.DashPath{From: messages.Point(dash.From), To: messages.Point(dash.To)}
	}
	return wire
}
