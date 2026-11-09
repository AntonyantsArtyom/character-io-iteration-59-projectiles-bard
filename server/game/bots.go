package game

import (
	"cmp"
	"fmt"
	"math"
	"slices"
)

var botHeroes = [...]Hero{Knight, Archer, Mage, Rogue, Berserker, Bard}

func (w *World) Bots() []Player {
	var bots []Player
	for _, p := range w.players {
		if p.Bot {
			bots = append(bots, p.Player)
		}
	}
	slices.SortFunc(bots, func(a, b Player) int { return cmp.Compare(a.ID, b.ID) })
	return bots
}

// FillBots counts connected players, including those waiting for respawn.
func (w *World) FillBots(target int, blueSpawns, redSpawns []Vec2) {
	var blue, red int
	for _, p := range w.players {
		if p.Bot {
			continue
		}
		if p.Team == Red {
			red++
		} else {
			blue++
		}
	}
	var blueBots, redBots int
	for blue+red+blueBots+redBots < target {
		if blue+blueBots <= red+redBots {
			blueBots++
		} else {
			redBots++
		}
	}
	bots := w.Bots()
	w.fillTeamBots(bots, Blue, blueBots, blueSpawns)
	w.fillTeamBots(bots, Red, redBots, redSpawns)
}

func (w *World) fillTeamBots(bots []Player, team Team, wanted int, spawns []Vec2) {
	count := 0
	for _, bot := range bots {
		if bot.Team == team {
			if count >= wanted {
				w.Remove(bot.ID)
			}
			count++
		}
	}
	for count < wanted && len(spawns) > 0 {
		w.botSerial++
		id := fmt.Sprintf("bot-%d", w.botSerial)
		hero := botHeroes[(w.botSerial-1)%uint64(len(botHeroes))]
		if err := w.spawn(id, spawns[count%len(spawns)], hero, team); err != nil {
			return
		}
		w.players[id].Bot = true
		count++
	}
}

func (w *World) ThinkBots() {
	if w.tick%4 != 0 || w.round.RestartTick != 0 {
		return
	}
	assigned := map[Team]int{}
	for _, bot := range w.Bots() {
		index := assigned[bot.Team]
		assigned[bot.Team]++
		p := w.players[bot.ID]
		if p.HP == 0 {
			continue
		}
		if p.Hero == Berserker && p.HP*4 < p.MaxHP && w.tick >= p.SecondaryReadyTick {
			w.Secondary(p.ID, p.lastAction+1, p.Aim, nil)
		}
		var target *playerState
		distance := math.Inf(1)
		for _, candidate := range w.players {
			if !canDamage(p.ID, p.Team, candidate) || candidate.InvisibleUntil > w.tick {
				continue
			}
			d := botDistance(p.Position, candidate.Position)
			if d < distance || (d == distance && target != nil && candidate.ID < target.ID) {
				target, distance = candidate, d
			}
		}
		var objective *ControlZone
		if len(w.zones) > 0 {
			objective = &w.zones[index%len(w.zones)].ControlZone
		}
		if target == nil && objective != nil {
			w.moveBotToZone(p, *objective, p.Aim)
			continue
		}
		if target == nil {
			_ = w.SetInput(p.ID, Input{Aim: p.Aim})
			continue
		}
		w.playBot(p, target, distance, objective)
	}
}

func (w *World) playBot(p, target *playerState, distance float64, objective *ControlZone) {
	dx, dz := target.Position.X-p.Position.X, target.Position.Z-p.Position.Z
	aim := math.Atan2(dz, dx)
	visible := w.wallHit(p.Position, direction(aim), distance) > distance
	spec := attacks[p.Hero]
	move := Vec2{}
	if objective != nil && (botDistance(p.Position, objective.Position) > objective.Radius ||
		botDistance(target.Position, objective.Position) > objective.Radius+2) {
		w.moveBotToZone(p, *objective, aim)
		if visible {
			w.attackWithBot(p, target, distance, aim, spec)
		}
		return
	}
	if !visible || distance > spec.reach*0.82 {
		next := w.botWaypoint(p.Position, target.Position)
		move = direction(math.Atan2(next.Z-p.Position.Z, next.X-p.Position.X))
		if botDistance(p.Position, next) < 0.02 {
			move = Vec2{}
		}
	} else if spec.speed > 0 && distance < 2 {
		dir := direction(aim)
		move = Vec2{-dir.X, -dir.Z}
	}
	_ = w.SetInput(p.ID, Input{Move: move, Aim: aim})
	if !visible {
		return
	}
	w.attackWithBot(p, target, distance, aim, spec)
}

func (w *World) moveBotToZone(p *playerState, zone ControlZone, aim float64) {
	move := Vec2{}
	if botDistance(p.Position, zone.Position) > 0.8 {
		next := w.botWaypoint(p.Position, zone.Position)
		if botDistance(p.Position, next) > 0.02 {
			move = direction(math.Atan2(next.Z-p.Position.Z, next.X-p.Position.X))
		}
	}
	_ = w.SetInput(p.ID, Input{Move: move, Aim: aim})
}

func (w *World) attackWithBot(p, target *playerState, distance, aim float64, spec attackSpec) {
	useSkill := false
	switch p.Hero {
	case Knight:
		useSkill = distance > spec.reach && distance < dashDistance+spec.reach
	case Archer:
		useSkill = distance <= fanReach
	case Mage:
		useSkill = distance <= meteorReach
	case Bard:
		useSkill = distance <= spec.reach && p.InspirationCharges == 0
	case Rogue:
		useSkill = distance > spec.reach && distance < 6 && p.InvisibleUntil == 0
	}
	if useSkill && w.tick >= p.SecondaryReadyTick &&
		w.Secondary(p.ID, p.lastAction+1, aim, &target.Position) {
		return
	}
	if distance <= spec.reach && w.tick >= p.PrimaryReadyTick {
		w.Primary(p.ID, p.lastAction+1, aim)
	}
}

func botDistance(a, b Vec2) float64 {
	return math.Hypot(a.X-b.X, a.Z-b.Z)
}

func (w *World) botSegmentClear(a, b Vec2) bool {
	d := botDistance(a, b)
	return d == 0 || w.travelClearance(a, Vec2{(b.X - a.X) / d, (b.Z - a.Z) / d}, d) >= d-1e-7
}

// Find a short route around wall corners on the current arena's small map.
func (w *World) botWaypoint(from, target Vec2) Vec2 {
	if w.botSegmentClear(from, target) {
		return target
	}
	nodes := []Vec2{from, target}
	const margin = PlayerRadius + 0.22
	for _, wall := range w.walls {
		for _, x := range []float64{wall.Min.X - margin, wall.Max.X + margin} {
			for _, z := range []float64{wall.Min.Z - margin, wall.Max.Z + margin} {
				if point := (Vec2{x, z}); w.canSpawn(point) {
					nodes = append(nodes, point)
				}
			}
		}
	}
	cost := make([]float64, len(nodes))
	first := make([]int, len(nodes))
	visited := make([]bool, len(nodes))
	for i := 1; i < len(nodes); i++ {
		cost[i] = math.Inf(1)
	}
	for {
		current := -1
		best := math.Inf(1)
		for i, c := range cost {
			if !visited[i] && c < best {
				current, best = i, c
			}
		}
		if current == -1 {
			return from
		}
		if current == 1 {
			return nodes[first[current]]
		}
		visited[current] = true
		for i, node := range nodes {
			total := best + botDistance(nodes[current], node)
			if visited[i] || total >= cost[i] || !w.botSegmentClear(nodes[current], node) {
				continue
			}
			cost[i] = total
			first[i] = first[current]
			if current == 0 {
				first[i] = i
			}
		}
	}
}
