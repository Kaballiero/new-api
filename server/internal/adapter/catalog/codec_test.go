package catalog_test

import (
	"testing"

	catalogcodec "github.com/QuantumNous/new-api/internal/adapter/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogCodecParity(t *testing.T) {
	codec := catalogcodec.Codec{}
	t.Run("record_versions", func(t *testing.T) {
		// These digests use the pre-extraction JSON inputs, including nil pointers and field order.
		assert.Equal(t, "fe44a8cccd89edae70b54c5e58399979bd7d8b12643a8396ad62d089a781b692", codec.MetadataRecordVersion(nil, nil, nil))
		assert.Equal(t, "9a82c970f4bfa7f5d9e075dcd108b350ae77fa87acc735ef490eff5729bf1e6a", codec.MetadataRecordVersion(&domain.Model{}, nil, nil))
		vendor := &domain.Vendor{Id: 7, Name: " A<&> ", Description: "описание", Icon: "icon", CreatedTime: 12, UpdatedTime: 13}
		assert.Equal(t, "61f1c7e755bdd11ce94dcddb76f58605d975326d59a7c09f6201e2983e356ba9", codec.VendorRecordVersion(vendor))
		vendor.Version, vendor.ModelCount = "display-only", 9
		assert.Equal(t, "61f1c7e755bdd11ce94dcddb76f58605d975326d59a7c09f6201e2983e356ba9", codec.VendorRecordVersion(vendor))
	})
	t.Run("operation_version_nil_and_empty", func(t *testing.T) {
		version, err := codec.VendorOperationVersion(&domain.VendorOperationPreview{}, nil)
		require.NoError(t, err)
		assert.Equal(t, "cc20701d11dc91a444b5febc98ad3933a0d828f0748836cfa0f469c693841b40", version)
		preview := &domain.VendorOperationPreview{Sources: []domain.Vendor{}, Models: []domain.VendorAssignmentModel{}}
		version, err = codec.VendorOperationVersion(preview, []string{})
		require.NoError(t, err)
		assert.Equal(t, "d2ccf5ad93461b362dbba163844b0a6ed7c365ba1bd5e075c9a382ae8b9a8972", version)
	})
	t.Run("endpoint_rules", func(t *testing.T) {
		for _, tc := range []struct{ raw, message string }{
			{"", ""}, {" \n", ""}, {`[]`, ""}, {`{}`, ""}, {`["openai"]`, ""},
			{`{"openai":"/chat"}`, ""}, {`{"openai":{"path":"/chat"}}`, ""},
			{`{"openai":{"path":"/chat","method":"OPTIONS","ignored":false}}`, ""},
			{`null`, "endpoints must be a JSON object or array"}, {`false`, "endpoints must be a JSON object or array"},
			{`[0]`, "endpoint types must be non-empty strings"}, {`[" "]`, "endpoint types must be non-empty strings"},
			{`{" ":"/"}`, "endpoint type is required"}, {`{"openai":false}`, "endpoint configuration must be a path or object"},
			{`{"openai":"chat"}`, "endpoint paths must start with /"}, {`{"openai":{"path":null}}`, "endpoint paths must start with /"},
			{`{"openai":{"path":"/","method":null}}`, "invalid endpoint HTTP method"},
			{`{"openai":{"path":"/","method":{}}}`, "invalid endpoint HTTP method"},
			{`{"openai":{"path":"/","method":"post"}}`, "invalid endpoint HTTP method"},
		} {
			t.Run(tc.raw, func(t *testing.T) {
				err := domain.ValidateModelEndpoints(tc.raw, codec.DecodeEndpoints)
				if tc.message == "" {
					require.NoError(t, err)
				} else {
					require.EqualError(t, err, tc.message)
				}
			})
		}
		err := domain.ValidateModelEndpoints(`{`, codec.DecodeEndpoints)
		require.ErrorContains(t, err, "invalid endpoints: ")
	})
	t.Run("preview_fields", func(t *testing.T) {
		assert.Equal(t, map[string]any{"description": "", "icon": "", "tags": "", "endpoints": "", "vendor": "", "name_rule": float64(0), "status": float64(0)}, codec.MetadataFields(domain.MetadataValues{}))
	})
}
