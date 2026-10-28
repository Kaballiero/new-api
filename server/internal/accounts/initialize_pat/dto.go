package initialize_pat

type Input struct {
	UserID           int    `json:"user_id"`
	ExpectedUsername string `json:"expected_username"`
	Apply            bool   `json:"apply"`
}
type Result struct {
	UserID      int     `json:"user_id"`
	State       string  `json:"state"`
	Outcome     string  `json:"outcome"`
	AccessToken *string `json:"access_token"`
}
type Snapshot struct {
	ID, Role, Status      int
	Username, AccessToken string
}
