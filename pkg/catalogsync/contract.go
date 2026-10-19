// Package catalogsync consumes immutable upstream catalog facts. It never
// performs provider routing, request admission or billing settlement.
package catalogsync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

const SchemaVersion = 1
const MaxModels = 10000

type Reference struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}
type Manifest struct {
	SchemaVersion        int         `json:"schemaVersion"`
	NormalizationVersion string      `json:"normalizationVersion"`
	Revision             string      `json:"revision"`
	SourceRevision       string      `json:"sourceRevision"`
	FetchedAt            string      `json:"fetchedAt"`
	Models               []Reference `json:"models"`
}
type Coverage struct {
	Offers          string   `json:"offers"`
	Normalization   string   `json:"normalization"`
	Reasons         []string `json:"reasons"`
	ExpectedSources []string `json:"expectedSources"`
	MissingSources  []string `json:"missingSources"`
}
type Provider struct {
	ID   *string `json:"id"`
	Name *string `json:"name"`
	Tag  *string `json:"tag"`
}
type Alias struct {
	TargetID string `json:"targetId"`
}
type Author struct {
	Namespace string `json:"namespace"`
}
type Modalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}
type Document struct {
	SchemaVersion int             `json:"schemaVersion"`
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Description   *string         `json:"description"`
	Author        *Author         `json:"author"`
	Type          string          `json:"type"`
	Modalities    Modalities      `json:"modalities"`
	Metadata      json.RawMessage `json:"metadata"`
	Capabilities  json.RawMessage `json:"capabilities"`
	Limits        json.RawMessage `json:"limits"`
	Alias         *Alias          `json:"alias"`
	Offers        []Offer         `json:"offers"`
	Coverage      Coverage        `json:"coverage"`
	Limitations   []string        `json:"limitations"`
	Provenance    json.RawMessage `json:"provenance"`
}
type Eligibility struct {
	Eligible bool     `json:"eligible"`
	Reasons  []string `json:"reasons"`
}
type Offer struct {
	ID           string          `json:"id"`
	Scope        string          `json:"scope"`
	Identity     json.RawMessage `json:"identity"`
	Provider     *Provider       `json:"provider"`
	Operation    *string         `json:"operation"`
	Mode         *string         `json:"mode"`
	Variant      json.RawMessage `json:"variant"`
	Capabilities json.RawMessage `json:"capabilities"`
	Limits       json.RawMessage `json:"limits"`
	Eligibility  *Eligibility    `json:"eligibility"`
	Components   []Component     `json:"components"`
	Coverage     Coverage        `json:"coverage"`
	Limitations  []string        `json:"limitations"`
	Facts        json.RawMessage `json:"facts"`
}
type Component struct {
	Parameter   string          `json:"parameter"`
	ComponentID string          `json:"componentId"`
	Billable    *string         `json:"billable"`
	Direction   *string         `json:"direction"`
	Modality    *string         `json:"modality"`
	AmountUSD   *string         `json:"amountUSD"`
	Unit        string          `json:"unit"`
	Quantity    string          `json:"quantity"`
	Variant     json.RawMessage `json:"variant"`
	Conditions  json.RawMessage `json:"conditions"`
	Source      struct {
		Amount   *string `json:"amount"`
		Currency *string `json:"currency"`
		Unit     *string `json:"unit"`
		Key      string  `json:"key"`
	} `json:"source"`
	Provenance    json.RawMessage `json:"provenance"`
	Normalization struct {
		Comparable      bool     `json:"comparable"`
		Effective       bool     `json:"effective"`
		Reasons         []string `json:"reasons"`
		DiscountBasis   string   `json:"discountBasis"`
		AppliedDiscount *string  `json:"appliedDiscount"`
	} `json:"normalization"`
}

func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func ValidHash(hash string) bool {
	if len(hash) != 64 || hash != strings.ToLower(hash) {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

// CompareIDs follows JavaScript code-unit sorting, including non-BMP names.
func CompareIDs(a, b string) int {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion || m.NormalizationVersion == "" || len(m.NormalizationVersion) > 128 || !ValidHash(m.Revision) || !ValidHash(m.SourceRevision) || len(m.Models) > MaxModels || m.Models == nil {
		return fmt.Errorf("invalid catalog manifest identity")
	}
	if _, err := time.Parse(time.RFC3339Nano, m.FetchedAt); err != nil {
		return fmt.Errorf("invalid catalog manifest fetchedAt")
	}
	for i, entry := range m.Models {
		if entry.ID == "" || len(entry.ID) > 1024 || !ValidHash(entry.Hash) || i > 0 && CompareIDs(m.Models[i-1].ID, entry.ID) >= 0 {
			return fmt.Errorf("invalid, duplicate or unsorted catalog model reference")
		}
	}
	// Reference fields also need canonical hash-before-id key ordering.
	type canonicalReference struct {
		Hash string `json:"hash"`
		ID   string `json:"id"`
	}
	refs := make([]canonicalReference, len(m.Models))
	for i, entry := range m.Models {
		refs[i] = canonicalReference{entry.Hash, entry.ID}
	}
	raw, err := common.Marshal(struct {
		Models               []canonicalReference `json:"models"`
		NormalizationVersion string               `json:"normalizationVersion"`
		SchemaVersion        int                  `json:"schemaVersion"`
	}{refs, m.NormalizationVersion, m.SchemaVersion})
	if err != nil {
		return err
	}
	// Go's JSON wrapper escapes HTML and line separators; JSON.stringify does not.
	// Process escape tokens, preserving literal backslash-u sequences unchanged.
	var canonical bytes.Buffer
	for i := 0; i < len(raw); {
		if raw[i] == '\\' && i+1 < len(raw) {
			if i+6 <= len(raw) {
				switch string(raw[i : i+6]) {
				case `\u0026`:
					canonical.WriteByte('&')
					i += 6
					continue
				case `\u003c`:
					canonical.WriteByte('<')
					i += 6
					continue
				case `\u003e`:
					canonical.WriteByte('>')
					i += 6
					continue
				case `\u2028`:
					canonical.WriteRune('\u2028')
					i += 6
					continue
				case `\u2029`:
					canonical.WriteRune('\u2029')
					i += 6
					continue
				}
			}
			canonical.Write(raw[i : i+2])
			i += 2
			continue
		}
		canonical.WriteByte(raw[i])
		i++
	}
	canonical.WriteByte('\n')
	if Hash(canonical.Bytes()) != m.Revision {
		return fmt.Errorf("catalog manifest revision mismatch")
	}
	return nil
}

var exactDecimal = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)

func DecodeDocument(raw []byte, ref Reference) (*Document, error) {
	if Hash(raw) != ref.Hash {
		return nil, fmt.Errorf("catalog document hash mismatch")
	}
	if err := validateDocumentShape(raw); err != nil {
		return nil, err
	}
	var d Document
	if err := common.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("invalid catalog document JSON: %w", err)
	}
	if d.SchemaVersion != SchemaVersion || d.ID != ref.ID || d.Name == "" || d.Type == "" || d.Offers == nil || d.Modalities.Input == nil || d.Modalities.Output == nil {
		return nil, fmt.Errorf("invalid catalog document identity or shape")
	}
	if d.Alias != nil && (d.Alias.TargetID == "" || d.Alias.TargetID == d.ID || len(d.Offers) != 0) {
		return nil, fmt.Errorf("invalid catalog alias")
	}
	for _, rawObject := range []json.RawMessage{d.Metadata, d.Capabilities, d.Limits, d.Provenance} {
		if common.GetJsonType(rawObject) != "object" {
			return nil, fmt.Errorf("invalid catalog metadata object")
		}
	}
	for _, offer := range d.Offers {
		if offer.ID == "" || !slices.Contains([]string{"model", "endpoint"}, offer.Scope) || offer.Components == nil {
			return nil, fmt.Errorf("invalid catalog offer")
		}
		for _, component := range offer.Components {
			if component.ComponentID == "" || component.Parameter == "" || component.Unit == "" || len(component.Quantity) > 256 || !exactDecimal.MatchString(component.Quantity) {
				return nil, fmt.Errorf("invalid catalog component")
			}
			quantity, err := decimal.NewFromString(component.Quantity)
			if err != nil || !quantity.IsPositive() {
				return nil, fmt.Errorf("invalid catalog rate quantity")
			}
			for _, amount := range []*string{component.AmountUSD, component.Source.Amount, component.Normalization.AppliedDiscount} {
				if amount != nil && (len(*amount) > 256 || !exactDecimal.MatchString(*amount)) {
					return nil, fmt.Errorf("invalid exact catalog decimal")
				}
			}
			if common.GetJsonType(component.Conditions) != "object" || common.GetJsonType(component.Variant) != "object" || common.GetJsonType(component.Provenance) != "object" {
				return nil, fmt.Errorf("invalid catalog component conditions")
			}
		}
	}
	return &d, nil
}

// ResolveAlias follows only declarations from one verified snapshot. A missing
// target or cycle must not be replaced with guessed model-name normalization.
func ResolveAlias(documents map[string]*Document, id string) (*Document, error) {
	seen := make(map[string]bool)
	for {
		if seen[id] {
			return nil, fmt.Errorf("catalog alias cycle")
		}
		seen[id] = true
		doc := documents[id]
		if doc == nil {
			return nil, fmt.Errorf("catalog alias target is missing")
		}
		if doc.Alias == nil {
			return doc, nil
		}
		id = doc.Alias.TargetID
	}
}
