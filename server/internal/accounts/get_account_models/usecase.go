package get_account_models

type ModelAccountGroupReader interface {
	AccountGroup(int) (string, error)
}

type AccountModelsSelection interface {
	ModelGroups(string) []string
	EnabledModels([]string) []string
}

// ReadAccountModels выбирает группы после чтения аккаунта и возвращает доступные модели.
func ReadAccountModels(userID int, reader ModelAccountGroupReader, selection AccountModelsSelection) ([]string, error) {
	group, err := reader.AccountGroup(userID)
	if err != nil {
		return nil, err
	}
	groups := selection.ModelGroups(group)
	return selection.EnabledModels(groups), nil
}

type Usecase struct {
	reader    ModelAccountGroupReader
	selection AccountModelsSelection
}

func NewUsecase(reader ModelAccountGroupReader, selection AccountModelsSelection) Usecase {
	return Usecase{reader: reader, selection: selection}
}
func (u Usecase) Execute(input Input) ([]string, error) {
	return ReadAccountModels(input.UserID, u.reader, u.selection)
}
