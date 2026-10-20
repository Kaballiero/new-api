package get_codex_usage

type Input struct{ ChannelID int }
type Output struct {
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	UpstreamStatus int    `json:"upstream_status"`
	Data           any    `json:"data"`
}
