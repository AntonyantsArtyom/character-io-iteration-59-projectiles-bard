package game

import (
	"math"
	"testing"
)

func testWorld(t *testing.T, walls ...Rect) *World {
	t.Helper()
	w, err := NewWorld(Rect{Min: Vec2{-10, -10}, Max: Vec2{10, 10}}, walls)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func movingPlayer(t *testing.T, w *World, id string, at Vec2, input Input) {
	t.Helper()
	if err := w.Spawn(id, at); err != nil {
		t.Fatal(err)
	}
	if err := w.SetInput(id, input); err != nil {
		t.Fatal(err)
	}
}

func playerPosition(t *testing.T, w *World, id string) Vec2 {
	t.Helper()
	p, ok := w.Player(id)
	if !ok {
		t.Fatalf("player %q missing", id)
	}
	return p.Position
}

func closeTo(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestStepMovesPlayersIndependently(t *testing.T) {
	w := testWorld(t)
	movingPlayer(t, w, "straight", Vec2{}, Input{Move: Vec2{1, 0}})
	movingPlayer(t, w, "diagonal", Vec2{}, Input{Move: Vec2{1, 1}, Aim: 1.25})
	for range TickRate {
		w.Step()
	}

	straight := playerPosition(t, w, "straight")
	diagonal := playerPosition(t, w, "diagonal")
	closeTo(t, straight.X, Knight.MoveSpeed())
	closeTo(t, straight.Z, 0)
	closeTo(t, diagonal.X, Knight.MoveSpeed()/math.Sqrt2)
	closeTo(t, diagonal.Z, Knight.MoveSpeed()/math.Sqrt2)
	p, _ := w.Player("diagonal")
	closeTo(t, p.Aim, 1.25)
	if w.Tick() != TickRate {
		t.Errorf("got tick %d, want %d", w.Tick(), TickRate)
	}
}

func TestStepSlidesAlongWallAndStopsAtBoundary(t *testing.T) {
	w := testWorld(t, Rect{Min: Vec2{0, -10}, Max: Vec2{0.01, 10}})
	movingPlayer(t, w, "slider", Vec2{-0.4, 8}, Input{Move: Vec2{1, 1}})
	for range TickRate {
		w.Step()
	}
	p := playerPosition(t, w, "slider")
	closeTo(t, p.X, -PlayerRadius)
	closeTo(t, p.Z, 10-PlayerRadius)
}

func TestStepHitsRoundedCorner(t *testing.T) {
	w := testWorld(t, Rect{Min: Vec2{0, 0}, Max: Vec2{1, 1}})
	movingPlayer(t, w, "corner", Vec2{-0.5, -0.2}, Input{Move: Vec2{1, 0}})
	for range TickRate {
		w.Step()
	}
	p := playerPosition(t, w, "corner")
	closeTo(t, p.X, -math.Sqrt(PlayerRadius*PlayerRadius-0.2*0.2))
	closeTo(t, p.Z, -0.2)
}

func TestSpawnAndInputValidation(t *testing.T) {
	w := testWorld(t, Rect{Min: Vec2{0, 0}, Max: Vec2{1, 1}})
	if err := w.Spawn("blocked", Vec2{0, 0}); err == nil {
		t.Fatal("spawn in a wall accepted")
	}
	movingPlayer(t, w, "p", Vec2{-2, 0}, Input{Move: Vec2{1, 0}})
	if err := w.Spawn("p", Vec2{-3, 0}); err == nil {
		t.Fatal("duplicate ID accepted")
	}
	if err := w.SetInput("p", Input{Move: Vec2{math.NaN(), 0}}); err == nil {
		t.Fatal("nonfinite input accepted")
	}
	w.Step()
	closeTo(t, playerPosition(t, w, "p").X, -2+Knight.MoveSpeed()/TickRate)
	w.Remove("p")
	if _, ok := w.Player("p"); ok {
		t.Fatal("removed player still present")
	}
}
