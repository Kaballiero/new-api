package get_ratio_config

type Settings interface {
	Exposed() bool
	SyncData() map[string]any
}
type Usecase struct{ settings Settings }

func NewUsecase(settings Settings) Usecase { return Usecase{settings: settings} }
func (u Usecase) Execute() (Output, bool) {
	if !u.settings.Exposed() {
		return nil, false
	}
	return u.settings.SyncData(), true
}
