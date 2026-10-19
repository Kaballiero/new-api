package model

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func upstreamCatalogTestDB(t *testing.T, dialect string, upgrade bool) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector
	switch dialect {
	case "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "catalog.db") + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN not configured")
		}
		driver = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN not configured")
		}
		driver = postgres.Open(dsn)
	default:
		t.Fatalf("unsupported dialect %s", dialect)
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("catalog_contract_%d_", os.Getpid())}})
	require.NoError(t, err)
	previous := DB
	DB = db
	t.Cleanup(func() {
		DB = previous
		require.NoError(t, db.Migrator().DropTable(&CatalogDocument{}, &CatalogRevision{}, &CatalogSource{}, &Option{}))
		pool, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, pool.Close())
	})
	// Option is unchanged from released tag v0.3.4-27. Preserve its data and PK
	// while adding the new catalog tables to a representative existing database.
	if upgrade {
		require.NoError(t, db.AutoMigrate(&Option{}))
		require.NoError(t, db.Create(&Option{Key: "catalog-existing-price", Value: `{"model":"Модель 🚀","price":"0.001"}`}).Error)
	}
	for range 2 {
		require.NoError(t, db.AutoMigrate(&CatalogSource{}, &CatalogRevision{}, &CatalogDocument{}))
		if upgrade {
			var previous Option
			require.NoError(t, db.Where(&Option{Key: "catalog-existing-price"}).Take(&previous).Error)
			assert.Equal(t, `{"model":"Модель 🚀","price":"0.001"}`, previous.Value)

		}
	}
	if upgrade {
		require.Error(t, db.Create(&Option{Key: "catalog-existing-price", Value: "duplicate"}).Error)
	}
	var version string
	if dialect == "sqlite" {
		require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
	} else {
		require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
	}
	t.Logf("database=%s version=%s", dialect, version)
	return db
}

func TestUpstreamCatalogStorage(t *testing.T) {
	for _, tc := range []struct {
		dialect string
		upgrade bool
	}{
		{"sqlite", false}, {"mysql", false}, {"postgres", false},
		{"sqlite", true}, {"mysql", true}, {"postgres", true},
	} {
		t.Run(fmt.Sprintf("%s/upgrade=%t", tc.dialect, tc.upgrade), func(t *testing.T) {
			db := upstreamCatalogTestDB(t, tc.dialect, tc.upgrade)
			ctx := context.Background()
			source := CatalogSource{ID: CatalogHash([]byte("public-catalog")), Kind: "openrouter", BaseURL: "https://catalog.example"}
			head, err := EnsureCatalogSource(ctx, source)
			require.NoError(t, err)
			raw := `{"id":"vendor/model","name":"Модель 🚀","price":"0.0000000000000000000123456789","unknown":null,"free":"0"}`
			first := CatalogDocument{SourceID: source.ID, ModelID: "vendor/model", Payload: raw, Hash: CatalogHash([]byte(raw))}
			second := CatalogDocument{SourceID: source.ID, ModelID: "vendor/second", Payload: `{"id":"vendor/second","provider":null}`}
			second.Hash = CatalogHash([]byte(second.Payload))
			selection := CatalogSelection{SchemaVersion: 1, NormalizationVersion: "1", Models: []CatalogModelReference{{second.ModelID, second.Hash}, {first.ModelID, first.Hash}}}
			remote := strings.Repeat("a", 64)
			fetched := "2026-10-19T00:00:00Z"
			pub, err := PublishCatalogSelection(ctx, head, selection, remote, fetched, []CatalogDocument{first, second})
			require.NoError(t, err)
			assert.Equal(t, 2, pub.NewDocuments)
			assert.True(t, pub.Changed)
			original := pub.Revision
			docs, err := GetCatalogDocuments(ctx, source.ID, []string{first.Hash, second.Hash})
			require.NoError(t, err)
			require.Len(t, docs, 2)
			assert.Equal(t, raw, docs[first.Hash].Payload)
			head, err = EnsureCatalogSource(ctx, source)
			require.NoError(t, err)
			pub, err = PublishCatalogSelection(ctx, head, selection, remote, "2026-10-19T01:00:00Z", nil)
			require.NoError(t, err)
			assert.Zero(t, pub.NewDocuments)
			assert.Equal(t, 2, pub.ReusedDocuments)
			assert.False(t, pub.Changed)
			assert.Equal(t, original, pub.Revision)
			head, err = EnsureCatalogSource(ctx, source)
			require.NoError(t, err)
			assert.Equal(t, "2026-10-19T01:00:00Z", head.FetchedAt)
			changed := first
			changed.Payload = strings.Replace(first.Payload, "0123456789", "0987654321", 1)
			changed.Hash = CatalogHash([]byte(changed.Payload))
			selection.Models = []CatalogModelReference{{first.ModelID, changed.Hash}, {second.ModelID, second.Hash}}
			pub, err = PublishCatalogSelection(ctx, head, selection, strings.Repeat("b", 64), fetched, []CatalogDocument{changed})
			require.NoError(t, err)
			assert.Equal(t, 1, pub.NewDocuments)
			assert.Equal(t, 1, pub.ReusedDocuments)
			assert.True(t, pub.Changed)
			old, err := ReadCatalogSelection(ctx, source.ID, original)
			require.NoError(t, err)
			assert.Equal(t, first.Hash, old.Models[0].Hash)
			before, err := EnsureCatalogSource(ctx, source)
			require.NoError(t, err)
			missing := CatalogSelection{SchemaVersion: 1, NormalizationVersion: "1", Models: []CatalogModelReference{{"vendor/missing", strings.Repeat("c", 64)}}}
			_, err = PublishCatalogSelection(ctx, before, missing, remote, fetched, nil)
			require.ErrorContains(t, err, "missing a document")
			after, err := EnsureCatalogSource(ctx, source)
			require.NoError(t, err)
			assert.Equal(t, before, after, "failed staging cannot move head")
			_, err = PublishCatalogSelection(ctx, head, selection, remote, fetched, nil)
			require.ErrorIs(t, err, ErrCatalogSourceChanged)
			stale := changed
			stale.Payload = "corrupt"
			_, err = PublishCatalogSelection(ctx, before, selection, remote, fetched, []CatalogDocument{stale})
			require.Error(t, err)
			bad := CatalogDocument{SourceID: source.ID, ModelID: "invalid", Payload: "{}not-json"}
			bad.Hash = CatalogHash([]byte(bad.Payload))
			invalid := CatalogSelection{SchemaVersion: 1, NormalizationVersion: "1", Models: []CatalogModelReference{{bad.ModelID, bad.Hash}}}
			_, err = PublishCatalogSelection(ctx, before, invalid, remote, fetched, []CatalogDocument{bad})
			require.Error(t, err)
			// A database error after document insertion must roll back both data and head.
			injected := errors.New("catalog revision storage failed")
			callback := "catalog_test_revision_error"
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "CatalogRevision" {
					tx.AddError(injected)
				}
			}))
			next := changed
			next.Payload = strings.Replace(changed.Payload, "0987654321", "1111111111", 1)
			next.Hash = CatalogHash([]byte(next.Payload))
			stage := CatalogSelection{SchemaVersion: 1, NormalizationVersion: "1", Models: []CatalogModelReference{{first.ModelID, next.Hash}}}
			_, err = PublishCatalogSelection(ctx, before, stage, remote, fetched, []CatalogDocument{next})
			require.ErrorIs(t, err, injected)
			require.NoError(t, db.Callback().Create().Remove(callback))
			docs, err = GetCatalogDocuments(ctx, source.ID, []string{next.Hash})
			require.NoError(t, err)
			assert.Empty(t, docs)
			after, err = EnsureCatalogSource(ctx, source)
			require.NoError(t, err)
			assert.Equal(t, before, after)
			// Successful removal affects references only; old pinned revisions remain readable.
			selection.Models = []CatalogModelReference{{first.ModelID, changed.Hash}}
			pub, err = PublishCatalogSelection(ctx, before, selection, remote, fetched, nil)
			require.NoError(t, err)
			assert.Zero(t, pub.NewDocuments)
			assert.Equal(t, 1, pub.ReusedDocuments)
			old, err = ReadCatalogSelection(ctx, source.ID, original)
			require.NoError(t, err)
			assert.Len(t, old.Models, 2)
			require.NoError(t, db.AutoMigrate(&CatalogSource{}, &CatalogRevision{}, &CatalogDocument{}))
			retained, err := GetCatalogDocuments(ctx, source.ID, []string{first.Hash, second.Hash, changed.Hash})
			require.NoError(t, err)
			assert.Len(t, retained, 3)
			other := source
			other.ID = CatalogHash([]byte("account-other"))
			_, err = EnsureCatalogSource(ctx, other)
			require.NoError(t, err)
			docs, err = GetCatalogDocuments(ctx, other.ID, []string{first.Hash})
			require.NoError(t, err)
			assert.Empty(t, docs)
		})
	}
}

type catalogContractTransport func(*http.Request) (*http.Response, error)

func (f catalogContractTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestUpstreamCatalogWireContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/upstream_catalog/manifest.json")
	require.NoError(t, err)
	var manifest catalogsync.Manifest
	require.NoError(t, common.Unmarshal(raw, &manifest))
	require.NoError(t, manifest.Validate())
	fixtures := map[string][]byte{}
	for _, name := range []string{"direct", "aggregator", "alias"} {
		doc, err := os.ReadFile("testdata/upstream_catalog/" + name + ".json")
		require.NoError(t, err)
		fixtures[catalogsync.Hash(doc)] = doc
	}
	requests := 0
	client, err := catalogsync.NewClient("https://catalog.example", &http.Client{Transport: catalogContractTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		body := raw
		if req.URL.Path != "/v1/catalog/manifest" {
			var exists bool
			body, exists = fixtures[strings.TrimPrefix(req.URL.Path, "/v1/catalog/documents/")]
			require.True(t, exists)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})})
	require.NoError(t, err)
	fetched, err := client.Manifest(context.Background())
	require.NoError(t, err)
	assert.Equal(t, manifest.Revision, fetched.Revision)
	for _, ref := range fetched.Models {
		exact, doc, err := client.Document(context.Background(), ref)
		require.NoError(t, err)
		assert.Equal(t, ref.ID, doc.ID)
		assert.Equal(t, fixtures[ref.Hash], exact)
		if doc.ID == "acme/direct" {
			require.Len(t, doc.Offers, 1)
			assert.Nil(t, doc.Offers[0].Provider)
			assert.Equal(t, "0.000000000000123456789", *doc.Offers[0].Components[0].AmountUSD)
			assert.Nil(t, doc.Offers[0].Components[1].AmountUSD)
			assert.Equal(t, "0", *doc.Offers[0].Components[2].AmountUSD)
		}
		if doc.Alias != nil {
			assert.Empty(t, doc.Offers)
			assert.Equal(t, "router/aggregated", doc.Alias.TargetID)
		}
		_, err = catalogsync.DecodeDocument(append(exact, ' '), ref)
		require.ErrorContains(t, err, "hash mismatch")
	}
	assert.Equal(t, 4, requests)
	fetched.Revision = strings.Repeat("0", 64)
	require.ErrorContains(t, fetched.Validate(), "revision mismatch")
}

func TestUpstreamCatalogWireContractRejectsInvalidDocuments(t *testing.T) {
	fixture, err := os.ReadFile("testdata/upstream_catalog/direct.json")
	require.NoError(t, err)
	for _, tc := range []struct {
		name, before, after, errorText string
	}{
		{"missing_nullable_amount", `"amountUSD":"0.000000000000123456789",`, "", "missing or unknown fields"},
		{"missing_normalization", `"normalization":{"appliedDiscount":"0","comparable":true,"discountBasis":"net","effective":true,"reasons":[]},`, "", "missing or unknown fields"},
		{"null_normalization", `"normalization":{"appliedDiscount":"0","comparable":true,"discountBasis":"net","effective":true,"reasons":[]}`, `"normalization":null`, "null catalog field normalization"},
		{"missing_comparable", `"comparable":true,`, "", "missing or unknown fields"},
		{"null_comparable", `"comparable":true`, `"comparable":null`, "null catalog field comparable"},
		{"null_effective", `"effective":true`, `"effective":null`, "null catalog field effective"},
		{"null_eligible", `"eligible":true`, `"eligible":null`, "null catalog field eligible"},
		{"invalid_offer_coverage", `"offers":"partial"`, `"offers":"unavailable"`, "invalid catalog coverage state"},
		{"invalid_normalization_coverage", `"normalization":"partial"`, `"normalization":"unavailable"`, "invalid catalog coverage state"},
		{"invalid_discount_basis", `"discountBasis":"net"`, `"discountBasis":"unspecified"`, "invalid catalog discount basis"},
		{"invalid_scope", `"scope":"endpoint","variant":{}`, `"scope":"provider","variant":{}`, "invalid catalog offer"},
		{"unknown_document_field", `"alias":null`, `"unexpected":null,"alias":null`, "missing or unknown fields"},
		{"unknown_offer_field", `"components":[`, `"unexpected":null,"components":[`, "missing or unknown fields"},
		{"unknown_component_field", `"componentId":"input"`, `"unexpected":null,"componentId":"input"`, "missing or unknown fields"},
		{"unknown_normalization_field", `"comparable":true`, `"unexpected":null,"comparable":true`, "missing or unknown fields"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Contains(t, string(fixture), tc.before)
			invalid := []byte(strings.Replace(string(fixture), tc.before, tc.after, 1))
			// Hash the malformed wire payload itself: shape validation must reject
			// it even when transport integrity and model identity are correct.
			doc, err := catalogsync.DecodeDocument(invalid, catalogsync.Reference{ID: "acme/direct", Hash: catalogsync.Hash(invalid)})
			require.ErrorContains(t, err, tc.errorText)
			assert.Nil(t, doc)
		})
	}
	for _, state := range []string{"complete", "partial", "unknown"} {
		t.Run("accepted_coverage_"+state, func(t *testing.T) {
			raw := []byte(strings.ReplaceAll(string(fixture), `"partial"`, `"`+state+`"`))
			doc, err := catalogsync.DecodeDocument(raw, catalogsync.Reference{ID: "acme/direct", Hash: catalogsync.Hash(raw)})
			require.NoError(t, err)
			assert.Equal(t, state, doc.Coverage.Offers)
			assert.Equal(t, state, doc.Coverage.Normalization)
			assert.Equal(t, state, doc.Offers[0].Coverage.Offers)
			assert.Equal(t, state, doc.Offers[0].Coverage.Normalization)
		})
	}
}

func TestUpstreamCatalogWireContractRejectsInvalidManifests(t *testing.T) {
	fixture, err := os.ReadFile("testdata/upstream_catalog/manifest.json")
	require.NoError(t, err)
	for _, tc := range []struct{ name, before, after string }{
		{"missing_normalization_version", `"normalizationVersion":"1",`, ""},
		{"null_normalization_version", `"normalizationVersion":"1"`, `"normalizationVersion":null`},
		{"unknown_field", `"schemaVersion":1`, `"unexpected":null,"schemaVersion":1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Contains(t, string(fixture), tc.before)
			raw := strings.Replace(string(fixture), tc.before, tc.after, 1)
			client, err := catalogsync.NewClient("https://catalog.example", &http.Client{Transport: catalogContractTransport(func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "/v1/catalog/manifest", req.URL.Path)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}, nil
			})})
			require.NoError(t, err)
			_, err = client.Manifest(context.Background())
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "revision mismatch", "wire shape must fail before semantic revision validation")
		})
	}
}

func TestUpstreamCatalogClientHTTP(t *testing.T) {
	baseURL := os.Getenv("CATALOG_SYNC_TEST_API_URL")
	if baseURL == "" {
		t.Skip("CATALOG_SYNC_TEST_API_URL not configured; real catalog HTTP check is opt-in")
	}
	// Only the catalog's read endpoints are called. No provider inference,
	// acquisition/refresh endpoint or persistent application state is involved.
	client, err := catalogsync.NewClient(baseURL, &http.Client{Timeout: 30 * time.Second})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	manifest, err := client.Manifest(ctx)
	require.NoError(t, err)
	require.NoError(t, manifest.Validate())
	require.NotEmpty(t, manifest.Models, "catalog must contain a current model document to check")
	ref := manifest.Models[0]
	raw, doc, err := client.Document(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, ref.Hash, catalogsync.Hash(raw))
	assert.Equal(t, ref.ID, doc.ID)
	t.Logf("catalog_revision=%s model=%s document_hash=%s document_bytes=%d", manifest.Revision, ref.ID, ref.Hash, len(raw))
}

func TestUpstreamCatalogProviderPolicyChangesRevision(t *testing.T) {
	policy := `{"provider":{"only":["a"]}}`
	item := Pricing{ModelName: "router/model", UpstreamCatalogRoutesByGroup: map[string][]UpstreamCatalogRoute{"default": {{ChannelID: 1, ModelID: "router/model", DocumentHash: strings.Repeat("a", 64), ParamOverride: &policy}}}}
	before, err := pricingCatalogueFingerprint([]Pricing{item}, nil)
	require.NoError(t, err)
	policy = `{"provider":{"only":["b"]}}`
	after, err := pricingCatalogueFingerprint([]Pricing{item}, nil)
	require.NoError(t, err)
	assert.NotEqual(t, before, after)
}
