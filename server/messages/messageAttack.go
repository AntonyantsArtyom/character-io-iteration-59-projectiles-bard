package messages

type Attack struct {
	WindupMoveMultiplier float64   `json:"windupMoveMultiplier,omitempty"`
	Dash                 *DashPath `json:"dash,omitempty"`
	ID                   uint64    `json:"id"`
	StartTick            uint64    `json:"startTick"`
	HitTick              uint64    `json:"hitTick"`
	Aim                  float64   `json:"aim"`
}
