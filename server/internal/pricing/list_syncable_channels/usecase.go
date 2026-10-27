package list_syncable_channels

type Channels interface {
	All() ([]SyncableChannel, error)
}
type Usecase struct{ channels Channels }

func NewUsecase(channels Channels) Usecase { return Usecase{channels: channels} }
func (u Usecase) Execute() (Output, error) {
	channels, err := u.channels.All()
	if err != nil {
		return nil, err
	}
	var out Output
	for _, ch := range channels {
		if ch.BaseURL != "" {
			out = append(out, ch)
		}
	}
	// The preset names are identifiers the client matches; they are not localized.
	out = append(out, SyncableChannel{ID: -100, Name: "官方倍率预设", BaseURL: "https://basellm.github.io", Status: 1})
	out = append(out, SyncableChannel{ID: -101, Name: "models.dev 价格预设", BaseURL: "https://models.dev", Status: 1})
	return out, nil
}
