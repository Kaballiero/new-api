package pricing

type RatioSettings struct {
	Expose   func() bool
	Data     func() map[string]any
	Defaults func() string
	Write    func(string, string) error
	Update   func(string) error
}

func (s RatioSettings) Exposed() bool                    { return s.Expose() }
func (s RatioSettings) SyncData() map[string]any         { return s.Data() }
func (s RatioSettings) DefaultRatios() string            { return s.Defaults() }
func (s RatioSettings) Persist(key, value string) error  { return s.Write(key, value) }
func (s RatioSettings) PublishRatios(value string) error { return s.Update(value) }
