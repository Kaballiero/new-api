package extend_deployment

type ExtendDurationRequest struct {
	DurationHours int `json:"duration_hours"`
}

type Input struct {
	APIKey, ID    string
	DurationHours int
}
