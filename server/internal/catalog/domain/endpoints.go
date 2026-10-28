package domain

type EndpointDocument struct {
	Array   bool
	Object  bool
	Types   []EndpointTypeFact
	Entries []EndpointEntry
}
type EndpointTypeFact struct {
	Text     string
	IsString bool
}
type EndpointEntry struct {
	Type           string
	Path           string
	IsPath         bool
	IsObject       bool
	Method         string
	HasMethod      bool
	MethodIsString bool
}
type EndpointDecoder func(string) (EndpointDocument, error)

type OperationVersions interface {
	MetadataRecordVersion(*Model, *Vendor, *Vendor) string
	VendorOperationVersion(*VendorOperationPreview, []string) (string, error)
}
