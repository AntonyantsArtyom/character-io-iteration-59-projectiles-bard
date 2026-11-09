package messages

type Error struct {
	Type string `json:"type"`
	Code string `json:"code"`
}
