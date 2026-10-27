package charge_violation_fee

type Settings struct {
	Enabled bool
	Amount  func() float64
}
type Input struct {
	Chargeable bool
	GroupRatio float64
}
type Result struct {
	Charged bool
	Quota   int
	Amount  float64
}
type Usecase struct {
	settings func() Settings
	quota    func(float64, float64) int
}

func NewUsecase(settings func() Settings, quota func(float64, float64) int) Usecase {
	return Usecase{settings: settings, quota: quota}
}

// Execute keeps fee admission and deduction in their original order. Apply is
// request-bound because it mutates the active billing session and quota ledgers.
func (u Usecase) Execute(in Input, apply func(int) error) (Result, error) {
	if !in.Chargeable {
		return Result{}, nil
	}
	settings := u.settings()
	if !settings.Enabled {
		return Result{}, nil
	}
	quota := u.quota(settings.Amount(), in.GroupRatio)
	if quota <= 0 {
		return Result{}, nil
	}
	if err := apply(quota); err != nil {
		return Result{}, err
	}
	return Result{Charged: true, Quota: quota, Amount: settings.Amount()}, nil
}
