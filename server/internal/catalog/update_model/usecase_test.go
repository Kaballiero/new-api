package update_model

import (
	"errors"
	"testing"

	catalogcodec "github.com/QuantumNous/new-api/internal/adapter/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	duplicate bool
	statusErr error
	status    [2]int
	updated   *domain.Model
}

func (s *fakeStore) ModelDuplicate(int, string) (bool, error) { return s.duplicate, nil }
func (s *fakeStore) WithModelUpdate(change func(ModelMutation) error) error {
	return change(s)
}
func (s *fakeStore) SetModelStatus(id, status int) error {
	s.status = [2]int{id, status}
	return s.statusErr
}
func (s *fakeStore) VendorExists(int) (bool, error)  { return true, nil }
func (s *fakeStore) InsertModel(*domain.Model) error { return errors.New("unexpected insert") }
func (s *fakeStore) RestoreModelDefaults(int, int, int) error {
	return errors.New("unexpected restore")
}
func (s *fakeStore) UpdateModel(value *domain.Model) error {
	copied := *value
	s.updated = &copied
	return nil
}

func TestUpdateModel(t *testing.T) {
	cases := []struct {
		name      string
		store     *fakeStore
		input     Input
		err       error
		refreshed bool
	}{
		{name: "missing id", store: &fakeStore{}, input: Input{Value: domain.Model{ModelName: "gpt"}}, err: domain.ErrModelIDMissing},
		{name: "status only rejects visibility", store: &fakeStore{}, input: Input{Value: domain.Model{Id: 3, Status: 2}, StatusOnly: true}, err: domain.ErrInvalidVisibility},
		{name: "status only storage failure", store: &fakeStore{statusErr: errors.New("db down")}, input: Input{Value: domain.Model{Id: 3, Status: 1}, StatusOnly: true}, err: domain.ErrInternal},
		{name: "status only", store: &fakeStore{}, input: Input{Value: domain.Model{Id: 3, Status: 0}, StatusOnly: true}, refreshed: true},
		{name: "blank name", store: &fakeStore{}, input: Input{Value: domain.Model{Id: 3, ModelName: "  "}}, err: domain.ErrModelNameRequired},
		{name: "duplicate", store: &fakeStore{duplicate: true}, input: Input{Value: domain.Model{Id: 3, ModelName: "gpt"}}, err: domain.ErrModelNameExists},
		{name: "update", store: &fakeStore{}, input: Input{Value: domain.Model{Id: 3, ModelName: "gpt", Status: 1}}, refreshed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refreshed := false
			out, err := NewUsecase(tc.store, func() int64 { return 200 }, func() { refreshed = true }, catalogcodec.Codec{}.DecodeEndpoints).Execute(tc.input)
			assert.Equal(t, tc.refreshed, refreshed)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			if tc.input.StatusOnly {
				assert.Equal(t, [2]int{3, 0}, tc.store.status)
				assert.Nil(t, tc.store.updated)
				return
			}
			assert.Equal(t, int64(200), out.UpdatedTime)
			require.NotNil(t, tc.store.updated)
			assert.Equal(t, "gpt", tc.store.updated.ModelName)
		})
	}
}
