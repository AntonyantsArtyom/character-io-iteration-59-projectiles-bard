package messages

type Join struct {
	CompactSnapshots bool   `json:"compactSnapshots,omitempty"`
	Nick             string `json:"nick"`
	Hero             string `json:"hero"`
}
