package get_status

type Input struct{ UserID int }
type Output struct {
	Exists     bool   `json:"exists"`
	TokenRef   string `json:"token_ref"`
	CreatedAt  *int64 `json:"created_at"`
	LastUsedAt *int64 `json:"last_used_at"`
	LastUsedIp string `json:"last_used_ip"`
}
