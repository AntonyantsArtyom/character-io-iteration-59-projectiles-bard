package game

import (
	"fmt"
	"testing"
)

func TestBotsFillToTenAndLeaveForPlayers(t *testing.T) {
	w := testWorld(t)
	blue, red := []Vec2{{X: -5}}, []Vec2{{X: 5}}
	w.FillBots(10, blue, red)
	if len(w.Bots()) != 10 {
		t.Fatal("empty arena should start with ten bots")
	}
	for i := range 11 {
		id := fmt.Sprint(i)
		team, err := w.Join(id, Knight, blue, red)
		want := Blue
		if i%2 != 0 {
			want = Red
		}
		if err != nil || team != want {
			t.Fatalf("human %s assigned %q, %v; want %q", id, team, err, want)
		}
		w.FillBots(10, blue, red)
		if count := len(w.Bots()); count != max(0, 9-i) {
			t.Fatalf("after %d joins: %d bots", i+1, count)
		}
	}
	w.Remove("1")
	w.Remove("3")
	w.FillBots(10, blue, red)
	bots := w.Bots()
	if len(bots) != 1 || bots[0].Team != Red {
		t.Fatalf("expected a bot to fill smaller red team: %+v", bots)
	}
}

func TestBotGoesAroundWallAndAttacks(t *testing.T) {
	w := testWorld(t, Rect{Min: Vec2{X: -1, Z: -2}, Max: Vec2{X: 1, Z: 2}})
	if err := w.spawn("target", Vec2{X: 4}, Knight, Red); err != nil {
		t.Fatal(err)
	}
	w.FillBots(2, []Vec2{{X: -4}}, []Vec2{{X: 4}})
	for range 7 * TickRate {
		w.ThinkBots()
		w.Step()
		p, _ := w.Player("target")
		if p.HP < p.MaxHP {
			return
		}
	}
	t.Fatalf("bot did not reach its target: %+v", w.Bots())
}
