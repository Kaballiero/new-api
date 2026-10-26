package domain

// ModelMappingCodec preserves the persisted model mapping representation.
type ModelMappingCodec interface {
	DecodeModelMapping(string) (map[string]string, error)
	EncodeModelMapping(map[string]string) (string, error)
}

// OtherInfoCodec reads and writes status metadata, including decode diagnostics.
type OtherInfoCodec interface {
	ReadOtherInfo(*Channel) map[string]any
	WriteOtherInfo(*Channel, map[string]any)
}

// CodexKeyCodec reads and writes the existing OAuth credential representation.
type CodexKeyCodec interface {
	DecodeCodexKey(string) (*CodexOAuthKey, error)
	EncodeCodexKey(*CodexOAuthKey) (string, error)
}
