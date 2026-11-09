package game

import (
	"cmp"
	"math"
	"slices"
)

type Hero string

const (
	Knight       Hero = "knight"
	Archer       Hero = "archer"
	Mage         Hero = "mage"
	Rogue        Hero = "rogue"
	Berserker    Hero = "berserker"
	Bard         Hero = "bard"
	MaxHP             = 130
	respawnDelay      = 5 * TickRate / 2
)

func (h Hero) Valid() bool {
	_, ok := attacks[h]
	return ok
}

type attackSpec struct {
	damage           int
	windup, cooldown uint64
	reach, speed     float64
	sectorDot        float64
	liveHit          bool
}

var attacks = map[Hero]attackSpec{
	Knight:    {damage: 60, windup: 15, cooldown: 25, reach: 1.8, sectorDot: 0.5, liveHit: true},
	Archer:    {damage: 22, windup: 1, cooldown: 12, reach: 12, speed: 28},
	Mage:      {damage: 28, windup: 2, cooldown: 17, reach: 10, speed: 22},
	Rogue:     {damage: 22, windup: 4, cooldown: 13, reach: 1.45, sectorDot: 0.79},
	Berserker: {damage: 100, windup: 21, cooldown: 34, reach: 1.9, sectorDot: 0.25, liveHit: true},
	Bard:      {damage: 16, windup: 2, cooldown: 16, reach: 8, speed: 18},
}

func ProjectileSpec(hero Hero) (speed, reach float64, windup uint64) {
	spec := attacks[hero]
	return spec.speed, spec.reach, spec.windup
}

const (
	hitRewindTicks            = TickRate / 5
	InspirationMoveMultiplier = 1.30
	inspiredAttackTimePercent = 40
)

type pastPosition struct {
	tick     uint64
	position Vec2
	alive    bool
}

func (w *World) attackTick(viewTick uint64) uint64 {
	if viewTick == 0 || viewTick >= w.tick {
		return w.tick
	}
	if w.tick-viewTick > hitRewindTicks {
		return w.tick - hitRewindTicks
	}
	return viewTick
}

func targetAt(p *playerState, tick uint64) Vec2 {
	past := p.history[tick%uint64(len(p.history))]
	if past.tick == tick && past.alive {
		return past.position
	}
	return p.Position
}

type DashPath struct {
	From, To Vec2
}

type Attack struct {
	WindupMoveMultiplier float64
	Dash                 *DashPath
	targetTick           uint64
	ID                   uint64
	StartTick            uint64
	HitTick              uint64
	Aim                  float64
}

// Primary consumes each valid action ID once, including attempts during cooldown.
func (w *World) Primary(id string, actionID uint64, aim float64) bool {
	return w.PrimaryAt(id, actionID, aim, 0)
}

func (w *World) PrimaryAt(id string, actionID uint64, aim float64, viewTick uint64) bool {
	p := w.players[id]
	if p == nil || actionID <= p.lastAction || !finite(aim) {
		return false
	}
	p.lastAction = actionID
	if w.round.RestartTick != 0 || p.HP == 0 || w.tick < p.PrimaryReadyTick {
		return false
	}
	p.InvisibleUntil = 0
	spec := attacks[p.Hero]
	windup := spec.windup
	recovery := spec.cooldown - spec.windup
	if p.InspirationCharges > 0 {
		p.InspirationCharges--
		if bard := w.players[p.inspirationOwnerID]; bard != nil && bard != p {
			bard.Stats.InspiredAttacks++
		}
		recovery = (inspiredAttackTimePercent*recovery + 50) / 100
		if spec.speed == 0 {
			windup = (inspiredAttackTimePercent*windup + 50) / 100
		}
	}
	p.Attack = Attack{ID: actionID, StartTick: w.tick, HitTick: w.tick + windup, Aim: aim, targetTick: w.attackTick(viewTick) + windup}
	if spec.speed == 0 {
		p.Attack.WindupMoveMultiplier = 1.25
	}
	p.PrimaryReadyTick = w.tick + windup + recovery
	p.pending = true
	return true
}

func (w *World) stepCombat() {
	if len(w.projectiles) == 0 && len(w.meteors) == 0 {
		hasPending := false
		for _, p := range w.players {
			if p.pending {
				hasPending = true
				break
			}
		}
		if !hasPending {
			return
		}
	}
	players := w.combatPlayers()

	for _, p := range players {
		if !p.pending || w.tick < p.Attack.HitTick {
			continue
		}
		p.pending = false
		spec := attacks[p.Hero]
		dir := direction(p.Attack.Aim)
		if spec.speed > 0 {
			w.fire(p.ID, p.Team, p.Hero, p.Attack.ID, p.Position, dir, spec)
			continue
		}
		targetTick := p.Attack.targetTick
		if spec.liveHit {
			// A defender who leaves the telegraphed arc before impact dodges.
			targetTick = w.tick
		}
		w.hitSector(p, players, dir, spec.reach, spec.sectorDot, spec.damage, targetTick)
	}
	w.stepProjectiles(players)
	w.stepMeteors(players)
}

func (w *World) combatPlayers() []*playerState {
	players := make([]*playerState, 0, len(w.players))
	for _, p := range w.players {
		players = append(players, p)
	}
	slices.SortFunc(players, func(a, b *playerState) int { return cmp.Compare(a.ID, b.ID) })
	return players
}

func direction(aim float64) Vec2 {
	return Vec2{X: math.Cos(aim), Z: math.Sin(aim)}
}

func (w *World) hitSector(p *playerState, players []*playerState, dir Vec2, reach, sectorDot float64, damage int, targetTick uint64) {
	for _, target := range players {
		if !canDamage(p.ID, p.Team, target) {
			continue
		}
		if !w.inSector(p.Position, targetAt(target, targetTick), dir, reach, sectorDot) {
			continue
		}
		w.damageFrom(p.ID, target, damage)
	}

}

func (w *World) inSector(from, target, dir Vec2, reach, sectorDot float64) bool {
	dx, dz := target.X-from.X, target.Z-from.Z
	distance := math.Hypot(dx, dz)
	if distance > reach+PlayerRadius {
		return false
	}
	if sectorDot > -1 {
		forward := dx*dir.X + dz*dir.Z
		if forward < distance*sectorDot {
			side := math.Abs(dx*dir.Z - dz*dir.X)
			sine := math.Sqrt(1 - sectorDot*sectorDot)
			along := clamp(forward*sectorDot+side*sine, 0, reach)
			if math.Hypot(forward-along*sectorDot, side-along*sine) > PlayerRadius {
				return false
			}
		}
	}
	return distance == 0 || w.wallHit(from, Vec2{dx / distance, dz / distance}, distance) > distance
}

func canDamage(ownerID string, team Team, target *playerState) bool {
	return target.HP > 0 && hostile(ownerID, team, target.ID, target.Team)
}

func hostile(ownerID string, team Team, targetID string, targetTeam Team) bool {
	return targetID != ownerID && (team == "" || targetTeam != team)
}

func (w *World) damageFrom(ownerID string, target *playerState, amount int) {
	w.damage(target, amount)
	if owner := w.players[ownerID]; target.HP == 0 && owner != nil {
		owner.Stats.Kills++
	}
}

func (w *World) damage(p *playerState, amount int) {
	p.InvisibleUntil = 0
	p.HP = max(0, p.HP-amount)
	if p.HP == 0 {
		p.Stats.Deaths++
		p.inspirationOwnerID = ""
		p.pending = false
		p.regenTicks = 0
		p.Attack = Attack{}
		p.InspirationCharges = 0
		p.InspirationSpeedUntil = 0
		p.input = Input{Aim: p.Aim}
		p.RespawnTick = w.tick + respawnDelay
	}
}

// Secondary abilities and meteor impacts.
const (
	secondaryCooldown     = 13 * TickRate / 2
	berserkerHealCooldown = 20 * TickRate
	bardCooldown          = 15 * TickRate
	bardRadius            = 5.0
	dashDistance          = 4.2
	fanReach              = 4.0
	fanSectorDot          = math.Sqrt2 / 2
	fanDamage             = 40
	meteorReach           = 7.0
	meteorRadius          = 1.8
	meteorDamage          = 61
	meteorDelay           = 12
)

type Meteor struct {
	Team       Team
	ID         uint64
	OwnerID    string
	Position   Vec2
	ImpactTick uint64
	Radius     float64
}

// Secondary shares the action sequence with Primary and has its own cooldown.
// Only the mage uses target. A cast outside the allowed area consumes the ID.
func (w *World) Secondary(id string, actionID uint64, aim float64, target *Vec2) bool {
	return w.SecondaryAt(id, actionID, aim, target, 0)
}

func (w *World) SecondaryAt(id string, actionID uint64, aim float64, target *Vec2, viewTick uint64) bool {
	p := w.players[id]
	if p == nil || actionID <= p.lastAction || !finite(aim) {
		return false
	}
	p.lastAction = actionID
	if w.round.RestartTick != 0 || p.HP == 0 || w.tick < p.SecondaryReadyTick {
		return false
	}
	if p.Hero == Mage && (target == nil || !finite(target.X) || !finite(target.Z) ||
		target.X < w.bounds.Min.X || target.X > w.bounds.Max.X ||
		target.Z < w.bounds.Min.Z || target.Z > w.bounds.Max.Z ||
		math.Hypot(target.X-p.Position.X, target.Z-p.Position.Z) > meteorReach) {
		return false
	}

	cooldown := uint64(secondaryCooldown)
	if p.Hero == Berserker {
		if p.HP == p.MaxHP {
			return false
		}
		cooldown = berserkerHealCooldown
	}

	if p.Hero == Bard {
		cooldown = bardCooldown
	}
	p.SecondaryReadyTick = w.tick + cooldown
	p.SecondaryAttack = Attack{ID: actionID, StartTick: w.tick, HitTick: w.tick, Aim: aim}
	switch p.Hero {
	case Knight:
		dir := direction(aim)
		from := p.Position
		distance := w.travelClearance(from, dir, dashDistance)
		p.Position.X += dir.X * distance
		p.Position.Z += dir.Z * distance
		if distance > 0 {
			p.SecondaryAttack.Dash = &DashPath{From: from, To: p.Position}
			p.history = [hitRewindTicks + 1]pastPosition{}
		}
	case Archer:
		w.hitSector(p, w.combatPlayers(), direction(aim), fanReach, fanSectorDot, fanDamage, w.attackTick(viewTick))
	case Rogue:
		p.InvisibleUntil = w.tick + 5*TickRate
	case Berserker:
		if p.HP*4 < p.MaxHP {
			p.HP = (7*p.MaxHP + 5) / 10 // Restore to 70%, rounded to the nearest HP.
		} else {
			p.HP += (3*(p.MaxHP-p.HP) + 2) / 5 // Restore 60% of missing HP.
		}
	case Bard:
		for _, ally := range w.players {
			if ally.HP == 0 || (ally != p && hostile(p.ID, p.Team, ally.ID, ally.Team)) ||
				math.Hypot(ally.Position.X-p.Position.X, ally.Position.Z-p.Position.Z) > bardRadius {
				continue
			}
			ally.InspirationSpeedUntil = w.tick + 2*TickRate
			ally.inspirationOwnerID = p.ID
			ally.InspirationCharges = 5
			if attacks[ally.Hero].speed == 0 {
				ally.InspirationCharges = 3
			}
		}
		if p.HP == p.MaxHP {
			p.regenTicks = 0
		}
	case Mage:
		p.SecondaryAttack.HitTick += meteorDelay
		w.meteors = append(w.meteors, Meteor{
			ID: actionID, OwnerID: id, Team: p.Team, Position: *target,
			ImpactTick: p.SecondaryAttack.HitTick, Radius: meteorRadius,
		})
	}
	return true
}

func (w *World) Meteors() []Meteor {
	return append([]Meteor(nil), w.meteors...)
}

func (w *World) stepMeteors(players []*playerState) {
	pending := w.meteors[:0]
	for _, m := range w.meteors {
		if w.tick < m.ImpactTick {
			pending = append(pending, m)
			continue
		}
		for _, p := range players {
			if canDamage(m.OwnerID, m.Team, p) &&
				math.Hypot(p.Position.X-m.Position.X, p.Position.Z-m.Position.Z) <= m.Radius {
				w.damageFrom(m.OwnerID, p, meteorDamage)
			}
		}

	}
	w.meteors = pending
}

// The expanded wall is the union of two strips and four round corners.
func (w *World) travelClearance(from, dir Vec2, limit float64) float64 {
	const r = PlayerRadius
	_, xExit := rayAxis(from.X, dir.X, w.bounds.Min.X+r, w.bounds.Max.X-r)
	_, zExit := rayAxis(from.Z, dir.Z, w.bounds.Min.Z+r, w.bounds.Max.Z-r)
	distance := min(limit, xExit, zExit)
	for _, wall := range w.walls {
		distance = min(distance, stripHit(from, dir, Rect{
			Min: Vec2{wall.Min.X - r, wall.Min.Z}, Max: Vec2{wall.Max.X + r, wall.Max.Z},
		}), stripHit(from, dir, Rect{
			Min: Vec2{wall.Min.X, wall.Min.Z - r}, Max: Vec2{wall.Max.X, wall.Max.Z + r},
		}))
		for _, x := range [...]float64{wall.Min.X, wall.Max.X} {
			for _, z := range [...]float64{wall.Min.Z, wall.Max.Z} {
				dx, dz := x-from.X, z-from.Z
				projection := dx*dir.X + dz*dir.Z
				cross := dx*dir.Z - dz*dir.X
				if chord := r*r - cross*cross; chord > 0 {
					half := math.Sqrt(chord)
					if projection+half > 1e-9 {
						distance = min(distance, max(0, projection-half))
					}
				}
			}
		}
	}
	return max(0, distance)
}

func stripHit(from, dir Vec2, r Rect) float64 {
	if (dir.X == 0 && (from.X <= r.Min.X || from.X >= r.Max.X)) ||
		(dir.Z == 0 && (from.Z <= r.Min.Z || from.Z >= r.Max.Z)) {
		return math.Inf(1)
	}
	x0, x1 := rayAxis(from.X, dir.X, r.Min.X, r.Max.X)
	z0, z1 := rayAxis(from.Z, dir.Z, r.Min.Z, r.Max.Z)
	entry := max(0, x0, z0)
	if entry < min(x1, z1) {
		return entry
	}
	return math.Inf(1)
}
