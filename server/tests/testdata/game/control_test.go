package game

import "testing"

func TestControlRound(t *testing.T) {
	w, err := NewWorld(Rect{Min: Vec2{-15, -15}, Max: Vec2{15, 15}}, nil,
		ControlZone{Radius: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.spawn("blue", Vec2{-8, 0}, Knight, Blue); err != nil {
		t.Fatal(err)
	}
	if err := w.spawn("red", Vec2{8, 0}, Knight, Red); err != nil {
		t.Fatal(err)
	}
	blue, red := w.players["blue"], w.players["red"]
	blue.Position = Vec2{}
	for range captureTicks / 2 {
		w.Step()
	}
	red.Position = Vec2{1, 0}
	for range captureTicks {
		w.Step()
	}
	if zone := w.Zones()[0]; !zone.Contested || zone.Owner != "" || zone.Progress != 0.5 {
		t.Fatalf("contested zone must pause capture: %+v", zone)
	}
	red.InvisibleUntil = w.Tick() + captureTicks
	for range captureTicks / 2 {
		w.Step()
	}
	if zone := w.Zones()[0]; zone.Owner != Blue || zone.Contested {
		t.Fatalf("blue should capture while red is invisible: %+v", zone)
	}
	red.Position = red.spawn
	blue.Position = Vec2{6, 0}
	before := w.Round().Blue
	for range TickRate {
		w.Step()
	}
	if w.Round().Blue != before+1 {
		t.Fatal("an owned zone should score even when empty")
	}

	red.InvisibleUntil = 0
	red.Position = Vec2{}
	for range captureTicks {
		w.Step()
	}
	if w.Zones()[0].Owner != Red {
		t.Fatal("red should take over an undefended zone")
	}
	w.round.Red = WinScore - 1
	for range TickRate {
		w.Step()
	}
	win := w.Round()
	if win.Red != WinScore || win.Winner != Red || win.RestartTick <= w.Tick() {
		t.Fatalf("expected red to win: %+v", win)
	}
	if w.Primary("blue", 1, 0) {
		t.Fatal("attacks must be paused between rounds")
	}
	blue.HP = 1
	for w.Tick() < win.RestartTick {
		w.Step()
	}
	if w.Round() != (RoundState{Number: 2}) || w.Zones()[0] != (ZoneState{}) {
		t.Fatalf("new round should reset scores and control: %+v, %+v", w.Round(), w.Zones())
	}
	if blue.Position != blue.spawn || blue.HP != blue.MaxHP || blue.Team != Blue {
		t.Fatalf("player should restart at their team's spawn: %+v", blue.Player)
	}
	if w.Primary("blue", 1, 0) || !w.Primary("blue", 2, 0) {
		t.Fatal("action IDs should stay monotonic across rounds")
	}
}

func TestRoundRestartsOnTimerWithNewPlayer(t *testing.T) {
	w := testWorld(t)
	combatSpawn(t, w, "one", Vec2{}, Knight)
	combatSpawn(t, w, "two", Vec2{X: 1}, Archer)
	combatSpawn(t, w, "bot", Vec2{X: 2}, Bard)
	w.players["bot"].Bot = true
	w.players["bot"].Team = Blue
	w.players["one"].Team = Blue
	w.players["two"].Team = Red
	w.round = RoundState{Number: 2, RestartTick: 12 * TickRate}
	w.damageFrom("one", w.players["two"], 999)
	if w.players["one"].Stats.Kills != 1 || w.players["two"].Stats.Deaths != 1 {
		t.Fatal("expected a kill and a death")
	}
	blue, red := []Vec2{{X: -4}}, []Vec2{{X: 4}}
	w.FillBots(3, blue, red)
	if _, err := w.Join("new", Mage, blue, red); err != nil {
		t.Fatal(err)
	}
	w.FillBots(3, blue, red)
	if len(w.Bots()) != 0 {
		t.Fatal("a bot should make room immediately during results")
	}
	for w.Tick()+1 < w.Round().RestartTick {
		w.Step()
		if w.Round().Number != 2 {
			t.Fatal("restarted before the timer finished")
		}
	}
	w.Step()
	if w.Round().Number != 3 || w.players["one"].Stats != (RoundStats{}) ||
		w.players["two"].HP != w.players["two"].MaxHP {
		t.Fatal("new round did not reset players and stats")
	}
}
