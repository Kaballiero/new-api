package audit

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/audit/record"
	"github.com/QuantumNous/new-api/internal/audit/domain"
	"github.com/QuantumNous/new-api/internal/audit/dto"
)

func eventRecord(event domain.Event) record.AuditLog {
	other := record.AuditOther{RootInfo: event.RootFields, LoginMethod: event.LoginMethod, UserAgent: event.LoginUserAgent}
	if event.OperationPresent {
		other.Op = &record.AuditOperation{Action: event.OperationAction, Params: event.Parameters}
	}
	if op := event.Operator; op != nil {
		other.AdminInfo = &record.AuditAdminInfo{AdminID: op.ID, AdminUsername: op.Username, AdminRole: op.Role, AuthMethod: op.AuthMethod}
	}
	if out := event.Outcome; out != nil {
		other.AuditInfo = &record.AuditRequestInfo{Method: out.Method, Route: out.Route, Path: out.Path, Status: out.Status, Success: out.Success, Params: out.Params}
	}
	return record.AuditLog{EventId: event.EventID, UserId: event.OwnerID, Username: event.Username, ActorRole: event.ActorRole,
		CreatedAt: event.CreatedAt, Category: event.Category, Action: event.Action, TokenRef: event.TokenRef, AuthMethod: event.AuthMethod,
		Ip: event.IP, UserAgent: event.UserAgent, Method: event.Method, Route: event.Route, Status: event.Status, Success: event.Success,
		RequestId: event.RequestID, Content: event.Content, Other: other}
}

func rowsDTO(rows []*record.AuditLog, policy domain.VisibilityPolicy) []*dto.Event {
	result := make([]*dto.Event, 0, len(rows))
	for _, row := range rows {
		other := dto.AuditOther{RootInfo: dto.AuditFields(row.Other.RootInfo), LoginMethod: row.Other.LoginMethod, UserAgent: row.Other.UserAgent}
		if op := row.Other.Op; op != nil {
			other.Op = &dto.AuditOperation{Action: op.Action, Params: dto.AuditFields(op.Params)}
		}
		if admin := row.Other.AdminInfo; admin != nil && !policy.HidePrivileged {
			other.AdminInfo = (*dto.AuditAdminInfo)(admin)
		}
		if outcome := row.Other.AuditInfo; outcome != nil && !policy.HidePrivileged {
			other.AuditInfo = (*dto.AuditRequestInfo)(outcome)
		}
		if policy.HideRoot {
			other.RootInfo = nil
		}
		result = append(result, &dto.Event{Id: row.Id, EventId: row.EventId, UserId: row.UserId, Username: row.Username, ActorRole: row.ActorRole,
			CreatedAt: row.CreatedAt, Category: row.Category, Action: row.Action, TokenRef: row.TokenRef, AuthMethod: row.AuthMethod,
			Ip: row.Ip, UserAgent: row.UserAgent, Method: row.Method, Route: row.Route, Status: row.Status, Success: row.Success,
			RequestId: row.RequestId, Content: row.Content, Other: other})
	}
	return result
}
