package list_available_models

type Input struct {
	UserID                int
	UserGroup, TokenGroup string
	LimitEnabled          bool
	Limits                map[string]bool
	AutoGroups            func(string) []string
}
