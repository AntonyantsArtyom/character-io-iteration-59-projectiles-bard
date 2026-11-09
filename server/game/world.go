package game

import (
	"fmt"
	"math"
	"slices"
)

const (
	TickRate          = 30
	PlayerRadius      = 0.35
	WindupEnemyRadius = 4.0
)

var movementSpeeds = map[Hero]float64{
	Knight:    6.9,
	Archer:    6.5,
	Mage:      6.2,
	Rogue:     7.4,
	Berserker: 5.7,
	Bard:      6.5,
}

func (hero Hero) MoveSpeed() float64 {
	return movementSpeeds[hero]
}

type Vec2 struct {
	X, Z float64
}

type Rect struct {
	Min, Max Vec2
}

type Input struct {
	Move Vec2
	Aim  float64
}

func (input Input) Valid() bool {
	return finite(input.Move.X) && finite(input.Move.Z) && finite(input.Aim) &&
		math.Abs(input.Move.X) <= 1 && math.Abs(input.Move.Z) <= 1
}

type Team string

const (
	Blue Team = "blue"
	Red  Team = "red"
)

type Player struct {
	Stats                 RoundStats
	Bot                   bool
	Team                  Team
	ID                    string
	Position              Vec2
	Aim                   float64
	Hero                  Hero
	HP                    int
	MaxHP                 int
	InvisibleUntil        uint64
	Attack                Attack
	RespawnTick           uint64
	PrimaryReadyTick      uint64
	SecondaryReadyTick    uint64
	InspirationCharges    int
	InspirationSpeedUntil uint64
	SecondaryAttack       Attack
}

type playerState struct {
	inspirationOwnerID string
	windupEnemyNearby  bool
	regenTicks         int
	Player
	input      Input
	spawn      Vec2
	lastAction uint64
	pending    bool
	history    [hitRewindTicks + 1]pastPosition
}

type World struct {
	bounds         Rect
	walls          []Rect
	players        map[string]*playerState
	tick           uint64
	projectiles    []shot
	projectileEnds []ProjectileEnd
	nextProjectile uint64
	meteors        []Meteor
	botSerial      uint64
	zones          []controlPoint
	round          RoundState
	scoreTick      uint64
}

func NewWorld(bounds Rect, walls []Rect, zones ...ControlZone) (*World, error) {
	if !bounds.valid() || bounds.Max.X-bounds.Min.X < 2*PlayerRadius ||
		bounds.Max.Z-bounds.Min.Z < 2*PlayerRadius {
		return nil, fmt.Errorf("invalid arena bounds")
	}
	for i, wall := range walls {
		if !wall.valid() {
			return nil, fmt.Errorf("invalid wall at index %d", i)
		}
	}
	w := &World{
		bounds:  bounds,
		walls:   slices.Clone(walls),
		players: make(map[string]*playerState),
	}
	if err := w.setControlZones(zones); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *World) Tick() uint64 { return w.tick }

func (w *World) Player(id string) (Player, bool) {
	p, ok := w.players[id]
	if !ok {
		return Player{}, false
	}
	return p.Player, true
}

func (w *World) Spawn(id string, at Vec2) error {
	return w.SpawnHero(id, at, Knight)
}

func (w *World) SpawnHero(id string, at Vec2, hero Hero) error {
	return w.spawn(id, at, hero, "")
}

// Join assigns the smaller team using the players currently in the world.
func (w *World) Join(id string, hero Hero, blueSpawns, redSpawns []Vec2) (Team, error) {
	blue, red := 0, 0
	for _, p := range w.players {
		if p.Bot {
			continue
		}
		switch p.Team {
		case Blue:
			blue++
		case Red:
			red++
		}
	}
	team, spawns, members := Blue, blueSpawns, blue
	if red < blue {
		team, spawns, members = Red, redSpawns, red
	}
	if len(spawns) == 0 {
		return "", fmt.Errorf("missing %s spawn points", team)
	}
	if err := w.spawn(id, spawns[members%len(spawns)], hero, team); err != nil {
		return "", err
	}
	return team, nil
}

func (w *World) spawn(id string, at Vec2, hero Hero, team Team) error {
	if !hero.Valid() {
		return fmt.Errorf("invalid hero %q", hero)
	}
	if id == "" {
		return fmt.Errorf("empty player ID")
	}
	if _, exists := w.players[id]; exists {
		return fmt.Errorf("player %q already exists", id)
	}
	if !w.canSpawn(at) {
		return fmt.Errorf("invalid spawn position for %q", id)
	}
	health := MaxHP
	switch hero {
	case Archer, Mage:
		health = 105
	case Rogue:
		health = 120
	case Berserker:
		health = 175
	case Bard:
		health = 85
	}
	w.players[id] = &playerState{Player: Player{ID: id, Position: at, Hero: hero, Team: team, HP: health, MaxHP: health}, spawn: at}
	return nil
}

func (w *World) Remove(id string) {
	delete(w.players, id)
}

func (w *World) SetInput(id string, input Input) error {
	p, ok := w.players[id]
	if !ok {
		return fmt.Errorf("unknown player %q", id)
	}
	if !input.Valid() {
		return fmt.Errorf("invalid input for %q", id)
	}
	if length := math.Hypot(input.Move.X, input.Move.Z); length > 1 {
		input.Move.X /= length
		input.Move.Z /= length
	}
	if p.HP == 0 {
		return nil
	}
	p.input = input
	if p.pending && p.Attack.WindupMoveMultiplier > 0 {
		p.Attack.Aim = input.Aim
	}
	return nil
}

// Step advances the arena by one fixed tick. The last input stays active
// until the caller replaces it.
func (w *World) Step() {
	w.tick++
	for len(w.projectileEnds) > 0 && w.tick-w.projectileEnds[0].Tick >= 2*TickRate {
		w.projectileEnds = w.projectileEnds[1:]
	}
	if w.round.RestartTick != 0 {
		if w.tick >= w.round.RestartTick {
			w.restartRound()
		}
		return
	}
	// Sample proximity before anyone moves, so map iteration order cannot change the bonus.
	for _, p := range w.players {
		if p.pending && p.Attack.WindupMoveMultiplier > 0 {
			p.windupEnemyNearby = w.hasWindupEnemy(p)
		}
	}
	for _, p := range w.players {
		if p.HP == 0 {
			if w.tick >= p.RespawnTick {
				p.Position, p.HP, p.RespawnTick = p.spawn, p.MaxHP, 0
				p.history = [hitRewindTicks + 1]pastPosition{}
			}
			continue
		}
		if p.HP < p.MaxHP {
			interval := TickRate / 5
			if attacks[p.Hero].speed > 0 {
				interval *= 2
			}
			p.regenTicks++
			if p.regenTicks >= interval {
				p.HP++
				p.regenTicks = 0
			}
		} else {
			p.regenTicks = 0
		}
		if p.InvisibleUntil <= w.tick {
			p.InvisibleUntil = 0
		}
		multiplier := 1.0
		if w.tick <= p.InspirationSpeedUntil {
			multiplier = InspirationMoveMultiplier
		}
		if p.pending && p.windupEnemyNearby {
			multiplier = max(multiplier, p.Attack.WindupMoveMultiplier)
		}
		distancePerTick := p.Hero.MoveSpeed() * multiplier / TickRate
		p.Position.X = w.moveAxis(p.Position, p.input.Move.X*distancePerTick, true)
		p.Position.Z = w.moveAxis(p.Position, p.input.Move.Z*distancePerTick, false)
		p.Aim = p.input.Aim
	}
	w.stepCombat()
	w.stepControl()
	for _, p := range w.players {
		p.history[w.tick%uint64(len(p.history))] = pastPosition{tick: w.tick, position: p.Position, alive: p.HP > 0}
	}
}

func (w *World) hasWindupEnemy(p *playerState) bool {
	for _, target := range w.players {
		if !canDamage(p.ID, p.Team, target) || target.InvisibleUntil > w.tick {
			continue
		}
		dx, dz := p.Position.X-target.Position.X, p.Position.Z-target.Position.Z
		if dx*dx+dz*dz <= WindupEnemyRadius*WindupEnemyRadius {
			return true
		}
	}
	return false
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func (r Rect) valid() bool {
	return finite(r.Min.X) && finite(r.Min.Z) && finite(r.Max.X) && finite(r.Max.Z) &&
		r.Min.X < r.Max.X && r.Min.Z < r.Max.Z
}
