package reset_model_ratios

type Settings interface {
	DefaultRatios() string
	Persist(string, string) error
	PublishRatios(string) error
}
type Usecase struct{ settings Settings }

func NewUsecase(settings Settings) Usecase { return Usecase{settings: settings} }

// Execute persists the default model ratios and publishes them to the runtime.
func (u Usecase) Execute() error {
	value := u.settings.DefaultRatios()
	if err := u.settings.Persist("ModelRatio", value); err != nil {
		return err
	}
	return u.settings.PublishRatios(value)
}
