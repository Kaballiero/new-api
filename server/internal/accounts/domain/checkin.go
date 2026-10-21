package domain

// CheckinPolicy holds the current reward rules of the daily check-in.
type CheckinPolicy struct {
	Enabled  bool
	MinQuota int
	MaxQuota int
}

// Checkin is a daily check-in record.
type Checkin struct {
	Id           int    `json:"id"`
	UserId       int    `json:"user_id"`
	CheckinDate  string `json:"checkin_date"` // Format: YYYY-MM-DD.
	QuotaAwarded int    `json:"quota_awarded"`
	CreatedAt    int64  `json:"created_at"`
}

// CheckinRecord is a daily check-in record of the API response without private fields.
type CheckinRecord struct {
	CheckinDate  string `json:"checkin_date"`
	QuotaAwarded int    `json:"quota_awarded"`
}
