package domain

// CheckinPolicy содержит действующие правила награды за ежедневную отметку.
type CheckinPolicy struct {
	Enabled  bool
	MinQuota int
	MaxQuota int
}

// Checkin — запись ежедневной отметки.
type Checkin struct {
	Id           int    `json:"id"`
	UserId       int    `json:"user_id"`
	CheckinDate  string `json:"checkin_date"` // Формат: YYYY-MM-DD.
	QuotaAwarded int    `json:"quota_awarded"`
	CreatedAt    int64  `json:"created_at"`
}

// CheckinRecord — запись ежедневной отметки для ответа API без конфиденциальных полей.
type CheckinRecord struct {
	CheckinDate  string `json:"checkin_date"`
	QuotaAwarded int    `json:"quota_awarded"`
}
