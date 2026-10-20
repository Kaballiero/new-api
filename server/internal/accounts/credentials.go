package accounts

type GetAPICreateUserRequest struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name"`
	Group        string `json:"group,omitempty"`
	InitialQuota *int   `json:"initial_quota,omitempty"`
}

type GetAPICreateCredential struct {
	UserID       int    `json:"user_id"`
	AccessToken  string `json:"access_token"`
	InitialQuota int    `json:"initial_quota"`
}
