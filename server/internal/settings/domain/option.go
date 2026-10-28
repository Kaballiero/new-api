package domain

type Option struct {
	Key   string
	Value string
}

// PublicStatus contains technical facts read while holding the existing option read lock.
type PublicStatus struct {
	Values map[string]any
	Panels []Panel
}
type Panel struct {
	Key     string
	Enabled bool
	Read    func() any
}
