package main

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"arena-backend/game"
	"arena-backend/messages"
)

func TestArenaLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := startArena(ctx, testBounds(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Spawn("p", game.Vec2{}); err != nil {
		t.Fatal(err)
	}
	input := game.Input{Move: game.Vec2{X: 1}, Aim: 1.25}
	if err := a.SetInput("p", 3, input); err != nil {
		t.Fatal(err)
	}
	if err := a.SetInput("p", 2, input); !errors.Is(err, ErrStaleInput) {
		t.Fatalf("stale input: %v", err)
	}

	snapshot := waitSnapshot(t, a, func(s Snapshot) bool {
		return len(s.Players) == 1 && s.Players[0].LastInputSeq == 3
	})
	if p := snapshot.Players[0]; p.Position.X <= 0 || p.Aim != input.Aim {
		t.Fatalf("input missing from snapshot: %+v", p)
	}

	if err := a.Remove("p"); err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, a, func(s Snapshot) bool { return len(s.Players) == 0 })
	cancel()
	select {
	case <-a.Done():
	case <-time.After(time.Second):
		t.Fatal("arena did not stop")
	}
	if err := a.Remove("p"); !errors.Is(err, ErrClosed) {
		t.Fatalf("command after shutdown: %v", err)
	}
}

func TestExpiredInputAndUnreadSnapshots(t *testing.T) {
	w, err := game.NewWorld(testBounds(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := newArena()
	if err := w.Spawn("p", game.Vec2{}); err != nil {
		t.Fatal(err)
	}
	if err := w.SetInput("p", game.Input{Move: game.Vec2{X: 1}, Aim: 1}); err != nil {
		t.Fatal(err)
	}
	s := simulation{
		world:   w,
		players: map[string]inputState{"p": {seq: 7, aim: 1, active: true}},
	}
	for range game.TickRate {
		a.step(&s)
	}
	got := <-a.Snapshots()
	if got.ServerTick != game.TickRate || len(got.Players) != 1 {
		t.Fatalf("wanted latest snapshot, got %+v", got)
	}
	p := got.Players[0]
	wantX := inputTimeout * game.Knight.MoveSpeed() / game.TickRate
	if math.Abs(p.Position.X-wantX) > 1e-9 || p.Aim != 1 || p.LastInputSeq != 7 {
		t.Fatalf("expired input should preserve aim and sequence and stop at %v, got %+v", wantX, p)
	}
}

func testBounds() game.Rect {
	return game.Rect{Min: game.Vec2{X: -10, Z: -10}, Max: game.Vec2{X: 10, Z: 10}}
}

func waitSnapshot(t *testing.T, a *Arena, matches func(Snapshot) bool) Snapshot {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case s, open := <-a.Snapshots():
			if !open {
				t.Fatal("snapshot feed closed")
			}
			if matches(s) {
				return s
			}
		case <-timeout.C:
			t.Fatal("expected snapshot did not arrive")
		}
	}
}

func TestQueuedMovementConsumesExactlyOneCommandPerTick(t *testing.T) {
	w, err := game.NewWorld(testBounds(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := newArena()
	if err := w.Spawn("p", game.Vec2{}); err != nil {
		t.Fatal(err)
	}
	s := simulation{world: w, players: map[string]inputState{"p": {}}}
	for _, seq := range []uint64{1, 1, 2} {
		if err := a.QueueStep("p", seq, game.Input{Move: game.Vec2{X: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, expected := range []uint64{1, 2, 2, 2} {
		a.step(&s)
		p := s.snapshot().Players[0]
		want := float64(expected) * game.Knight.MoveSpeed() / game.TickRate
		if p.LastInputSeq != expected || math.Abs(p.Position.X-want) > 1e-9 {
			t.Fatalf("tick %d: got seq %d and X %v; want seq %d and X %v",
				w.Tick(), p.LastInputSeq, p.Position.X, expected, want)
		}
	}
}

func TestMovingShotStartsOnItsInputStep(t *testing.T) {
	for _, hero := range []game.Hero{game.Archer, game.Mage, game.Bard} {
		t.Run(string(hero), func(t *testing.T) {
			w, err := game.NewWorld(testBounds(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.SpawnHero("p", game.Vec2{}, hero); err != nil {
				t.Fatal(err)
			}
			a := newArena()
			s := simulation{world: w, players: map[string]inputState{"p": {}}}
			aim := math.Pi / 2
			if err := a.QueueStep("p", 1, game.Input{Move: game.Vec2{X: -1}}); err != nil {
				t.Fatal(err)
			}
			if err := a.queueStep("p", 2, game.Input{Move: game.Vec2{X: 1}},
				[]messages.Action{{ID: 1, Button: "primary", Aim: &aim}}); err != nil {
				t.Fatal(err)
			}
			a.step(&s)
			if player, _ := w.Player("p"); player.Attack.ID != 0 {
				t.Fatal("shot started before its movement step")
			}
			a.step(&s)
			a.step(&s)
			shots := w.Projectiles()
			if len(shots) != 1 || math.Abs(shots[0].LaunchPosition.X) > 1e-9 ||
				math.Abs(shots[0].Aim-aim) > 1e-9 {
				t.Fatalf("expected shot from the position after the linked movement: %+v", shots)
			}
		})
	}
}
