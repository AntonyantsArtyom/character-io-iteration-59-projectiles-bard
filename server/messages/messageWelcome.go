package messages

type ProjectilePrediction struct {
	Speed  float64 `json:"speed"`
	Reach  float64 `json:"reach"`
	Windup uint64  `json:"windup"`
}

type Welcome struct {
	CompactSnapshots          bool                            `json:"compactSnapshots,omitempty"`
	ProjectilePrediction      map[string]ProjectilePrediction `json:"projectilePrediction"`
	InspirationMoveMultiplier float64                         `json:"inspirationMoveMultiplier"`
	WindupEnemyRadius         float64                         `json:"windupEnemyRadius"`
	Zones                     []ControlZone                   `json:"zones"`
	WinScore                  int                             `json:"winScore"`
	Team                      string                          `json:"team"`
	Type                      string                          `json:"type"`
	PlayerID                  string                          `json:"playerId"`
	TickRate                  int                             `json:"tickRate"`
	MoveSpeed                 float64                         `json:"moveSpeed"`
	PlayerRadius              float64                         `json:"playerRadius"`
	Bounds                    Rect                            `json:"bounds"`
	Walls                     []Rect                          `json:"walls"`
}
