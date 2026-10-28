package provision_account

type NewAccount struct {
	Username, Password, DisplayName, Group string
	Role, Status                           int
}

// GetAPICreateUserRequest is the body of a GetAPI account provisioning request.
type GetAPICreateUserRequest struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name"`
	Group        string `json:"group,omitempty"`
	InitialQuota *int   `json:"initial_quota,omitempty"`
}

// GetAPICreateCredential is the credential issued for a provisioned account.
type GetAPICreateCredential struct {
	UserID       int    `json:"user_id"`
	AccessToken  string `json:"access_token"`
	InitialQuota int    `json:"initial_quota"`
}
