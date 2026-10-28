package domain

type AuthIdentity struct {
	UserID          int    `json:"user_id"`
	SessionID       string `json:"session_id"`
	UserAuthVersion int64  `json:"auth_version"`
	SessionVersion  int64  `json:"session_version"`
}
