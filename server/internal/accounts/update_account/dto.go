package update_account

type Input struct {
	ID             int
	Username       string
	RequestedRole  int
	ChangePassword bool
	Permissions    map[string]map[string]bool
}
