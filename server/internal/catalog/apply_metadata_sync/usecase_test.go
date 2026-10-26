package apply_metadata_sync

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSource struct {
	version  string
	upstream map[string]domain.MetadataValues
	err      error
}

func (s fakeSource) Fetch(context.Context, string) (domain.SyncUpstreamSource, map[string]domain.MetadataValues, map[string]domain.Vendor, error) {
	return domain.SyncUpstreamSource{Version: s.version}, s.upstream, map[string]domain.Vendor{}, s.err
}

type fakeStore struct {
	locals   map[string]*domain.Model
	inserted []map[string]any
}

func (s *fakeStore) WithMetadataSync(change func(domain.MetadataMutation) error) error {
	return change(s)
}
func (s *fakeStore) SyncFields() []string                      { return []string{"description", "status"} }
func (s *fakeStore) OtherVendors(int) ([]domain.Vendor, error) { return nil, nil }
func (s *fakeStore) State() (map[string]*domain.Model, map[string]*domain.Vendor, error) {
	return s.locals, map[string]*domain.Vendor{}, nil
}
func (s *fakeStore) InsertVendor(*domain.Vendor) error { return errors.New("unexpected vendor") }
func (s *fakeStore) InsertModel(fields map[string]any) error {
	s.inserted = append(s.inserted, fields)
	return nil
}
func (s *fakeStore) UpdateModel(int, map[string]any) error { return errors.New("unexpected update") }

func TestApplyMetadataSync(t *testing.T) {
	upstream := map[string]domain.MetadataValues{"gpt": {Description: "chat", Status: 1}}
	createVersion := domain.MetadataRecordVersion(nil, nil, nil)
	cases := []struct {
		name    string
		source  fakeSource
		locals  map[string]*domain.Model
		input   Input
		err     error
		errText string
	}{
		{name: "upstream unavailable", source: fakeSource{err: errors.New("unsupported metadata language")}, input: Input{SourceVersion: "v1"}, errText: "unsupported metadata language"},
		{name: "source changed", source: fakeSource{version: "v2", upstream: upstream}, input: Input{SourceVersion: "v1"}, err: domain.ErrSyncSourceChanged},
		{name: "model disappeared", source: fakeSource{version: "v1", upstream: upstream}, input: Input{SourceVersion: "v1", Selections: []domain.MetadataSyncSelection{{ModelName: "o1", Create: true, RecordVersion: createVersion}}}, err: domain.ErrSyncModelUnavailable},
		{name: "record changed", source: fakeSource{version: "v1", upstream: upstream}, locals: map[string]*domain.Model{"gpt": {Id: 1, ModelName: "gpt", SyncOfficial: 1}}, input: Input{SourceVersion: "v1", Selections: []domain.MetadataSyncSelection{{ModelName: "gpt", Create: true, RecordVersion: createVersion}}}, err: domain.ErrMetadataSyncConflict},
		{name: "invalid selection", source: fakeSource{version: "v1", upstream: upstream}, input: Input{SourceVersion: "v1", Selections: []domain.MetadataSyncSelection{{ModelName: "gpt", RecordVersion: createVersion}}}, err: domain.ErrSyncRejected, errText: "select fields to update"},
		{name: "create", source: fakeSource{version: "v1", upstream: upstream}, input: Input{SourceVersion: "v1", Selections: []domain.MetadataSyncSelection{{ModelName: "gpt", Create: true, RecordVersion: createVersion}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{locals: tc.locals}

			refreshed := false
			out, err := NewUsecase(store, tc.source, func() int64 { return 50 }, func() { refreshed = true }).Execute(context.Background(), tc.input)
			if tc.err != nil || tc.errText != "" {
				if tc.err != nil {
					require.ErrorIs(t, err, tc.err)
				}
				if tc.errText != "" {
					require.EqualError(t, err, tc.errText)
				}

				assert.False(t, refreshed)
				assert.Empty(t, store.inserted)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []string{"gpt"}, out.CreatedModels)

			assert.True(t, refreshed)
			assert.Equal(t, []map[string]any{{"description": "chat", "status": 1, "model_name": "gpt", "sync_official": 1, "created_time": int64(50), "updated_time": int64(50)}}, store.inserted)
		})
	}
}
