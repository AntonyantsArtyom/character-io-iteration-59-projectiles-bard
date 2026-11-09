package main

import "arena-backend/game"

// Each team gets the same cover on its approach to the three plazas.
func courtyardCover(zones []game.ControlZone) []game.Rect {
	blue := []game.Rect{
		// Broken gate on the north route, with room on either side.
		{Min: game.Vec2{X: -15, Z: -21}, Max: game.Vec2{X: -12, Z: -17}},
		{Min: game.Vec2{X: -15, Z: -13}, Max: game.Vec2{X: -12, Z: -10}},
		// Staggered cover lets melee cross the central approach in stages.
		{Min: game.Vec2{X: -23, Z: -4}, Max: game.Vec2{X: -19, Z: 1}},
		{Min: game.Vec2{X: -10, Z: 1}, Max: game.Vec2{X: -7, Z: 4}},
		// Planters split the south approach into an inner and an outer route.
		{Min: game.Vec2{X: -18, Z: 12}, Max: game.Vec2{X: -12, Z: 14}},
		{Min: game.Vec2{X: -10, Z: 19}, Max: game.Vec2{X: -6, Z: 22}},
	}
	// Screen the center of each plaza from the main lane while keeping
	// the north and south approaches open around both ends of the cover.
	for _, zone := range zones {
		z := zone.Position.Z
		blue = append(blue, game.Rect{
			Min: game.Vec2{X: -zone.Radius + 0.6, Z: z - 1.65},
			Max: game.Vec2{X: -zone.Radius + 1.8, Z: z + 1.65},
		})
	}
	walls := append([]game.Rect(nil), blue...)
	for _, wall := range blue {
		walls = append(walls, game.Rect{
			Min: game.Vec2{X: -wall.Max.X, Z: wall.Min.Z},
			Max: game.Vec2{X: -wall.Min.X, Z: wall.Max.Z},
		})
	}
	return walls
}
