package create_model

import (
	"errors"
	"testing"

	catalogcodec "github.com/QuantumNous/new-api/internal/adapter/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	duplicate    bool
	duplicateErr error
	vendors      map[int]bool
	inserted     *domain.Model
	restored     [3]int
}

func (s *fakeStore) ModelDuplicate(int, string) (bool, error) { return s.duplicate, s.duplicateErr }
func (s *fakeStore) WithModelCreation(change func(ModelMutation) error) error {
	return change(s)
}
func (s *fakeStore) VendorExists(id int) (bool, error) { return s.vendors[id], nil }
func (s *fakeStore) InsertModel(value *domain.Model) error {
	value.Id = 9
	copied := *value
	s.inserted = &copied
	return nil
}
func (s *fakeStore) RestoreModelDefaults(id, status, syncOfficial int) error {
	s.restored = [3]int{id, status, syncOfficial}
	return nil
}
func (s *fakeStore) UpdateModel(*domain.Model) error { return errors.New("unexpected update") }

func TestCreateModel(t *testing.T) {
	storageErr := errors.New("db down")
	cases := []struct {
		name      string
		store     *fakeStore
		value     domain.Model
		err       error
		errText   string
		refreshed bool
	}{
		{name: "empty name", store: &fakeStore{}, err: domain.ErrModelNameEmpty},
		{name: "invalid visibility", store: &fakeStore{}, value: domain.Model{ModelName: "gpt", Status: 2}, err: domain.ErrInvalidVisibility},
		{name: "duplicate check fails", store: &fakeStore{duplicateErr: storageErr}, value: domain.Model{ModelName: "gpt", Status: 1}, err: domain.ErrInternal, errText: "db down"},
		{name: "duplicate", store: &fakeStore{duplicate: true}, value: domain.Model{ModelName: "gpt", Status: 1}, err: domain.ErrModelNameExists},
		{name: "unsaved vendor", store: &fakeStore{}, value: domain.Model{ModelName: "gpt", Status: 1, VendorID: -1}, err: domain.ErrInternal, errText: "select a saved vendor"},
		{name: "missing vendor", store: &fakeStore{vendors: map[int]bool{}}, value: domain.Model{ModelName: "gpt", Status: 1, VendorID: 4}, err: domain.ErrInternal, errText: "vendor does not exist"},
		{name: "create", store: &fakeStore{vendors: map[int]bool{4: true}}, value: domain.Model{ModelName: "gpt", Status: 0, SyncOfficial: 0, VendorID: 4}, refreshed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refreshed := false
			out, err := NewUsecase(tc.store, func() int64 { return 100 }, func() { refreshed = true }, catalogcodec.Codec{}.DecodeEndpoints).Execute(Input{Value: tc.value})
			assert.Equal(t, tc.refreshed, refreshed)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				if tc.errText != "" {
					assert.EqualError(t, err, tc.errText)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 9, out.Id)
			assert.Equal(t, int64(100), out.CreatedTime)
			assert.Equal(t, int64(100), out.UpdatedTime)
			// Explicit zero status and sync flag survive the insert defaults.
			assert.Equal(t, [3]int{9, 0, 0}, tc.store.restored)
		})
	}
}
