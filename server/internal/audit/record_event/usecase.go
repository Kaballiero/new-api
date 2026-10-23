package record_event

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/internal/audit/domain"
)

type Store interface {
	Append(context.Context, domain.Event) error
	Username(int) string
}

type Diagnostics interface {
	InvalidRole(context.Context, string, int)
}

type Usecase struct {
	store       Store
	diagnostics Diagnostics
	roles       domain.Roles
	now         func() int64
	newID       func() string
}

func NewUsecase(store Store, diagnostics Diagnostics, roles domain.Roles, now func() int64, newID func() string) Usecase {
	return Usecase{store: store, diagnostics: diagnostics, roles: roles, now: now, newID: newID}
}

// Execute selects the event once the operation completes. Request snapshots belong to transport.
func (u Usecase) Execute(ctx context.Context, in domain.Recording) error {
	if in.Source == domain.AdminFallback && in.AlreadyLogged {
		return nil
	}
	req := in.Request
	event := domain.Event{OwnerID: in.Actor.ID, Username: in.Actor.Username, ActorRole: in.Actor.Role,
		Action: in.Action, Content: in.Content, Parameters: in.Parameters, EventID: in.EventID, CreatedAt: in.CreatedAt}
	switch in.Source {
	case domain.Management, domain.Security, domain.AdminFallback, domain.QuotaAdjustment:
		event.Username = ""
		event.OperationPresent = true
		event.OperationAction = in.Action
		event.Category = domain.AuditCategoryOperation
		event.Status, event.Success = 200, true
		if in.Source == domain.Security {
			event.OwnerID = in.TargetID
			event.Category = domain.AuditCategorySecurity
			if in.SecurityCode != "" {
				if event.Parameters == nil {
					event.Parameters = map[string]any{}
				}
				event.Parameters["code"] = in.SecurityCode
			}
			if success, ok := event.Parameters["success"].(bool); ok {
				event.Outcome = &domain.RequestOutcome{Method: req.Method, Route: req.Route, Path: req.Route, Status: req.Status, Success: success}
			}
		} else {
			event.Operator = &domain.Operator{ID: in.Actor.ID, Username: in.Actor.Username, Role: in.Actor.Role, AuthMethod: req.AuthMethod}
			if event.Parameters == nil {
				event.Parameters = map[string]any{}
			}
			if in.Source == domain.Management {
				if _, exists := event.Parameters["target_user_id"]; !exists && in.TargetID > 0 && in.TargetID != in.Actor.ID {
					event.Parameters["target_user_id"] = in.TargetID
				}
			}
		}
		event.Content = domain.OperationContent(event.Action, event.Parameters)
		if in.Source == domain.AdminFallback {
			event.Action = domain.AdminAction(req.Method, req.Route)
			event.OperationAction = event.Action
			event.Parameters = map[string]any{}
			if event.Action == "generic" {
				event.Parameters["method"], event.Parameters["route"] = req.Method, req.Route
			}
			event.Content = req.Method + " " + req.Route
			event.Outcome = &domain.RequestOutcome{Method: req.Method, Route: req.Route, Path: req.Route,
				Status: in.Response.Status, Success: in.Response.Succeeded(), Params: req.RouteParams}
		}
		if in.Source == domain.QuotaAdjustment {
			if !in.Success {
				event.Content = "Failed user quota adjustment"
			}
			event.Outcome = &domain.RequestOutcome{Method: req.Method, Route: req.Route, Status: req.Status, Success: in.Success}
		}
		if event.Outcome != nil {
			event.Status, event.Success = event.Outcome.Status, event.Outcome.Success
		}
		// Operation recording historically resolves the owner before the general writer defaults.
		event.Username = u.store.Username(event.OwnerID)
	case domain.Login:
		event.Category, event.Action, event.Success = domain.AuditCategoryLogin, "login", true
		event.OperationPresent, event.OperationAction = true, "login"
		event.Parameters = map[string]any{"method": in.LoginMethod}
		if in.VerificationMethod != "" {
			event.Parameters["verification_method"] = in.VerificationMethod
		}
		event.LoginMethod, event.LoginUserAgent = in.LoginMethod, req.UserAgent
		event.Content = fmt.Sprintf("Logged in successfully via %s", in.LoginMethod)
	case domain.TokenOperation:
		event.Category = domain.AuditCategorySecurity
		event.OperationPresent, event.OperationAction = true, event.Action
		event.Status, event.Success = in.Response.Status, in.Response.Succeeded()
		if in.Response.BufferFull {
			event.Success = event.Status < 400 && in.Response.HandlerSucceeded
		}
	case domain.AccessToken:
		event.Category, event.AuthMethod, event.TokenRef = domain.AuditCategoryAccessToken, "access_token", in.TokenRef
		event.Status, event.Success = in.Response.Status, in.Response.Succeeded()
	case domain.Integration:
		event.Category, event.Success = domain.AuditCategoryOperation, req.Status < 400
		event.RootFields = map[string]any{"integration_id": in.IntegrationID, "target_user_id": in.TargetID}
	case domain.SubscriptionReset:
		event.OwnerID, event.Username = in.TargetID, u.store.Username(in.TargetID)
		event.Category, event.Success, event.Operator = domain.AuditCategoryOperation, true, in.Operator
	}
	if req.Present {
		event.RequestID, event.IP, event.UserAgent = req.RequestID, req.IP, req.UserAgent
		event.Method, event.Route = req.Method, req.Route
		if event.Status == 0 {
			event.Status = req.Status
		}
		if event.AuthMethod == "" {
			event.AuthMethod = req.AuthMethod
		}
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = u.now()
	}
	if event.RequestID == "" {
		event.RequestID = u.newID()
	}
	if event.EventID == "" {
		event.EventID = u.newID()
	}
	if !u.roles.Valid(event.ActorRole) {
		u.diagnostics.InvalidRole(ctx, event.RequestID, event.ActorRole)
		event.ActorRole = 0
	}
	if event.Username == "" {
		event.Username = u.store.Username(event.OwnerID)
	}
	if ua := []rune(event.UserAgent); len(ua) > 512 {
		event.UserAgent = string(ua[:512])
	}
	if err := u.store.Append(ctx, event); err != nil {
		return fmt.Errorf("audit log write failed (request_id=%s): %w", event.RequestID, err)
	}
	return nil
}
