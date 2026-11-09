package game

import "testing"

func TestTeamAssignmentAndRespawn(t *testing.T) {
	w := testWorld(t)
	blue, red := []Vec2{{X: -5}, {X: -6}}, []Vec2{{X: 5}, {X: 6}}
	for i, id := range []string{"a", "b", "c", "d"} {
		team, err := w.Join(id, Knight, blue, red)
		want := [...]Team{Blue, Red, Blue, Red}[i]
		if err != nil || team != want {
			t.Fatalf("%s: got %q, %v; want %q", id, team, err, want)
		}
	}
	w.Remove("b")
	team, err := w.Join("e", Knight, blue, red)
	if err != nil || team != Red {
		t.Fatalf("join after red departure: %q, %v", team, err)
	}
	p := w.players["c"]
	p.Position = Vec2{}
	w.damage(p, MaxHP)
	for range respawnDelay {
		w.Step()
	}
	if p.Team != Blue || p.Position != blue[1] || p.HP != MaxHP {
		t.Fatalf("wrong team respawn: %+v", p.Player)
	}
}

func TestTeamAttacksSkipAllies(t *testing.T) {
	for _, tc := range []struct {
		name        string
		hero        Hero
		secondary   bool
		damage      int
		regenerated int
	}{
		{"sword", Knight, false, 60, 2},
		{"arrow", Archer, false, 22, 4},
		{"spell", Mage, false, 28, 4},
		{"fan", Archer, true, fanDamage, 5},
		{"meteor", Mage, true, meteorDamage, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := testWorld(t)
			for _, p := range []struct {
				id   string
				team Team
				hero Hero
				x    float64
			}{
				{"attacker", Blue, tc.hero, 0},
				{"ally", Blue, Knight, 0.6},
				{"enemy", Red, Knight, 1.4},
			} {
				if err := w.spawn(p.id, Vec2{X: p.x}, p.hero, p.team); err != nil {
					t.Fatal(err)
				}
			}
			if tc.secondary {
				if !w.Secondary("attacker", 1, 0, &Vec2{X: 1}) {
					t.Fatal("secondary rejected")
				}
			} else if !w.Primary("attacker", 1, 0) {
				t.Fatal("primary rejected")
			}
			if tc.name == "meteor" {
				w.Remove("attacker")
			}
			for range TickRate {
				w.Step()
			}
			hp(t, w, "ally", MaxHP)
			hp(t, w, "enemy", MaxHP-tc.damage+tc.regenerated)
		})
	}
}
