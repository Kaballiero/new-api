package update_vendor

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errMissing = errors.New("record not found")

type fakeStore struct {
	duplicate bool
	saved     *domain.Vendor
	others    []domain.Vendor
	updated   *domain.Vendor
}

func (s *fakeStore) VendorDuplicate(int, string) (bool, error) { return s.duplicate, nil }
func (s *fakeStore) WithVendorMetadata(change func(domain.VendorMetadataMutation) error) error {
	return change(s)
}
func (s *fakeStore) IsMissing(err error) bool { return errors.Is(err, errMissing) }
func (s *fakeStore) SavedVendor(int) (*domain.Vendor, error) {
	if s.saved == nil {
		return nil, errMissing
	}
	return s.saved, nil
}
func (s *fakeStore) OtherVendors(int) ([]domain.Vendor, error) { return s.others, nil }
func (s *fakeStore) InsertVendor(*domain.Vendor) error         { return errors.New("unexpected insert") }
func (s *fakeStore) UpdateVendor(value *domain.Vendor) error {
	copied := *value
	s.updated = &copied
	return nil
}

func TestUpdateVendor(t *testing.T) {
	saved := &domain.Vendor{Id: 2, Name: "OpenAI", Status: 1, CreatedTime: 10}
	cases := []struct {
		name    string
		store   *fakeStore
		value   domain.Vendor
		err     error
		errText string
	}{
		{name: "missing id", store: &fakeStore{}, value: domain.Vendor{Name: "OpenAI"}, err: domain.ErrVendorIDMissing},
		{name: "blank name", store: &fakeStore{}, value: domain.Vendor{Id: 2, Name: " "}, err: domain.ErrVendorNameEmpty},
		{name: "duplicate", store: &fakeStore{duplicate: true}, value: domain.Vendor{Id: 2, Name: "OpenAI"}, err: domain.ErrVendorNameExists},
		{name: "missing vendor", store: &fakeStore{}, value: domain.Vendor{Id: 2, Name: "OpenAI"}, err: domain.ErrVendorNotFound},
		{name: "stale version", store: &fakeStore{saved: saved}, value: domain.Vendor{Id: 2, Name: "OpenAI", Version: "old"}, err: domain.ErrVendorConflict, errText: domain.ErrVendorConflict.Error()},
		{name: "name taken case-insensitively", store: &fakeStore{saved: saved, others: []domain.Vendor{{Id: 5, Name: "openai"}}}, value: domain.Vendor{Id: 2, Name: "OpenAI"}, err: domain.ErrVendorRejected, errText: "vendor name already exists"},
		{name: "update", store: &fakeStore{saved: saved}, value: domain.Vendor{Id: 2, Name: " OpenAI ", Version: domain.VendorRecordVersion(saved)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {

			refreshed := false
			out, err := NewUsecase(tc.store, func() int64 { return 300 }, func() { refreshed = true }).Execute(Input{Value: tc.value})
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				if tc.errText != "" {
					assert.EqualError(t, err, tc.errText)
				}
				assert.False(t, refreshed)

				return
			}
			require.NoError(t, err)
			assert.True(t, refreshed)

			// Creation time and status come from the saved record.
			assert.Equal(t, domain.Vendor{Version: tc.value.Version, Id: 2, Name: "OpenAI", Status: 1, CreatedTime: 10, UpdatedTime: 300}, *tc.store.updated)
			assert.Equal(t, domain.VendorRecordVersion(out), out.Version)
		})
	}
}
