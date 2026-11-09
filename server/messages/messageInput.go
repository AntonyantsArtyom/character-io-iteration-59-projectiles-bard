package messages

type Input struct {
	Actions []Action `json:"actions,omitempty"`
	Seq     uint64   `json:"seq"`
	MoveX   float64  `json:"moveX"`
	MoveZ   float64  `json:"moveZ"`
	Aim     float64  `json:"aim"`
}
