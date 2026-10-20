package manage_account

type ManageRequest struct {
	Id     int    `json:"id"`
	Action string `json:"action"`
	Value  int    `json:"value"`
	Mode   string `json:"mode"`
}

// ManageUserResponse — результат успешного запроса POST /api/user/manage.
// Action — выполненное действие ("disable"|"enable"|"delete"|"promote"|"demote"|"add_quota").
// Role и Status отражают состояние пользователя после действия; при изменении квоты
// Quota содержит её новое значение. Некоторые действия оставляют поля нулевыми — клиент
// должен определять значимые поля по Action.
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
