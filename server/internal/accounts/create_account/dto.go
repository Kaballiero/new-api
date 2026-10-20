package create_account

type Input struct {
	Username, Password, DisplayName string
	Role                            int
	Permissions                     map[string]map[string]bool
}

type NewAccount struct {
	Username, Password, DisplayName string
	Role                            int
}
