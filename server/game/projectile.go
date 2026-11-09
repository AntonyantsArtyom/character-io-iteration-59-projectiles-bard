package game

import "math"

const projectileHitRadius = PlayerRadius + 0.08

type Projectile struct {
	ActionID        uint64
	LaunchTick      uint64
	LaunchPosition  Vec2
	Aim             float64
	Speed           float64
	PreviewDistance float64
	Team            Team
	ID              uint64
	OwnerID         string
	Hero            Hero
	Position        Vec2
}

// ProjectileEnd is repeated in recent snapshots so dropped snapshots do not hide impacts.
type ProjectileEnd struct {
	ActionID   uint64  `json:"actionId"`
	LaunchTick uint64  `json:"launchTick"`
	LaunchX    float64 `json:"launchX"`
	LaunchZ    float64 `json:"launchZ"`
	Aim        float64 `json:"aim"`
	ID         uint64  `json:"id"`
	OwnerID    string  `json:"ownerId"`
	Hero       Hero    `json:"hero"`
	Tick       uint64  `json:"tick"`
	X          float64 `json:"x"`
	Z          float64 `json:"z"`
	Reason     string  `json:"reason"`
}

func (w *World) ProjectileEnds() []ProjectileEnd {
	return append([]ProjectileEnd(nil), w.projectileEnds...)
}

func (w *World) endProjectile(p shot, distance float64, reason string) {
	w.projectileEnds = append(w.projectileEnds, ProjectileEnd{
		ID: p.ID, ActionID: p.ActionID, OwnerID: p.OwnerID, Hero: p.Hero, Tick: w.tick, Reason: reason,
		LaunchTick: p.LaunchTick, LaunchX: p.LaunchPosition.X, LaunchZ: p.LaunchPosition.Z, Aim: p.Aim,
		X: p.Position.X + p.dir.X*distance, Z: p.Position.Z + p.dir.Z*distance,
	})
}

type shot struct {
	Projectile
	dir         Vec2
	left, speed float64
	damage      int
}

func (w *World) Projectiles() []Projectile {
	result := make([]Projectile, 0, len(w.projectiles))
	for _, p := range w.projectiles {
		preview := p.Projectile
		preview.Speed = p.speed
		_, xExit := rayAxis(p.Position.X, p.dir.X, w.bounds.Min.X, w.bounds.Max.X)
		_, zExit := rayAxis(p.Position.Z, p.dir.Z, w.bounds.Min.Z, w.bounds.Max.Z)
		preview.PreviewDistance = max(0, min(p.left, xExit, zExit, w.wallHit(p.Position, p.dir, p.left)))
		result = append(result, preview)
	}
	return result
}

func (w *World) fire(ownerID string, team Team, hero Hero, actionID uint64, position, dir Vec2, spec attackSpec) {
	w.nextProjectile++
	w.projectiles = append(w.projectiles, shot{
		// The first segment is advanced in this step. The origin is its opening sample.
		Projectile: Projectile{ActionID: actionID, LaunchTick: w.tick - 1, LaunchPosition: position, Aim: math.Atan2(dir.Z, dir.X), ID: w.nextProjectile, OwnerID: ownerID, Team: team, Hero: hero, Position: position},
		dir:        dir, left: spec.reach, speed: spec.speed, damage: spec.damage,
	})
}

func (w *World) stepProjectiles(players []*playerState) {
	active := w.projectiles[:0]
	for _, p := range w.projectiles {
		_, xExit := rayAxis(p.Position.X, p.dir.X, w.bounds.Min.X, w.bounds.Max.X)
		_, zExit := rayAxis(p.Position.Z, p.dir.Z, w.bounds.Min.Z, w.bounds.Max.Z)
		remaining := min(p.left, xExit, zExit)
		distance := min(p.speed/TickRate, remaining)
		first := w.wallHit(p.Position, p.dir, distance)
		var target *playerState
		for _, candidate := range players {
			if !canDamage(p.OwnerID, p.Team, candidate) {
				continue
			}
			hit := circleHit(p.Position, p.dir, candidate.Position)
			if hit > distance || hit >= first {
				continue
			}
			// The larger hit radius can overlap cover. The contact point still needs a clear path to the target.
			contact := Vec2{X: p.Position.X + p.dir.X*hit, Z: p.Position.Z + p.dir.Z*hit}
			dx, dz := candidate.Position.X-contact.X, candidate.Position.Z-contact.Z
			gap := math.Hypot(dx, dz)
			if gap > 0 && w.wallHit(contact, Vec2{X: dx / gap, Z: dz / gap}, gap) <= gap {
				continue
			}
			first, target = hit, candidate
		}
		if target != nil {
			w.damageFrom(p.OwnerID, target, p.damage)
			w.endProjectile(p, first, "hit")
			continue
		}
		if first <= distance {
			w.endProjectile(p, first, "wall")
			continue
		}
		if distance >= remaining {
			w.endProjectile(p, distance, "expired")
			continue
		}
		p.left -= distance
		p.Position.X += p.dir.X * distance
		p.Position.Z += p.dir.Z * distance
		active = append(active, p)
	}
	w.projectiles = active
}

func circleHit(from, dir, center Vec2) float64 {
	dx, dz := center.X-from.X, center.Z-from.Z
	projection := dx*dir.X + dz*dir.Z
	cross := dx*dir.Z - dz*dir.X
	discriminant := projectileHitRadius*projectileHitRadius - cross*cross
	if discriminant < 0 {
		return math.Inf(1)
	}
	halfChord := math.Sqrt(discriminant)
	if projection+halfChord < 0 {
		return math.Inf(1)
	}
	return max(0, projection-halfChord)
}

func (w *World) wallHit(from, dir Vec2, distance float64) float64 {
	first := math.Inf(1)
	for _, wall := range w.walls {
		x0, x1 := rayAxis(from.X, dir.X, wall.Min.X, wall.Max.X)
		z0, z1 := rayAxis(from.Z, dir.Z, wall.Min.Z, wall.Max.Z)
		enter := max(0, x0, z0)
		if enter <= min(distance, x1, z1) {
			first = min(first, enter)
		}
	}
	return first
}

func rayAxis(origin, direction, lo, hi float64) (float64, float64) {
	if direction == 0 {
		if origin < lo || origin > hi {
			return math.Inf(1), math.Inf(-1)
		}
		return math.Inf(-1), math.Inf(1)
	}
	a, b := (lo-origin)/direction, (hi-origin)/direction
	return min(a, b), max(a, b)
}
