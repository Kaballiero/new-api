package record

import "github.com/QuantumNous/new-api/internal/audit/domain"

const (
	AuditCategoryLogin       = domain.AuditCategoryLogin
	AuditCategorySecurity    = domain.AuditCategorySecurity
	AuditCategoryOperation   = domain.AuditCategoryOperation
	AuditCategoryAccessToken = domain.AuditCategoryAccessToken
)

// AuditLog is retained independently of usage logs and their cleanup/TTL policy.
// TokenRef identifies a PAT generation without storing its bearer credential.
type AuditLog struct {
	Id         int        `json:"id"`
	EventId    string     `json:"event_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId     int        `json:"user_id" gorm:"index:idx_audit_user_time,priority:1"`
	Username   string     `json:"username" gorm:"type:varchar(64);index"`
	ActorRole  int        `json:"actor_role"` // Immutable role of the actor when the event began, not the log owner.
	CreatedAt  int64      `json:"created_at" gorm:"type:bigint;index:idx_audit_user_time,priority:2;index:idx_audit_token_time,priority:2;index"`
	Category   string     `json:"category" gorm:"type:varchar(24);index"`
	Action     string     `json:"action" gorm:"type:varchar(128)"`
	TokenRef   string     `json:"token_ref" gorm:"type:varchar(64);index:idx_audit_token_time,priority:1"`
	AuthMethod string     `json:"auth_method" gorm:"type:varchar(24)"`
	Ip         string     `json:"ip" gorm:"type:varchar(64)"`
	UserAgent  string     `json:"user_agent" gorm:"type:varchar(512)"`
	Method     string     `json:"method" gorm:"type:varchar(16)"`
	Route      string     `json:"route" gorm:"type:varchar(255)"`
	Status     int        `json:"status"`
	Success    bool       `json:"success"`
	RequestId  string     `json:"request_id" gorm:"type:varchar(64);index"`
	Content    string     `json:"content" gorm:"type:text"`
	Other      AuditOther `json:"other" gorm:"type:json"`
}
