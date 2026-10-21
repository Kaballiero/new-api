package manage_account

type ManageRequest struct {
	Id     int    `json:"id"`
	Action string `json:"action"`
	Value  int    `json:"value"`
	Mode   string `json:"mode"`
}

// ManageUserResponse is the result of a successful POST /api/user/manage request.
// Action is the performed action ("disable"|"enable"|"delete"|"promote"|"demote"|"add_quota").
// Role and Status reflect the account after the action; on a quota change Quota
// holds the new value. Some actions leave fields zero, so clients choose the
// meaningful fields by Action.
type ManageUserResponse struct {
	Action string `json:"action"`
	UserId int    `json:"user_id"`
	Role   int    `json:"role"`
	Status int    `json:"status"`
	Quota  int    `json:"quota,omitempty"`
}

type Input struct {
	ID     int
	Action string
}

type Result struct {
	Action               string
	UserID, Role, Status int
	Deleted              bool
}
