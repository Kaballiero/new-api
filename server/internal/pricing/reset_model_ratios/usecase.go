package reset_model_ratios

type Settings interface {
	DefaultRatios() string
	Persist(string, string) error
	PublishRatios(string) error
}
type Usecase struct{ settings Settings }

func NewUsecase(settings Settings) Usecase { return Usecase{settings: settings} }
func (u Usecase) Execute() Output {
	value := u.settings.DefaultRatios()
	if err := u.settings.Persist("ModelRatio", value); err != nil {
		return Output{Message: err.Error()}
	}
	if err := u.settings.PublishRatios(value); err != nil {
		return Output{Message: err.Error()}
	}
	return Output{Success: true, Message: "重置模型倍率成功"}
}
