package dto

// Event is the audit API response shape shared by admin and own-event listings.
type Event struct {
	Id         int        `json:"id"`
	EventId    string     `json:"event_id"`
	UserId     int        `json:"user_id"`
	Username   string     `json:"username"`
	ActorRole  int        `json:"actor_role"` // Immutable role of the actor when the event began, not the log owner.
	CreatedAt  int64      `json:"created_at"`
	Category   string     `json:"category"`
	Action     string     `json:"action"`
	TokenRef   string     `json:"token_ref"`
	AuthMethod string     `json:"auth_method"`
	Ip         string     `json:"ip"`
	UserAgent  string     `json:"user_agent"`
	Method     string     `json:"method"`
	Route      string     `json:"route"`
	Status     int        `json:"status"`
	Success    bool       `json:"success"`
	RequestId  string     `json:"request_id"`
	Content    string     `json:"content"`
	Other      AuditOther `json:"other"`
}

// AuditOther is the structured metadata stored with an audit event. Privileged
// fields are separate so API projections can remove them without re-encoding JSON.
type AuditOther struct {
	Op          *AuditOperation   `json:"op,omitempty"`
	AdminInfo   *AuditAdminInfo   `json:"admin_info,omitempty"`
	AuditInfo   *AuditRequestInfo `json:"audit_info,omitempty"`
	RootInfo    AuditFields       `json:"root_info,omitempty"`
	LoginMethod string            `json:"login_method,omitempty"`
	UserAgent   string            `json:"user_agent,omitempty"`
}

type AuditOperation struct {
	Action string      `json:"action"`
	Params AuditFields `json:"params,omitempty"`
}

type AuditAdminInfo struct {
	AdminID       int    `json:"admin_id,omitempty"`
	AdminUsername string `json:"admin_username,omitempty"`
	AdminRole     int    `json:"admin_role,omitempty"`
	AuthMethod    string `json:"auth_method,omitempty"`
}

type AuditRequestInfo struct {
	Method  string            `json:"method"`
	Route   string            `json:"route"`
	Path    string            `json:"path"`
	Status  int               `json:"status"`
	Success bool              `json:"success"`
	Params  map[string]string `json:"params,omitempty"`
}

// AuditFields holds action-specific parameters and root-only extensions.
// Retain their encoded values when reading so arbitrary nested integers do not
// round-trip through float64 and lose precision before the API returns them.
type AuditFields map[string]any
