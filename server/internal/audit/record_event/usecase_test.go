package record_event_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/audit/domain"
	"github.com/QuantumNous/new-api/internal/audit/record_event"
	"github.com/QuantumNous/new-api/internal/audit/transport"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type eventStore struct {
	events  []domain.Event
	lookups []int
	failure error
	invalid []int
}

func (s *eventStore) Append(_ context.Context, event domain.Event) error {
	s.events = append(s.events, event)
	return s.failure
}

func (s *eventStore) Username(id int) string {
	s.lookups = append(s.lookups, id)
	return fmt.Sprintf("owner-%d", id)
}

func (s *eventStore) InvalidRole(_ context.Context, _ string, role int) {
	s.invalid = append(s.invalid, role)
}

func recordingUsecase(store *eventStore) record_event.Usecase {
	next := 0
	return record_event.NewUsecase(store, store, domain.Roles{Common: 1, Admin: 10, Root: 100}, func() int64 { return 123 }, func() string {
		next++
		return fmt.Sprintf("id-%d", next)
	})
}

func TestActorRoleAndDefaulting(t *testing.T) {
	for _, role := range []int{1, 10, 100, 0, -1, 99} {
		t.Run(fmt.Sprint(role), func(t *testing.T) {
			store := &eventStore{}
			err := recordingUsecase(store).Execute(context.Background(), domain.Recording{Source: domain.Integration, Actor: domain.Actor{ID: 7, Role: role}})
			require.NoError(t, err)
			require.Len(t, store.events, 1)
			event := store.events[0]
			expectedRole := role
			if role != 1 && role != 10 && role != 100 {
				expectedRole = 0
				assert.Equal(t, []int{role}, store.invalid)
			}
			assert.Equal(t, expectedRole, event.ActorRole)
			assert.Equal(t, "id-1", event.RequestID)
			assert.Equal(t, "id-2", event.EventID)
			assert.EqualValues(t, 123, event.CreatedAt)
			assert.Equal(t, "owner-7", event.Username)
			assert.Equal(t, []int{7}, store.lookups)
			assert.Empty(t, event.AuthMethod, "a missing request must not invent transport facts")
		})
	}
}

func TestEventPoliciesAtCompletion(t *testing.T) {
	failed := false
	actor := domain.Actor{ID: 7, Username: "snapshot", Role: 100}
	request := domain.RequestFacts{Present: true, RequestID: "request", Method: "POST", Route: "/api/unknown/:id", Status: 409,
		AuthMethod: "session", UserAgent: strings.Repeat("\u754c", 513), RouteParams: map[string]string{"id": "5"}}
	for _, tc := range []struct {
		name          string
		input         domain.Recording
		category      string
		owner, status int
		success       bool
	}{
		{"management", domain.Recording{Source: domain.Management, TargetID: 9, Action: "user.delete"}, "operation", 7, 200, true},
		{"security", domain.Recording{Source: domain.Security, TargetID: 9, Action: "user.security_verify",
			Parameters: map[string]any{"success": false}, SecurityCode: "denied"}, "security", 9, 409, false},
		{"fallback", domain.Recording{Source: domain.AdminFallback, Response: domain.ResponseResult{Status: 200, Success: &failed}}, "operation", 7, 200, false},
		{"quota", domain.Recording{Source: domain.QuotaAdjustment, Action: "user.quota_add", Success: false}, "operation", 7, 409, false},
		{"subscription", domain.Recording{Source: domain.SubscriptionReset, TargetID: 9, Content: "legacy", Operator: &domain.Operator{ID: 7}}, "operation", 9, 409, true},
		{"token full buffer", domain.Recording{Source: domain.TokenOperation, Action: "token.key_view", Response: domain.ResponseResult{Status: 200, BufferFull: true}}, "security", 7, 200, false},
		{"PAT full buffer", domain.Recording{Source: domain.AccessToken, EventID: "snapshot-event", CreatedAt: 100,
			TokenRef: "fingerprint", Response: domain.ResponseResult{Status: 200, BufferFull: true}}, "access_token", 7, 200, true},
		{"login", domain.Recording{Source: domain.Login, LoginMethod: "password", VerificationMethod: "totp"}, "login", 7, 409, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &eventStore{}
			in := tc.input
			in.Actor, in.Request = actor, request
			require.NoError(t, recordingUsecase(store).Execute(context.Background(), in))
			require.Len(t, store.events, 1)
			event := store.events[0]
			assert.Equal(t, tc.category, event.Category)
			assert.Equal(t, tc.owner, event.OwnerID)
			assert.Equal(t, tc.status, event.Status)
			assert.Equal(t, tc.success, event.Success)
			assert.Equal(t, 100, event.ActorRole)
			assert.Equal(t, "request", event.RequestID)
			assert.Equal(t, strings.Repeat("\u754c", 512), event.UserAgent)
			switch in.Source {
			case domain.Management:
				assert.Equal(t, 9, event.Parameters["target_user_id"])
				require.NotNil(t, event.Operator)
				assert.Equal(t, 7, event.Operator.ID)
			case domain.Security:
				assert.Nil(t, event.Operator)
				assert.Equal(t, "denied", event.Parameters["code"])
			case domain.AdminFallback:
				assert.Equal(t, "generic", event.Action)
				assert.Equal(t, map[string]any{"method": "POST", "route": "/api/unknown/:id"}, event.Parameters)
			case domain.QuotaAdjustment:
				assert.Equal(t, "Failed user quota adjustment", event.Content)
				assert.Empty(t, event.Outcome.Path, "quota audit historically omits the nested path")
			case domain.SubscriptionReset:
				assert.Empty(t, event.Action)
				assert.False(t, event.OperationPresent)
				assert.Equal(t, "legacy", event.Content)
			case domain.AccessToken:
				assert.Equal(t, "snapshot-event", event.EventID)
				assert.EqualValues(t, 100, event.CreatedAt)
				assert.Equal(t, "access_token", event.AuthMethod)
				assert.Equal(t, "snapshot", event.Username)
			case domain.Login:
				assert.Equal(t, request.UserAgent, event.LoginUserAgent, "nested login UA retains its original unbounded shape")
				assert.Equal(t, "totp", event.Parameters["verification_method"])
			}
		})
	}
}

func TestSuppressionAndStorageFailure(t *testing.T) {
	sentinel := errors.New("storage failed")
	store := &eventStore{failure: sentinel}
	usecase := recordingUsecase(store)
	require.NoError(t, usecase.Execute(context.Background(), domain.Recording{Source: domain.AdminFallback, AlreadyLogged: true}))
	assert.Empty(t, store.events)
	assert.Empty(t, store.lookups)
	err := usecase.Execute(context.Background(), domain.Recording{Source: domain.Security, TargetID: 7, Actor: domain.Actor{Role: 10}})
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, "audit log write failed (request_id=id-1): storage failed", err.Error())
	require.Len(t, store.events, 1, "a failed write must not be retried")
}

func TestTransportWithoutRequest(t *testing.T) {
	for _, withContext := range []bool{false, true} {
		t.Run(fmt.Sprint(withContext), func(t *testing.T) {
			var c *gin.Context
			if withContext {
				c, _ = gin.CreateTestContext(httptest.NewRecorder())
			}
			store := &eventStore{}
			usecase := recordingUsecase(store)
			recording := transport.NewRecording(usecase.Execute, func(_ context.Context, err error) { require.NoError(t, err) }, nil, nil)
			require.NotPanics(t, func() { recording.Login(c, domain.Actor{ID: 7, Username: "owner", Role: 1}, "password") })
			require.Len(t, store.events, 1)
			assert.Empty(t, store.events[0].Method)
			assert.Empty(t, store.events[0].AuthMethod)
		})
	}
}

func TestReadErrorsUseIdentity(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"domain", fmt.Errorf("wrapped: %w", domain.ErrPagination), "Недопустимые параметры пагинации аудита"},
		{"storage with same text", errors.New("invalid audit pagination"), "invalid audit pagination"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest("GET", "/api/audit", nil)
			c.Request.Header.Set("Accept-Language", "ru")
			transport.WriteError(c, tc.err)
			assert.Equal(t, 200, response.Code)
			assert.JSONEq(t, fmt.Sprintf(`{"success":false,"message":%q}`, tc.want), response.Body.String())
		})
	}
}
