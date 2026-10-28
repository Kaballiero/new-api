package update_billing_preference

// Writer preserves all user settings except the chosen preference and keeps the existing cache write.
type Writer interface {
	PutBillingPreference(userID int, preference string) error
}
type Input struct {
	UserID     int
	Preference string
}
type Usecase struct {
	writer    Writer
	normalize func(string) string
}

func NewUsecase(writer Writer, normalize func(string) string) Usecase {
	return Usecase{writer: writer, normalize: normalize}
}
func (u Usecase) Execute(in Input) (string, error) {
	preference := u.normalize(in.Preference)
	if err := u.writer.PutBillingPreference(in.UserID, preference); err != nil {
		return "", err
	}
	return preference, nil
}
