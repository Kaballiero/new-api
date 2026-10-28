package record

// jsonScanBytes accepts the JSON values returned by the supported SQL drivers.
func jsonScanBytes(value any) []byte {
	switch v := value.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	default:
		return nil
	}
}
