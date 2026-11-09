package messages

type Player struct {
	InspirationSpeedUntil uint64  `json:"inspirationSpeedUntil,omitempty"`
	InspirationCharges    int     `json:"inspirationCharges,omitempty"`
	PrimaryReadyTick      uint64  `json:"primaryReadyTick"`
	Bot                   bool    `json:"bot,omitempty"`
	InvisibleUntil        uint64  `json:"invisibleUntil,omitempty"`
	Team                  string  `json:"team"`
	ID                    string  `json:"id"`
	Nick                  string  `json:"nick"`
	Hero                  string  `json:"hero"`
	X                     float64 `json:"x"`
	Z                     float64 `json:"z"`
	Aim                   float64 `json:"aim"`
	HP                    int     `json:"hp"`
	MaxHP                 int     `json:"maxHp"`
	RespawnTick           uint64  `json:"respawnTick,omitempty"`
	Attack                *Attack `json:"attack,omitempty"`
	SecondaryAttack       *Attack `json:"secondaryAttack,omitempty"`
	SecondaryReadyTick    uint64  `json:"secondaryReadyTick"`
}
