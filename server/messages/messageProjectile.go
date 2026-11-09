package messages

type Projectile struct {
	ActionID   uint64  `json:"actionId"`
	LaunchTick uint64  `json:"launchTick"`
	LaunchX    float64 `json:"launchX"`
	LaunchZ    float64 `json:"launchZ"`
	Aim        float64 `json:"aim"`
	Team       string  `json:"team"`
	ID         uint64  `json:"id"`
	OwnerID    string  `json:"ownerId"`
	Hero       string  `json:"hero"`
	X          float64 `json:"x"`
	Z          float64 `json:"z"`
}
