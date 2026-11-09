package messages

type Action struct {
	ViewTick uint64   `json:"viewTick,omitempty"`
	ID       uint64   `json:"id"`
	Button   string   `json:"button"`
	Aim      *float64 `json:"aim"`
	Target   *Point   `json:"target,omitempty"`
}
