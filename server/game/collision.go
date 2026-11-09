package game

import "math"

func (w *World) canSpawn(p Vec2) bool {
	if !finite(p.X) || !finite(p.Z) ||
		p.X < w.bounds.Min.X+PlayerRadius || p.X > w.bounds.Max.X-PlayerRadius ||
		p.Z < w.bounds.Min.Z+PlayerRadius || p.Z > w.bounds.Max.Z-PlayerRadius {
		return false
	}
	for _, wall := range w.walls {
		nearestX := clamp(p.X, wall.Min.X, wall.Max.X)
		nearestZ := clamp(p.Z, wall.Min.Z, wall.Max.Z)
		if math.Hypot(p.X-nearestX, p.Z-nearestZ) < PlayerRadius {
			return false
		}
	}
	return true
}

// moveAxis sweeps the player circle along one axis, stopping at the first wall.
// The two axis sweeps let a player slide along a wall on diagonal input.
func (w *World) moveAxis(p Vec2, delta float64, alongX bool) float64 {
	start, across := axes(p, alongX)
	boundMin, _ := axes(w.bounds.Min, alongX)
	boundMax, _ := axes(w.bounds.Max, alongX)
	end := clamp(start+delta, boundMin+PlayerRadius, boundMax-PlayerRadius)
	for _, wall := range w.walls {
		wallMin, acrossMin := axes(wall.Min, alongX)
		wallMax, acrossMax := axes(wall.Max, alongX)
		gap := math.Abs(across - clamp(across, acrossMin, acrossMax))
		if gap >= PlayerRadius {
			continue
		}
		margin := math.Sqrt(PlayerRadius*PlayerRadius - gap*gap)
		near, far := wallMin-margin, wallMax+margin
		if delta > 0 && start <= wallMin && end > near {
			end = near
		}
		if delta < 0 && start >= wallMax && end < far {
			end = far
		}
	}
	return end
}

func axes(v Vec2, alongX bool) (along, across float64) {
	if alongX {
		return v.X, v.Z
	}
	return v.Z, v.X
}

func clamp(v, lo, hi float64) float64 {
	return max(lo, min(v, hi))
}
