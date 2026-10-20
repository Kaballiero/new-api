package adjust_quota

type Request struct {
	Id     int    `json:"id"`
	Action string `json:"action"`
	Value  int    `json:"value"`
	Mode   string `json:"mode"`
}

// Response — результат успешного запроса POST /api/user/manage.
// Action — выполненное действие ("disable"|"enable"|"delete"|"promote"|"demote"|"add_quota").
// Role и Status отражают состояние пользователя после действия; при изменении квоты
// Quota содержит её новое значение. Некоторые действия оставляют поля нулевыми — клиент
// должен определять значимые поля по Action.
type Response struct {
	Action string `json:"action"`
	UserId int    `json:"user_id"`
	Role   int    `json:"role"`
	Status int    `json:"status"`
	Quota  int    `json:"quota,omitempty"`
}

type QuotaAccount struct {
	ID                  int
	Username            string
	Role, Status, Quota int
}
