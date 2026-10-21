package apply_vendor_operation

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errMissing = errors.New("record not found")

type fakeStore struct {
	vendors  []domain.Vendor
	models   []domain.Model
	assigned []int
	deleted  []int
}

func (s *fakeStore) IsMissing(err error) bool { return errors.Is(err, errMissing) }
func (s *fakeStore) WithVendorMutation(change func(domain.VendorMutation) error) error {
	return change(s)
}
func (s *fakeStore) Vendors() ([]domain.Vendor, error)            { return s.vendors, nil }
func (s *fakeStore) Models(string, []int) ([]domain.Model, error) { return s.models, nil }
func (s *fakeStore) Assign(ids []int, _ int, _ int64) error {
	s.assigned = ids
	return nil
}
func (s *fakeStore) Delete(ids []int) error {
	s.deleted = ids
	return nil
}

// version returns the preview version the client must echo back.
func version(t *testing.T, store *fakeStore, operation Input) string {
	plan, err := domain.NewVendorOperationPlan(operation)
	require.NoError(t, err)
	require.NoError(t, plan.SetVendors(store.vendors))
	preview, err := plan.Complete(store.models)
	require.NoError(t, err)
	return preview.Version
}

func TestApplyVendorOperation(t *testing.T) {
	vendors := []domain.Vendor{{Id: 1, Name: "A"}, {Id: 2, Name: "B"}}
	t.Run("missing preview version is a conflict", func(t *testing.T) {
		_, err := NewUsecase(&fakeStore{}, nil, nil, nil).Execute(Input{Action: "delete", VendorIDs: []int{1}})
		require.ErrorIs(t, err, domain.ErrVendorRejected)
		require.ErrorIs(t, err, domain.ErrVendorConflict)
	})
	t.Run("referenced vendor cannot be deleted", func(t *testing.T) {
		_, err := NewUsecase(&fakeStore{vendors: vendors, models: []domain.Model{{Id: 9, VendorID: 1}}}, nil, nil, nil).Execute(Input{Action: "delete", VendorIDs: []int{1}, ExpectedVersion: "any"})
		require.ErrorIs(t, err, domain.ErrVendorRejected)
		var references *domain.VendorReferenceError
		require.ErrorAs(t, err, &references)
		assert.Equal(t, map[int]int64{1: 1}, references.Counts)
	})
	t.Run("stale preview is a conflict", func(t *testing.T) {
		_, err := NewUsecase(&fakeStore{vendors: vendors}, nil, nil, nil).Execute(Input{Action: "merge", VendorIDs: []int{1}, TargetVendorID: 2, ExpectedVersion: "old"})
		require.ErrorIs(t, err, domain.ErrVendorConflict)
	})
	t.Run("merge", func(t *testing.T) {
		store := &fakeStore{vendors: vendors, models: []domain.Model{{Id: 9, VendorID: 1}, {Id: 10, VendorID: 2}}}
		operation := Input{Action: "merge", VendorIDs: []int{1}, TargetVendorID: 2}
		operation.ExpectedVersion = version(t, store, operation)
		var audited *Output
		refreshed := false
		out, err := NewUsecase(store, func() int64 { return 1 }, func() { refreshed = true }, func(_ Input, result Output) { audited = &result }).Execute(operation)
		require.NoError(t, err)
		assert.Equal(t, &Output{UpdatedModels: []int{9}, DeletedVendors: []int{1}}, out)
		assert.Equal(t, out, audited)
		assert.Equal(t, []int{9}, store.assigned)
		assert.Equal(t, []int{1}, store.deleted)
		assert.True(t, refreshed)
	})
}
