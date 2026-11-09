package game

import (
	"math"
	"testing"
)

func TestDashDistanceAndWalls(t *testing.T) {
	for _, tc := range []struct {
		name      string
		walls     []Rect
		at        Vec2
		aim, x, z float64
	}{
		{name: "open", x: dashDistance},
		{name: "thin wall", walls: []Rect{{Vec2{1, -2}, Vec2{1.01, 2}}}, x: 1 - PlayerRadius},
		{name: "diagonal wall", walls: []Rect{{Vec2{1, -2}, Vec2{1.01, 2}}}, aim: math.Pi / 4, x: 1 - PlayerRadius, z: 1 - PlayerRadius},
		{name: "boundary", at: Vec2{X: 9}, x: 10 - PlayerRadius},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := testWorld(t, tc.walls...)
			combatSpawn(t, w, "k", tc.at, Knight)
			combatSpawn(t, w, "target", Vec2{X: .5}, Archer)
			if !w.Secondary("k", 1, tc.aim, nil) || w.Secondary("k", 1, tc.aim, nil) {
				t.Fatal("dash should activate only once")
			}
			p := playerPosition(t, w, "k")
			closeTo(t, p.X, tc.x)
			closeTo(t, p.Z, tc.z)
			knight, _ := w.Player("k")
			path := knight.SecondaryAttack.Dash
			if path == nil || path.From != tc.at || path.To != p {
				t.Fatalf("expected the dash path to match the actual movement: %+v", path)
			}
			hp(t, w, "target", 105)
			if !w.Primary("target", 1, 0) {
				t.Fatal("dash prevented target from attacking")
			}
		})
	}
}

func TestArcherFanHitsMultipleTargetsWithoutProjectiles(t *testing.T) {
	w := testWorld(t)
	combatSpawn(t, w, "a", Vec2{}, Archer)
	combatSpawn(t, w, "left", Vec2{2, -1}, Knight)
	combatSpawn(t, w, "right", Vec2{2, 1}, Mage)
	combatSpawn(t, w, "behind", Vec2{-1, 0}, Knight)
	combatSpawn(t, w, "extended", Vec2{3.7, 0}, Knight)
	combatSpawn(t, w, "far", Vec2{4.6, 0}, Knight)
	combatSpawn(t, w, "outside cone", Vec2{1.5, 2.5}, Knight)
	if !w.Secondary("a", 1, 0, nil) || w.Secondary("a", 2, 0, nil) {
		t.Fatal("expected one fan during cooldown")
	}
	hp(t, w, "left", MaxHP-fanDamage)
	hp(t, w, "right", 105-fanDamage)
	hp(t, w, "behind", MaxHP)
	hp(t, w, "extended", MaxHP-fanDamage)
	hp(t, w, "outside cone", MaxHP)
	hp(t, w, "far", MaxHP)
	if len(w.Projectiles()) != 0 {
		t.Fatal("fan spawned a projectile")
	}
}

func TestMeteorHitsAtImpactTick(t *testing.T) {
	w := testWorld(t)
	combatSpawn(t, w, "m", Vec2{}, Mage)
	combatSpawn(t, w, "inside", Vec2{4, 1}, Archer)
	combatSpawn(t, w, "outside", Vec2{4, 2}, Knight)
	if w.Secondary("m", 1, 0, &Vec2{X: 8}) || !w.Secondary("m", 2, 0, &Vec2{X: 4}) {
		t.Fatal("meteor targeting range was not respected")
	}
	for range meteorDelay - 1 {
		w.Step()
	}
	hp(t, w, "inside", 105)
	if meteors := w.Meteors(); len(meteors) != 1 || meteors[0].ImpactTick != meteorDelay {
		t.Fatalf("missing scheduled impact: %+v", meteors)
	}
	w.Step()
	hp(t, w, "inside", 105-meteorDamage)
	hp(t, w, "outside", MaxHP)
	if len(w.Meteors()) != 0 {
		t.Fatal("meteor remained after impact")
	}
}
