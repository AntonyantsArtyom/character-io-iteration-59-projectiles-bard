package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"arena-backend/game"
	"arena-backend/messages"
)

const (
	commandsPerTick = 64
	snapshotEvery   = 2
	inputTimeout    = game.TickRate / 2
	maxQueuedSteps  = 8
)

var (
	ErrClosed     = errors.New("arena closed")
	ErrBusy       = errors.New("arena command queue full")
	ErrStaleInput = errors.New("input sequence is not increasing")
)

type PlayerSnapshot struct {
	game.Player
	LastInputSeq uint64
}

type Snapshot struct {
	Round          game.RoundState
	Zones          []game.ZoneState
	ServerTick     uint64
	Players        []PlayerSnapshot
	Projectiles    []game.Projectile
	ProjectileEnds []game.ProjectileEnd
	Meteors        []game.Meteor
}

type command struct {
	apply  func(*simulation) error
	result chan error
}

type Arena struct {
	commands  chan command
	snapshots chan Snapshot
	done      chan struct{}
}

type movementStep struct {
	actions []messages.Action
	seq     uint64
	input   game.Input
}

type inputState struct {
	stepped bool
	queue   []movementStep
	seq     uint64
	tick    uint64
	aim     float64
	active  bool
}

type simulation struct {
	world                 *game.World
	players               map[string]inputState
	botTarget             int
	blueSpawns, redSpawns []game.Vec2
}

// startArena runs one arena until ctx is canceled.
func startArena(ctx context.Context, bounds game.Rect, walls []game.Rect, zones ...game.ControlZone) (*Arena, error) {
	world, err := game.NewWorld(bounds, walls, zones...)
	if err != nil {
		return nil, err
	}
	ticker := time.NewTicker(time.Second / game.TickRate)
	a := newArena()
	go func() {
		defer ticker.Stop()
		a.run(ctx, world, ticker.C)
	}()
	return a, nil
}

func newArena() *Arena {
	return &Arena{
		commands:  make(chan command, commandsPerTick),
		snapshots: make(chan Snapshot, 1),
		done:      make(chan struct{}),
	}
}

// EnableBots configures automatic population for this arena.
func (a *Arena) EnableBots(target int, blueSpawns, redSpawns []game.Vec2) error {
	return a.submit(func(s *simulation) error {
		s.botTarget = target
		s.blueSpawns, s.redSpawns = slices.Clone(blueSpawns), slices.Clone(redSpawns)
		return nil
	})
}

func (a *Arena) Done() <-chan struct{} { return a.done }

// Snapshots is a single-consumer feed. A slow reader gets the latest
// available snapshot; a future transport can distribute it to connections.
func (a *Arena) Snapshots() <-chan Snapshot { return a.snapshots }

func (a *Arena) Spawn(id string, at game.Vec2) error {
	return a.SpawnHero(id, at, game.Knight)
}

func (a *Arena) SpawnHero(id string, at game.Vec2, hero game.Hero) error {
	return a.submit(func(s *simulation) error {
		if err := s.world.SpawnHero(id, at, hero); err != nil {
			return err
		}
		s.players[id] = inputState{}
		return nil
	})
}

func (a *Arena) Join(id string, hero game.Hero, blueSpawns, redSpawns []game.Vec2) (game.Team, error) {
	var team game.Team
	err := a.submit(func(s *simulation) error {
		assigned, err := s.world.Join(id, hero, blueSpawns, redSpawns)
		if err != nil {
			return err
		}
		team = assigned
		s.players[id] = inputState{}
		return nil
	})
	if err != nil {
		return "", err
	}
	return team, nil
}

// Remove is idempotent. On ErrBusy the command was not queued and may be retried.
func (a *Arena) Remove(id string) error {
	return a.submit(func(s *simulation) error {
		s.world.Remove(id)
		delete(s.players, id)
		return nil
	})
}

// SetInput applies a positive, strictly increasing sequence for this player.
func (a *Arena) SetInput(id string, seq uint64, input game.Input) error {
	return a.submit(func(s *simulation) error {
		state, exists := s.players[id]
		if !exists {
			return fmt.Errorf("unknown player %q", id)
		}
		if state.stepped || seq <= state.seq {
			return ErrStaleInput
		}
		if err := s.world.SetInput(id, input); err != nil {
			return err
		}
		s.players[id] = inputState{seq: seq, tick: s.world.Tick(), aim: input.Aim, active: true}
		return nil
	})
}

// QueueStep enqueues one movement tick without waiting for simulation.
func (a *Arena) QueueStep(id string, seq uint64, input game.Input) error {
	return a.queueStep(id, seq, input, nil)
}

func (a *Arena) queueStep(id string, seq uint64, input game.Input, actions []messages.Action) error {
	if seq == 0 || !input.Valid() || len(actions) > 2 {
		return fmt.Errorf("invalid movement step")
	}
	return a.enqueue(command{apply: func(s *simulation) error {
		state, exists := s.players[id]
		if !exists || seq <= state.seq || (len(state.queue) > 0 && seq <= state.queue[len(state.queue)-1].seq) {
			return nil
		}
		state.stepped = true
		if len(state.queue) == maxQueuedSteps {
			copy(state.queue, state.queue[1:])
			state.queue = state.queue[:len(state.queue)-1]
		}
		state.queue = append(state.queue, movementStep{seq: seq, input: input, actions: actions})
		s.players[id] = state
		return nil
	}})
}

func applyAction(world *game.World, id string, action messages.Action) {
	if action.ID == 0 || action.Aim == nil {
		return
	}
	switch action.Button {
	case "primary":
		world.PrimaryAt(id, action.ID, *action.Aim, action.ViewTick)
	case "secondary":
		var target *game.Vec2
		if action.Target != nil {
			point := game.Vec2(*action.Target)
			target = &point
		}
		world.SecondaryAt(id, action.ID, *action.Aim, target, action.ViewTick)
	}
}

func (a *Arena) enqueue(c command) error {
	select {
	case <-a.done:
		return ErrClosed
	default:
	}
	select {
	case a.commands <- c:
		return nil
	default:
		return ErrBusy
	}
}

func (a *Arena) submit(apply func(*simulation) error) error {
	c := command{apply: apply, result: make(chan error, 1)}
	if err := a.enqueue(c); err != nil {
		return err
	}
	select {
	case err := <-c.result:
		return err
	case <-a.done:
		// A tick may have completed just before shutdown.
		select {
		case err := <-c.result:
			return err
		default:
			return ErrClosed
		}
	}
}

func (a *Arena) run(ctx context.Context, world *game.World, ticks <-chan time.Time) {
	defer close(a.done)
	defer close(a.snapshots)
	s := simulation{world: world, players: make(map[string]inputState)}
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			a.step(&s)
		}
	}
}

func (a *Arena) step(s *simulation) {
	for range len(a.commands) {
		c := <-a.commands
		err := c.apply(s)
		if c.result != nil {
			c.result <- err
		}
	}
	for id, state := range s.players {
		if state.stepped {
			input := game.Input{Aim: state.aim}
			var actions []messages.Action
			if len(state.queue) > 0 {
				next := state.queue[0]
				state.queue = state.queue[1:]
				state.seq, state.aim = next.seq, next.input.Aim
				input, actions = next.input, next.actions
			}
			_ = s.world.SetInput(id, input)
			for _, action := range actions {
				applyAction(s.world, id, action)
			}
			s.players[id] = state
		} else if state.active && s.world.Tick()-state.tick >= inputTimeout {
			// The player still exists and the stored aim has already been validated.
			_ = s.world.SetInput(id, game.Input{Aim: state.aim})
			state.active = false
			s.players[id] = state
		}
	}
	if s.botTarget > 0 {
		s.world.FillBots(s.botTarget, s.blueSpawns, s.redSpawns)
		s.world.ThinkBots()
	}
	round := s.world.Round().Number
	s.world.Step()
	if s.world.Round().Number != round {
		for id, state := range s.players {
			if len(state.queue) > 0 {
				state.seq = state.queue[len(state.queue)-1].seq
			}
			state.queue, state.active = nil, false
			s.players[id] = state
		}
	}
	if s.world.Tick()%snapshotEvery == 0 {
		a.publish(s.snapshot())
	}
}

func (s *simulation) snapshot() Snapshot {
	snapshot := Snapshot{
		ServerTick: s.world.Tick(),
		Round:      s.world.Round(), Zones: s.world.Zones(),
		Projectiles:    s.world.Projectiles(),
		ProjectileEnds: s.world.ProjectileEnds(),
		Meteors:        s.world.Meteors(),
		Players:        make([]PlayerSnapshot, 0, len(s.players)),
	}
	for id, state := range s.players {
		player, _ := s.world.Player(id)
		snapshot.Players = append(snapshot.Players, PlayerSnapshot{Player: player, LastInputSeq: state.seq})
	}
	for _, bot := range s.world.Bots() {
		snapshot.Players = append(snapshot.Players, PlayerSnapshot{Player: bot})
	}
	slices.SortFunc(snapshot.Players, func(a, b PlayerSnapshot) int {
		return strings.Compare(a.ID, b.ID)
	})
	return snapshot
}

func (a *Arena) publish(snapshot Snapshot) {
	select {
	case <-a.snapshots:
	default:
	}
	a.snapshots <- snapshot
}
