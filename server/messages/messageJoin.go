package messages

type Join struct {
	Nick string `json:"nick"`
	Hero string `json:"hero"`
}
