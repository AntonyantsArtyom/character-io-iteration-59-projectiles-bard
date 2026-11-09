package messages

type Meteor struct {
	Team       string  `json:"team"`
	ID         uint64  `json:"id"`
	OwnerID    string  `json:"ownerId"`
	X          float64 `json:"x"`
	Z          float64 `json:"z"`
	ImpactTick uint64  `json:"impactTick"`
	Radius     float64 `json:"radius"`
}
