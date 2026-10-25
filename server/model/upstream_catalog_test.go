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
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func TestCatalogConsumerOperationRequiresModelSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, kind, operation, modality string
		output                          []string
		endpoint                        constant.EndpointType
		batch                           bool
	}{
		{"text chat", "text", "text", "TEXT", []string{"text"}, constant.EndpointTypeOpenAI, false},
		{"text is not embedding", "text", "", "TEXT", []string{"text"}, constant.EndpointTypeEmbeddings, false},
		{"embedding", "embedding", "embeddings", "EMBEDDINGS", []string{"embeddings"}, constant.EndpointTypeEmbeddings, false},
		{"embedding is not text", "embedding", "", "EMBEDDINGS", []string{"embeddings"}, constant.EndpointTypeOpenAI, false},
		{"image via chat", "image", "image_generation", "IMAGE", []string{"image"}, constant.EndpointTypeOpenAI, false},
		{"image text output stays image operation", "image", "image_generation", "IMAGE", []string{"image", "text"}, constant.EndpointTypeOpenAI, false},
		{"native image", "image", "image_generation", "IMAGE", []string{"image"}, constant.EndpointTypeImageGeneration, false},
		{"image is not embedding", "image", "", "IMAGE", []string{"image", "text"}, constant.EndpointTypeEmbeddings, false},
		{"image does not imply Responses", "image", "", "IMAGE", []string{"image"}, constant.EndpointTypeOpenAIResponse, false},
		{"video", "video", "text_to_video", "VIDEO", []string{"video"}, constant.EndpointTypeOpenAIVideo, false},
		{"speech", "speech", "speech_generation", "OTHER", []string{"speech"}, constant.EndpointTypeOpenAI, false},
		{"transcription", "transcription", "transcription", "OTHER", []string{"transcription"}, constant.EndpointTypeOpenAI, false},
		{"rerank", "rerank", "rerank", "RERANK", []string{"rerank"}, constant.EndpointTypeOpenAI, false},
		{"audio with text", "audio", "audio_generation", "OTHER", []string{"audio", "text"}, constant.EndpointTypeOpenAI, false},
		{"decision is not Chat", "decision", "", "TEXT", []string{"decisions"}, constant.EndpointTypeOpenAI, false},
		{"native decisions", "decision", "decisions", "TEXT", []string{"decisions"}, PricingEndpointSystemOne, false},
		{"decision declaration required", "decision", "", "TEXT", []string{"text"}, PricingEndpointSystemOne, false},
		{"decisions not Batch", "decision", "", "TEXT", []string{"decisions"}, PricingEndpointSystemOne, true},
		{"unknown metadata", "unknown", "", "OTHER", nil, constant.EndpointTypeOpenAI, false},
		{"missing output capability", "image", "", "OTHER", nil, constant.EndpointTypeOpenAI, false},
		{"exact batch", "text", "text", "TEXT", []string{"text"}, PricingEndpointBatch, true},
		{"batch tariff never becomes sync", "text", "", "TEXT", []string{"text"}, constant.EndpointTypeOpenAI, true},
		{"base does not imply batch", "text", "", "TEXT", []string{"text"}, PricingEndpointBatch, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := &catalogsync.Document{ID: "vendor/model", Type: tc.kind, Modalities: catalogsync.Modalities{Input: []string{"text"}, Output: tc.output}}
			if tc.batch {
				doc.ID += ":batch"
			}
			assert.Equal(t, tc.operation, catalogConsumerOperation(doc, doc.ID, tc.endpoint).operation)
			_, modality := catalogConsumerProfile(doc)
			assert.Equal(t, tc.modality, modality)
			assert.Equal(t, tc.output, doc.Modalities.Output, "consumer projection must not rewrite source capabilities")
		})
	}
	// The consumer filter does not change configured billing/admission semantics.
	assert.Equal(t, "embeddings", configuredPricingOperation(&Channel{Type: constant.ChannelTypeOpenRouter}, "alias", "vendor/text", constant.EndpointTypeEmbeddings).operation)
}

func TestCatalogConsumerAuditClassification(t *testing.T) {
	directory := os.Getenv("CATALOG_SEMANTIC_AUDIT_DIR")
	if directory == "" {
		t.Skip("immutable 534-model audit fixtures not configured")
	}
	raw, err := os.ReadFile(filepath.Join(directory, "catalog-metadata.json"))
	require.NoError(t, err)
	var metadata struct{ Documents []catalogsync.Document }
	require.NoError(t, common.Unmarshal(raw, &metadata))
	byID := make(map[string]*catalogsync.Document)
	for i := range metadata.Documents {
		byID[metadata.Documents[i].ID] = &metadata.Documents[i]
	}
	raw, err = os.ReadFile(filepath.Join(directory, "classification.json"))
	require.NoError(t, err)
	var classifications []struct {
		ModelName, DisplayName, DisplayedModality, CatalogID string
	}
	require.NoError(t, common.Unmarshal(raw, &classifications))
	require.Len(t, classifications, 534)
	counts := make(map[string]int)
	changes := []map[string]any{}
	decisions := 0
	for _, before := range classifications {
		doc := byID[before.CatalogID]
		require.NotNil(t, doc, before.ModelName)
		_, modality := catalogConsumerProfile(doc)
		counts[modality]++
		if doc.Type == "decision" {
			decisions++
		}
		operations := []string{}
		for _, endpoint := range []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeEmbeddings, constant.EndpointTypeOpenAIVideo} {
			operation := catalogConsumerOperation(doc, doc.ID, endpoint).operation
			if operation != "" {
				operations = append(operations, operation)
			}
		}
		if doc.Type != "embedding" {
			assert.NotContains(t, operations, "embeddings", before.ModelName)
		}
		if doc.Type == "embedding" {
			assert.Equal(t, []string{"embeddings"}, operations, before.ModelName)
		}
		changes = append(changes, map[string]any{"modelName": before.ModelName, "displayName": before.DisplayName, "catalogId": doc.ID, "catalogType": doc.Type, "beforeModality": before.DisplayedModality, "afterModality": modality, "consumerOperations": operations, "evidenceClass": "immutable metadata/resolver fixture; not deployed API"})
	}
	assert.Equal(t, map[string]int{"TEXT": 342, "IMAGE": 60, "VIDEO": 30, "EMBEDDINGS": 30, "RERANK": 8, "OTHER": 64}, counts)
	assert.Equal(t, 20, decisions)
	if output := os.Getenv("CATALOG_SEMANTIC_AUDIT_OUTPUT"); output != "" {
		raw, err := common.Marshal(changes)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(output, raw, 0o644))
	}
	t.Logf("534 metadata fixtures: classifications=%v; unresolved decisions=%d", counts, decisions)
}

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
			selection := CatalogSelection{SchemaVersion: 1, NormalizationVersion: "1", Models: []CatalogModelReference{{ID: second.ModelID, Hash: second.Hash}, {ID: first.ModelID, Hash: first.Hash}}}
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
			selection.Models = []CatalogModelReference{{ID: first.ModelID, Hash: changed.Hash}, {ID: second.ModelID, Hash: second.Hash}}
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
			missing := CatalogSelection{SchemaVersion: 1, NormalizationVersion: "1", Models: []CatalogModelReference{{ID: "vendor/missing", Hash: strings.Repeat("c", 64)}}}
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
			invalid := CatalogSelection{SchemaVersion: 1, NormalizationVersion: "1", Models: []CatalogModelReference{{ID: bad.ModelID, Hash: bad.Hash}}}
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
			stage := CatalogSelection{SchemaVersion: 1, NormalizationVersion: "1", Models: []CatalogModelReference{{ID: first.ModelID, Hash: next.Hash}}}
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
			selection.Models = []CatalogModelReference{{ID: first.ModelID, Hash: changed.Hash}}
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
	item.UpstreamCatalogRoutesByGroup["default"][0].ServiceTierEnabled = true
	withServiceTier, err := pricingCatalogueFingerprint([]Pricing{item}, nil)
	require.NoError(t, err)
	assert.NotEqual(t, after, withServiceTier)
	item.UpstreamCatalogRoutesByGroup["default"][0].Mode = "batch"
	withBatch, err := pricingCatalogueFingerprint([]Pricing{item}, nil)
	require.NoError(t, err)
	assert.NotEqual(t, withServiceTier, withBatch)
	item.UpstreamCatalogRoutesByGroup["default"][0].DocumentHash = strings.Repeat("b", 64)
	withNewRates, err := pricingCatalogueFingerprint([]Pricing{item}, nil)
	require.NoError(t, err)
	assert.NotEqual(t, withBatch, withNewRates, "immutable document rate/mode changes invalidate the consumer revision")
	item.CatalogType = "speech"
	withType, err := pricingCatalogueFingerprint([]Pricing{item}, nil)
	require.NoError(t, err)
	assert.NotEqual(t, withNewRates, withType)

}

func TestUpstreamCatalogPassThroughInvalidatesPricing(t *testing.T) {
	global := model_setting.GetGlobalSettings()
	previous := global.PassThroughRequestEnabled
	t.Cleanup(func() { global.PassThroughRequestEnabled = previous; InvalidatePricingCache() })
	global.PassThroughRequestEnabled = false
	generation := pricingInputsGeneration.Load()
	require.True(t, handleConfigUpdate("global.pass_through_request_enabled", "true"))
	assert.True(t, global.PassThroughRequestEnabled)
	assert.Greater(t, pricingInputsGeneration.Load(), generation)
	generation = pricingInputsGeneration.Load()
	require.True(t, handleConfigUpdate("global.pass_through_request_enabled", "true"))
	assert.Equal(t, generation, pricingInputsGeneration.Load())
	require.True(t, handleConfigUpdate("global.pass_through_request_enabled", "false"))
	assert.False(t, global.PassThroughRequestEnabled)
	assert.Greater(t, pricingInputsGeneration.Load(), generation)
}

func TestDecisionConfiguredPriceCompatibility(t *testing.T) {
	expression := `len <= 1000 ? tier("small", p * 2 + c * 4) : tier("large", p * 3 + c * 6)`
	for _, tc := range []struct {
		name       string
		configured bool
		expr       string
		source     string
		priced     bool
	}{
		{"explicit configured tiers", false, expression, BillingSourceConfiguredTariff, true},
		{"explicit configured zero", true, "", BillingSourceConfiguredTariff, true},
		{"reserve alone is not price", false, "", BillingSourceOpenRouterActualCost, false},
		{"unconfigured defaults are not price", false, "", BillingSourceConfiguredTariff, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := Pricing{ModelName: "decision-alias", CatalogModality: "TEXT", CatalogType: "decision", PriceConfigured: tc.configured, QuotaType: 1, BillingExpr: tc.expr, BillingSourcesByGroup: map[string]map[constant.EndpointType][]string{"default": {constant.EndpointTypeOpenAI: {tc.source}}}}
			if tc.expr != "" {
				item.BillingMode = billing_setting.BillingModeTieredExpr
				item.ConfiguredPrices = map[string][][]billingexpr.DisplayPrice{tc.expr: billingexpr.CatalogPrices(tc.expr)}
			}
			channel := &Channel{Id: 43, Type: constant.ChannelTypeOpenRouter, Status: common.ChannelStatusEnabled, ParamOverride: common.GetPointer(`{"openrouter_model_reserve_usd":{"decision-alias":0.05}}`)}
			abilities := []AbilityWithChannel{{Ability: Ability{Model: item.ModelName, Group: "default", ChannelId: 43}}}
			projectPricingOperations(&item, abilities, map[int]*Channel{43: channel}, jsplugin.DefaultRegistry.Generation(), nil, nil)
			assert.Empty(t, item.UpstreamCatalogRoutesByGroup, "compatibility never creates a provider route")
			for _, offers := range []map[string][]CatalogOffer{item.CatalogOffersByGroup, item.PublicCatalogOffersByGroup} {
				require.NotEmpty(t, offers["default"])
				priced := false
				for _, offer := range offers["default"] {
					assert.Equal(t, "text", offer.Operation)
					assert.Equal(t, "sync", offer.Mode)
					priced = priced || len(offer.Prices) > 0
				}
				assert.Equal(t, tc.priced, priced)
			}
		})
	}
}

// TestPricingStorageUpgradeCompatibility opens the same persistent database in
// two separately built binaries: the captured checkout writes it, the refactor
// then migrates twice and reads the exact data through its real pricing APIs.
func TestPricingStorageUpgradeCompatibility(t *testing.T) {
	phase := os.Getenv("PRICING_UPGRADE_PHASE")
	if phase == "" {
		t.Skip("persistent baseline/candidate upgrade fixture not configured")
	}
	kind, dsn := os.Getenv("PRICING_UPGRADE_ENGINE"), os.Getenv("PRICING_UPGRADE_DSN")
	require.NotEmpty(t, dsn)
	var driver gorm.Dialector
	switch kind {
	case "sqlite":
		driver = sqlite.Open(dsn + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	case "mysql":
		driver = mysql.Open(dsn)
	case "postgres":
		driver = postgres.Open(dsn)
	default:
		t.Fatalf("unsupported engine %q", kind)
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	previousDB, previousKey, previousGroup := DB, commonKeyCol, commonGroupCol
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousOptions, previousCatalog := common.OptionMap, pricingCatalogState.Load()
	DB = db
	common.OptionMap = map[string]string{}
	pricingCatalogState.Store(nil)
	common.SetDatabaseTypes(common.DatabaseType(kind), common.DatabaseType(kind))
	commonKeyCol, commonGroupCol = "`key`", "`group`"
	if kind == "postgres" {
		commonKeyCol, commonGroupCol = `"key"`, `"group"`
	}
	t.Setenv(CatalogAPIURLEnv, "")
	t.Cleanup(func() {
		DB, commonKeyCol, commonGroupCol = previousDB, previousKey, previousGroup
		common.OptionMap = previousOptions
		pricingCatalogState.Store(previousCatalog)
		common.SetDatabaseTypes(previousMain, previousLog)
		pool, e := db.DB()
		require.NoError(t, e)
		require.NoError(t, pool.Close())
	})
	for range 2 {
		require.NoError(t, db.AutoMigrate(&Option{}, &Channel{}, &Ability{}, &Model{}, &Vendor{}, &CatalogSource{}, &CatalogRevision{}, &CatalogDocument{}))
	}
	name := "pricing-upgrade/ноль 🚀"
	channel := &Channel{Id: 970, Type: constant.ChannelTypeOpenRouter, Status: common.ChannelStatusEnabled, Name: "upgrade", Key: "unusable-upgrade-fixture", Models: "vendor/model", Group: "default"}
	source := CatalogSource{ID: CatalogHash([]byte("pricing-upgrade")), Kind: "openrouter", BaseURL: "https://catalog.invalid"}
	if phase == "baseline" {
		require.NoError(t, db.Create(channel).Error)
		before, e := GetModelPricingSnapshot([]string{name})
		require.NoError(t, e)
		require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: name, ExpectedVersion: before.Entries[0].Version, Pricing: PricingValues{"ModelRatio": float64(0), "CompletionRatio": float64(0), "CacheRatio": float64(0)}}}))
		zero := float64(0)
		entry := PricingCatalogEntry{ChannelID: channel.Id, UpstreamModel: "vendor/model", Endpoint: constant.EndpointTypeImageGeneration, Source: "https://openrouter.ai/api/v1/images/models/vendor/model/endpoints", FetchedAt: "2026-10-20T00:00:00Z", Fingerprint: PricingCatalogSourceFingerprint(channel, "vendor/model", constant.EndpointTypeImageGeneration), Offers: []CatalogOffer{{ID: "explicit-free", Operation: "image_generation", Mode: "sync", MainComponent: "image_output", Prices: []CatalogPrice{{Component: "image_output", Unit: "image", AmountUSD: &zero}}}}}
		require.NoError(t, PublishPricingCatalogEntry(entry))
		head, e := EnsureCatalogSource(context.Background(), source)
		require.NoError(t, e)
		raw := `{"id":"vendor/model","unknown":null,"zero":0,"name":"Модель 🚀"}`
		document := CatalogDocument{SourceID: source.ID, ModelID: "vendor/model", Payload: raw, Hash: CatalogHash([]byte(raw))}
		_, e = PublishCatalogSelection(context.Background(), head, CatalogSelection{SchemaVersion: 1, NormalizationVersion: "1", Models: []CatalogModelReference{{ID: document.ModelID, Hash: document.Hash}}}, strings.Repeat("a", 64), "2026-10-20T00:00:00Z", []CatalogDocument{document})
		require.NoError(t, e)
	} else {
		require.Equal(t, "candidate", phase)
	}
	var options []Option
	require.NoError(t, db.Order(commonKeyCol).Find(&options).Error)
	for _, option := range options {
		require.NoError(t, PublishOption(option.Key, option.Value))
	}
	pricing, e := GetModelPricingSnapshot([]string{name})
	require.NoError(t, e)
	require.Len(t, pricing.Entries, 1)
	assert.Equal(t, float64(0), pricing.Entries[0].Configured["ModelRatio"])
	assert.Equal(t, float64(0), pricing.Entries[0].Configured["CompletionRatio"])
	require.NoError(t, db.First(channel, "id = ?", channel.Id).Error)
	native, known := GetPricingCatalogEntry(channel, "vendor/model", constant.EndpointTypeImageGeneration)
	require.True(t, known)
	require.NotNil(t, native.Offers[0].Prices[0].AmountUSD)
	assert.Zero(t, *native.Offers[0].Prices[0].AmountUSD)
	var sources []CatalogSource
	var revisions []CatalogRevision
	var documents []CatalogDocument
	require.NoError(t, db.Order("id").Find(&sources).Error)
	require.NoError(t, db.Order("source_id, hash").Find(&revisions).Error)
	require.NoError(t, db.Order("source_id, hash").Find(&documents).Error)
	require.Len(t, sources, 1)
	selection, e := ReadCatalogSelection(context.Background(), source.ID, sources[0].Revision)
	require.NoError(t, e)
	stored, e := GetCatalogDocuments(context.Background(), source.ID, []string{selection.Models[0].Hash})
	require.NoError(t, e)
	require.Equal(t, documents[0].Payload, stored[documents[0].Hash].Payload)
	encoded, e := common.Marshal(struct {
		Options   []Option
		Pricing   *ModelPricingSnapshot
		Native    PricingCatalogEntry
		Sources   []CatalogSource
		Revisions []CatalogRevision
		Documents []CatalogDocument
	}{options, pricing, native, sources, revisions, documents})
	require.NoError(t, e)
	path := os.Getenv("PRICING_UPGRADE_SNAPSHOT")
	require.NotEmpty(t, path)
	if phase == "baseline" {
		require.NoError(t, os.WriteFile(path, encoded, 0600))
	} else {
		baseline, e := os.ReadFile(path)
		require.NoError(t, e)
		assert.JSONEq(t, string(baseline), string(encoded))
		require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: name, ExpectedVersion: pricing.Entries[0].Version, Pricing: PricingValues{"ModelRatio": float64(1)}}}))
		require.ErrorIs(t, UpdateModelPricing([]ModelPricingChange{{ModelName: name, ExpectedVersion: pricing.Entries[0].Version, Pricing: PricingValues{"ModelRatio": float64(2)}}}), ErrModelPricingConflict)
		require.Error(t, db.Create(&Option{Key: options[0].Key, Value: "duplicate"}).Error)
		require.Error(t, db.Create(&documents[0]).Error)
	}
	var version string
	query := "SELECT version()"
	if kind == "sqlite" {
		query = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("engine=%s version=%s phase=%s", kind, version, phase)
}
