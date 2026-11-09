package game

import "testing"

func TestRogueInvisibilityEnds(t *testing.T) {
	for _, cause := range []string{"timeout", "attack", "damage"} {
		t.Run(cause, func(t *testing.T) {
			w := testWorld(t)
			combatSpawn(t, w, "rogue", Vec2{}, Rogue)
			if !w.Secondary("rogue", 1, 0, nil) || w.players["rogue"].InvisibleUntil != 150 {
				t.Fatal("stealth did not start for five seconds")
			}
			switch cause {
			case "timeout":
				for range 149 {
					w.Step()
				}
				if w.players["rogue"].InvisibleUntil == 0 {
					t.Fatal("stealth ended too early")
				}
				w.Step()
			case "attack":
				if !w.Primary("rogue", 2, 0) {
					t.Fatal("dagger attack rejected")
				}
			case "damage":
				w.damage(w.players["rogue"], 1)
			}
			if w.players["rogue"].InvisibleUntil != 0 {
				t.Fatal("stealth was not cleared")
			}
		})
	}
}

func TestBerserkerHeal(t *testing.T) {
	for _, tc := range []struct{ start, want int }{
		{1, 123}, {43, 123}, {44, 123}, {100, 145}, {174, 175}, {175, 175},
	} {
		w := testWorld(t)
		combatSpawn(t, w, "berserker", Vec2{}, Berserker)
		combatSpawn(t, w, "enemy", Vec2{X: 1.5}, Knight)
		p := w.players["berserker"]
		p.HP = tc.start
		used := w.Secondary(p.ID, 1, 0, nil)
		if used != (tc.start < p.MaxHP) || p.HP != tc.want {
			t.Fatalf("started at %d HP: used=%t, got %d HP, want %d", tc.start, used, p.HP, tc.want)
		}
		hp(t, w, "enemy", 130)
		if used && (p.SecondaryReadyTick != 20*TickRate || w.Secondary(p.ID, 2, 0, nil)) {
			t.Fatal("heal cooldown was not enforced")
		}
		if !used && p.SecondaryReadyTick != 0 {
			t.Fatal("full-health attempt spent cooldown")
		}
	}
}

func TestHeroHealthOnSpawnAndRespawn(t *testing.T) {
	for _, tc := range []struct {
		hero Hero
		want int
	}{{Knight, 130}, {Archer, 105}, {Mage, 105}, {Rogue, 120}, {Berserker, 175}} {
		t.Run(string(tc.hero), func(t *testing.T) {
			w := testWorld(t)
			combatSpawn(t, w, "player", Vec2{}, tc.hero)
			hp(t, w, "player", tc.want)
			w.damage(w.players["player"], tc.want)
			for range respawnDelay {
				w.Step()
			}
			hp(t, w, "player", tc.want)
		})
	}
}

func TestBardInspiration(t *testing.T) {
	w := testWorld(t)
	for _, p := range []struct {
		id   string
		x    float64
		hero Hero
		team Team
	}{
		{"bard", 0, Bard, Blue}, {"other-bard", 0, Bard, Blue},
		{"knight", 1, Knight, Blue}, {"archer", 2, Archer, Blue},
		{"far", 6, Mage, Blue}, {"enemy", 3, Mage, Red},
	} {
		if err := w.spawn(p.id, Vec2{X: p.x}, p.hero, p.team); err != nil {
			t.Fatal(err)
		}
	}
	if !w.Secondary("bard", 1, 0, nil) {
		t.Fatal("inspiration rejected")
	}
	knight, archer := w.players["knight"], w.players["archer"]
	if knight.InspirationSpeedUntil != 2*TickRate || archer.InspirationSpeedUntil != 2*TickRate ||
		knight.InspirationCharges != 3 || archer.InspirationCharges != 5 ||
		w.players["bard"].InspirationCharges != 5 ||
		w.players["far"].InspirationCharges != 0 || w.players["enemy"].InspirationCharges != 0 {
		t.Fatal("wrong inspiration recipients or charge counts")
	}
	if !w.Primary("knight", 1, 0) || !w.Primary("archer", 1, 0) {
		t.Fatal("expected both attacks to start")
	}
	if knight.Attack.HitTick != 6 || knight.PrimaryReadyTick != 10 || knight.InspirationCharges != 2 ||
		archer.Attack.HitTick != 1 || archer.PrimaryReadyTick != 5 || archer.InspirationCharges != 4 {
		t.Fatal("wrong inspired attack timing or charge use")
	}
	if w.Primary("knight", 2, 0) || knight.InspirationCharges != 2 {
		t.Fatal("rejected attack must not consume a charge")
	}
	if !w.Secondary("other-bard", 1, 0, nil) ||
		knight.InspirationCharges != 3 || archer.InspirationCharges != 5 ||
		knight.Attack.HitTick != 6 || knight.PrimaryReadyTick != 10 {
		t.Fatal("second bard must replenish charges without stacking or changing an ongoing attack")
	}
	w.damage(knight, knight.HP)
	if knight.InspirationCharges != 0 || knight.InspirationSpeedUntil != 0 {
		t.Fatal("inspiration must clear on death")
	}
}
