package messages

import "arena-backend/game"

type RoundResultPlayer struct {
	ID   string    `json:"id"`
	Nick string    `json:"nick"`
	Team game.Team `json:"team"`
	Hero game.Hero `json:"hero"`
	Bot  bool      `json:"bot,omitempty"`
	game.RoundStats
}

type Snapshot struct {
	RoundResults   []RoundResultPlayer  `json:"roundResults,omitempty"`
	ProjectileEnds []game.ProjectileEnd `json:"projectileEnds,omitempty"`
	Round          game.RoundState      `json:"round"`
	Zones          []game.ZoneState     `json:"zones"`
	TeamCounts     map[string]int       `json:"teamCounts"`
	Type           string               `json:"type"`
	ServerTick     uint64               `json:"serverTick"`
	LastInputSeq   uint64               `json:"lastInputSeq"`
	Players        []Player             `json:"players"`
	Projectiles    []Projectile         `json:"projectiles"`
	Meteors        []Meteor             `json:"meteors"`
}
