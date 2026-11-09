package game

import (
	"math"
	"testing"
)

func combatSpawn(t *testing.T, w *World, id string, at Vec2, hero Hero) {
	t.Helper()
	if err := w.SpawnHero(id, at, hero); err != nil {
		t.Fatal(err)
	}
}

func hp(t *testing.T, w *World, id string, want int) {
	t.Helper()
	p, ok := w.Player(id)
	if !ok || p.HP != want {
		t.Fatalf("%s: got %d HP, want %d", id, p.HP, want)
	}
}

func TestKnightPrimaryAndRespawn(t *testing.T) {
	w := testWorld(t)
	combatSpawn(t, w, "knight", Vec2{}, Knight)
	combatSpawn(t, w, "front", Vec2{X: 1}, Archer)
	combatSpawn(t, w, "behind", Vec2{X: -1}, Mage)
	if !w.Primary("knight", 1, 0) || w.Primary("knight", 2, 0) {
		t.Fatal("expected one attack during cooldown")
	}
	for range attacks[Knight].windup - 1 {
		w.Step()
	}
	hp(t, w, "front", 105)
	w.Step()
	hp(t, w, "front", 45)
	hp(t, w, "behind", 105)
	for range attacks[Knight].cooldown {
		w.Step()
	}
	if w.Primary("knight", 2, 0) {
		t.Fatal("replayed cooldown attempt was accepted")
	}
	for id := uint64(3); id <= 4; id++ {
		if !w.Primary("knight", id, 0) {
			t.Fatal("fresh attack rejected")
		}
		for range attacks[Knight].cooldown {
			w.Step()
		}
	}
	hp(t, w, "front", 0)
	if w.Primary("front", 1, 0) {
		t.Fatal("dead player attacked")
	}
	if err := w.SetInput("front", Input{Move: Vec2{X: 1}}); err != nil {
		t.Fatal(err)
	}
	dead, _ := w.Player("front")
	for w.Tick() <= dead.RespawnTick {
		w.Step()
	}
	hp(t, w, "front", 105)
	if pos := playerPosition(t, w, "front"); pos != (Vec2{X: 1}) {
		t.Fatalf("respawn inherited movement: %+v", pos)
	}
	if w.Primary("front", 1, 0) {
		t.Fatal("respawn accepted a replayed action")
	}
}

func TestRangedPrimary(t *testing.T) {
	for _, tc := range []struct {
		hero Hero
		want int
	}{{Archer, 112}, {Mage, 106}} {
		t.Run(string(tc.hero), func(t *testing.T) {
			for _, blocked := range []bool{false, true} {
				var walls []Rect
				if blocked {
					walls = []Rect{{Min: Vec2{X: 1.1, Z: -1}, Max: Vec2{X: 1.11, Z: 1}}}
				}
				w := testWorld(t, walls...)
				combatSpawn(t, w, "shooter", Vec2{}, tc.hero)
				combatSpawn(t, w, "first", Vec2{X: 2}, Knight)
				combatSpawn(t, w, "second", Vec2{X: 4}, Knight)
				if !w.Primary("shooter", 1, 0) {
					t.Fatal("attack rejected")
				}
				for range 30 {
					w.Step()
				}
				want := tc.want
				if blocked {
					want = MaxHP
				}
				hp(t, w, "shooter", 105)
				hp(t, w, "first", want)
				hp(t, w, "second", MaxHP)
				if len(w.Projectiles()) != 0 {
					t.Fatal("projectile did not disappear")
				}
			}
		})
	}
}

func TestRogueUsesBoundedViewTick(t *testing.T) {
	for _, tc := range []struct {
		name     string
		viewTick uint64
		hp       int
	}{
		{"visible target within rewind", 1, 83},
		{"legacy input uses current target", 0, 105},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := testWorld(t)
			combatSpawn(t, w, "rogue", Vec2{}, Rogue)
			combatSpawn(t, w, "moving", Vec2{X: 0.6}, Archer)
			if err := w.SetInput("moving", Input{Move: Vec2{X: 1}}); err != nil {
				t.Fatal(err)
			}
			for range 7 {
				w.Step()
			}
			if !w.PrimaryAt("rogue", 1, 0, tc.viewTick) {
				t.Fatal("attack rejected")
			}
			for range attacks[Rogue].windup - 1 {
				w.Step()
			}
			hp(t, w, "moving", 105)
			w.Step()
			hp(t, w, "moving", tc.hp)
			if tick := w.attackTick(1); tick != w.Tick()-hitRewindTicks {
				t.Fatalf("rewind exceeded limit: %d", tick)
			}
		})
	}
}

func TestSectorTouchesTargetRadiusButCannotHitThroughWall(t *testing.T) {
	for _, tc := range []struct {
		name  string
		walls []Rect
		want  int
	}{
		{"edge hit", nil, 45},
		{"wall blocks", []Rect{{Min: Vec2{X: 0.8, Z: -1}, Max: Vec2{X: 1, Z: 1}}}, 105},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := testWorld(t, tc.walls...)
			combatSpawn(t, w, "knight", Vec2{}, Knight)
			combatSpawn(t, w, "target", Vec2{X: 2.0}, Archer)
			if !w.Primary("knight", 1, 0) {
				t.Fatal("attack rejected")
			}
			for range attacks[Knight].windup {
				w.Step()
			}
			hp(t, w, "target", tc.want)
		})
	}
}

func TestHeavyMeleeCanTurnAndBeDodged(t *testing.T) {
	for _, hero := range []Hero{Knight, Berserker} {
		t.Run(string(hero), func(t *testing.T) {
			w := testWorld(t)
			combatSpawn(t, w, "attacker", Vec2{}, hero)
			combatSpawn(t, w, "target", Vec2{X: 1.65}, Archer)
			for range 7 {
				w.Step()
			}
			if !w.PrimaryAt("attacker", 1, 0, 1) {
				t.Fatal("expected an attack")
			}
			attacker := w.players["attacker"]
			spec := attacks[hero]
			if err := w.SetInput("attacker", Input{Aim: 1.5}); err != nil {
				t.Fatal(err)
			}
			closeTo(t, attacker.Attack.Aim, 1.5)
			for range spec.windup - 3 {
				w.Step()
			}
			if err := w.SetInput("attacker", Input{Aim: 0}); err != nil {
				t.Fatal(err)
			}
			closeTo(t, attacker.Attack.Aim, 0)
			if err := w.SetInput("target", Input{Move: Vec2{X: 1}}); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				w.Step()
			}
			hp(t, w, "target", 105)
			if w.Primary("attacker", 2, 0) {
				t.Fatal("attack accepted during recovery")
			}
			if err := w.SetInput("attacker", Input{Move: Vec2{X: 1}}); err != nil {
				t.Fatal(err)
			}
			for w.Tick() < attacker.PrimaryReadyTick {
				w.Step()
			}
			if attacker.Position.X <= 0 || !w.Primary("attacker", 3, 0) {
				t.Fatal("attacker should move during recovery and attack afterward")
			}
		})
	}
}

func TestHitsToDefeatKnight(t *testing.T) {
	for _, tc := range []struct {
		hero Hero
		hits uint64
	}{{Knight, 3}, {Archer, 7}} {
		t.Run(string(tc.hero), func(t *testing.T) {
			w := testWorld(t)
			combatSpawn(t, w, "attacker", Vec2{}, tc.hero)
			combatSpawn(t, w, "defender", Vec2{X: 1}, Knight)
			for i := uint64(1); i <= tc.hits; i++ {
				if !w.Primary("attacker", i, 0) {
					t.Fatalf("attack %d rejected", i)
				}
				for range attacks[tc.hero].cooldown {
					w.Step()
				}
				defender, _ := w.Player("defender")
				if (defender.HP == 0) != (i == tc.hits) {
					t.Fatalf("defender has %d HP after attack %d", defender.HP, i)
				}
			}
		})
	}
}

func TestMeleeWindupMovementBonus(t *testing.T) {
	for _, tc := range []struct {
		hero       Hero
		multiplier float64
	}{{Knight, 1.25}, {Berserker, 1.25}, {Rogue, 1.25}, {Archer, 1}, {Mage, 1}} {
		t.Run(string(tc.hero), func(t *testing.T) {
			w := testWorld(t)
			combatSpawn(t, w, "attacker", Vec2{}, tc.hero)
			combatSpawn(t, w, "enemy", Vec2{X: 4}, Archer)
			if err := w.SetInput("attacker", Input{Move: Vec2{X: 1}}); err != nil {
				t.Fatal(err)
			}
			if !w.Primary("attacker", 1, 0) {
				t.Fatal("attack rejected")
			}
			windup := attacks[tc.hero].windup
			step := tc.hero.MoveSpeed() / TickRate
			for range windup {
				w.Step()
			}
			want := float64(windup) * step * tc.multiplier
			closeTo(t, playerPosition(t, w, "attacker").X, want)
			w.Step()
			closeTo(t, playerPosition(t, w, "attacker").X, want+step)
		})
	}
}

func TestWindupSpeedNeedsNearbyVisibleEnemy(t *testing.T) {
	w := testWorld(t)
	for _, p := range []struct {
		id   string
		team Team
	}{{"attacker", Blue}, {"ally", Blue}, {"enemy", Red}} {
		if err := w.spawn(p.id, Vec2{}, Knight, p.team); err != nil {
			t.Fatal(err)
		}
	}
	enemy := w.players["enemy"]
	enemy.Position.X = 8
	if err := w.SetInput("attacker", Input{Move: Vec2{X: 1}}); err != nil {
		t.Fatal(err)
	}
	if !w.Primary("attacker", 1, 0) {
		t.Fatal("attack rejected")
	}
	step := Knight.MoveSpeed() / TickRate
	w.Step()
	closeTo(t, playerPosition(t, w, "attacker").X, step)
	enemy.Position.X = 2
	enemy.InvisibleUntil = 10
	w.Step()
	closeTo(t, playerPosition(t, w, "attacker").X, 2*step)
	enemy.InvisibleUntil = 0
	w.Step()
	closeTo(t, playerPosition(t, w, "attacker").X, 3.25*step)
	enemy.Position.X = 8
	w.Step()
	closeTo(t, playerPosition(t, w, "attacker").X, 4.25*step)
}

func TestMeleeCanTurnOnLastWindupTick(t *testing.T) {
	w := testWorld(t)
	combatSpawn(t, w, "knight", Vec2{}, Knight)
	combatSpawn(t, w, "behind", Vec2{X: -1}, Archer)
	if !w.Primary("knight", 1, 0) {
		t.Fatal("attack rejected")
	}
	for range attacks[Knight].windup - 1 {
		w.Step()
	}
	if err := w.SetInput("knight", Input{Aim: math.Pi}); err != nil {
		t.Fatal(err)
	}
	w.Step()
	hp(t, w, "behind", 45)
}

func TestProjectileRangeAndWiderContact(t *testing.T) {
	for _, tc := range []struct {
		hero     Hero
		distance float64
		wantHP   int
	}{{Archer, 11.9, 110}, {Mage, 9.9, 104}, {Bard, 7.9, 116}} {
		t.Run(string(tc.hero), func(t *testing.T) {
			w := testWorld(t)
			combatSpawn(t, w, "shooter", Vec2{X: -5}, tc.hero)
			combatSpawn(t, w, "target", Vec2{X: -5 + tc.distance, Z: 0.4}, Knight)
			if !w.Primary("shooter", 42, 0) {
				t.Fatal("attack rejected")
			}
			for range attacks[tc.hero].windup {
				w.Step()
			}
			if shots := w.Projectiles(); len(shots) != 1 || shots[0].ActionID != 42 {
				t.Fatalf("active projectile must reference the input action: %+v", shots)
			}
			for range TickRate - attacks[tc.hero].windup {
				w.Step()
			}
			hp(t, w, "target", tc.wantHP)
			ends := w.ProjectileEnds()
			if len(ends) != 1 || ends[0].Reason != "hit" || ends[0].Hero != tc.hero || ends[0].OwnerID != "shooter" || ends[0].ActionID != 42 {
				t.Fatalf("unexpected impact events: %+v", ends)
			}
		})
	}
}

func TestProjectileRadiusDoesNotReachThroughCover(t *testing.T) {
	w := testWorld(t, Rect{Min: Vec2{X: 1, Z: -1}, Max: Vec2{X: 1.02, Z: 1}})
	combatSpawn(t, w, "shooter", Vec2{}, Archer)
	combatSpawn(t, w, "target", Vec2{X: 1.4}, Knight)
	if !w.Primary("shooter", 1, 0) {
		t.Fatal("attack rejected")
	}
	for range 5 {
		w.Step()
	}
	hp(t, w, "target", MaxHP)
	ends := w.ProjectileEnds()
	if len(ends) != 1 || ends[0].Reason != "wall" {
		t.Fatalf("expected a wall impact: %+v", ends)
	}
	closeTo(t, ends[0].X, 1)
}

func TestPassiveRegeneration(t *testing.T) {
	for _, tc := range []struct {
		hero Hero
		gain int
	}{{Knight, 10}, {Rogue, 10}, {Berserker, 10}, {Archer, 5}, {Mage, 5}, {Bard, 5}} {
		t.Run(string(tc.hero), func(t *testing.T) {
			w := testWorld(t)
			combatSpawn(t, w, "player", Vec2{}, tc.hero)
			p := w.players["player"]
			w.damage(p, 20)
			for range 2 * TickRate {
				w.Step()
			}
			hp(t, w, p.ID, p.MaxHP-20+tc.gain)
			for range 10 * TickRate {
				w.Step()
			}
			hp(t, w, p.ID, p.MaxHP)
			w.damage(p, p.MaxHP)
			for range TickRate {
				w.Step()
			}
			hp(t, w, p.ID, 0)
		})
	}
}
