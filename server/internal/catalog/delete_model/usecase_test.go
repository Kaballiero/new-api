package delete_model

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/catalog/domain"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errMissing = errors.New("record not found")

type fakeStore struct {
	models        map[int]domain.Model
	channels      []domain.ChannelRouting
	saved         []domain.ChannelRouting
	deleted       []int
	removedPrices map[string]struct{}
	locked        bool
}

func (s *fakeStore) LockMetadata() func() {
	s.locked = true
	return func() {}
}
func (s *fakeStore) WithDeletion(_ bool, change func(domain.MetadataDeletion) error) error {
	return change(s)
}
func (s *fakeStore) ModelExists(id int) error {
	if _, ok := s.models[id]; !ok {
		return errMissing
	}
	return nil
}
func (s *fakeStore) IsMissing(err error) bool { return errors.Is(err, errMissing) }
func (s *fakeStore) ReadModels(ids []int) ([]domain.Model, error) {
	var out []domain.Model
	for _, id := range ids {
		if v, ok := s.models[id]; ok {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *fakeStore) RoutingChannels() ([]domain.ChannelRouting, error) { return s.channels, nil }
func (s *fakeStore) SaveRouting(v domain.ChannelRouting) error {
	s.saved = append(s.saved, v)
	return nil
}
func (s *fakeStore) DeleteModels(ids []int) error {
	s.deleted = ids
	return nil
}
func (s *fakeStore) RemovePrices(names map[string]struct{}) { s.removedPrices = names }

func TestDeleteModel(t *testing.T) {
	exact := domain.Model{Id: 1, ModelName: "gpt", NameRule: domain.NameRuleExact}
	prefix := domain.Model{Id: 2, ModelName: "claude-", NameRule: domain.NameRulePrefix}
	cases := []struct {
		name    string
		input   Input
		err     error
		errText string
		want    Output
		saved   []domain.ChannelRouting
		prices  map[string]struct{}
	}{
		{name: "pricing needs root", input: Input{ModelIDs: []int{1}, RemovePricing: true}, err: domain.ErrPricingRootOnly},
		{name: "missing model", input: Input{ModelIDs: []int{7}}, err: domain.ErrModelNotFound},
		{name: "rule models stay in channels", input: Input{ModelIDs: []int{2}, RemoveFromChannels: true}, errText: "only exact-match models can be removed from channels"},
		{name: "remove from channels and pricing", input: Input{ModelIDs: []int{1}, RemoveFromChannels: true, RemovePricing: true, AllowRemovePricing: true}, want: Output{DeletedCount: 1, UpdatedChannels: 1}, saved: []domain.ChannelRouting{{ID: 5, Models: "o1"}}, prices: map[string]struct{}{"gpt": {}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{models: map[int]domain.Model{1: exact, 2: prefix}, channels: []domain.ChannelRouting{{ID: 5, Models: "gpt,o1"}, {ID: 6, Models: "o1"}}}

			refreshedChannels, refreshedPricing := false, false
			out, err := NewUsecase(store, func() { refreshedChannels = true }, func() { refreshedPricing = true }).Execute(tc.input)
			switch {
			case tc.err != nil:
				require.ErrorIs(t, err, tc.err)
			case tc.errText != "":
				require.EqualError(t, err, tc.errText)
			default:
				require.NoError(t, err)
			}
			if tc.err != nil || tc.errText != "" {

				assert.False(t, refreshedPricing)
				assert.Empty(t, store.deleted)
				return
			}
			assert.Equal(t, tc.want, out)

			assert.Equal(t, tc.saved, store.saved)
			assert.Equal(t, tc.prices, store.removedPrices)
			assert.Equal(t, []int{1}, store.deleted)
			assert.True(t, store.locked)
			assert.True(t, refreshedChannels)
			assert.True(t, refreshedPricing)
		})
	}
}

func TestDeleteModelHTTPRefreshAuditResponseOrder(t *testing.T) {
	for _, exists := range []bool{true, false} {
		t.Run(fmt.Sprint(exists), func(t *testing.T) {
			store := &fakeStore{models: map[int]domain.Model{}, channels: []domain.ChannelRouting{{ID: 5, Models: "gpt,o1"}}}
			if exists {
				store.models[1] = domain.Model{Id: 1, ModelName: "gpt"}
			}
			var events []string
			scenario := NewUsecase(store, func() { events = append(events, "channels") }, func() { events = append(events, "pricing") })
			response := httptest.NewRecorder()
			handler := NewHTTP(scenario, func(_ *gin.Context, input Input, out Output) {
				assert.Equal(t, []int{1}, store.deleted)
				assert.Empty(t, response.Body.String(), "audit precedes the response")
				assert.Equal(t, []int{1}, input.ModelIDs)
				assert.Equal(t, 1, out.DeletedCount)
				events = append(events, "audit")
			})
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest("DELETE", "/api/models/1?remove_from_channels=true", nil)
			context.Params = gin.Params{{Key: "id", Value: "1"}}
			handler.Handle(context)
			if exists {
				assert.Equal(t, []string{"channels", "pricing", "audit"}, events)
				assert.Contains(t, response.Body.String(), `"success":true`)
			} else {
				assert.Empty(t, events)
				assert.Contains(t, response.Body.String(), `"success":false`)
			}
		})
	}
}
