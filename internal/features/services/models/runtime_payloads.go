package models

type RuntimeUpdatePayload struct {
	Environment string `json:"environment"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
}

type BlueGreenPrimaryPayload struct {
	Environment string `json:"environment"`
	ActiveSlot  string `json:"activeSlot"`
}
