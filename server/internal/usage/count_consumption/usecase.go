package count_consumption

type Counter interface {
	AddUserConsumption(int, int)
	AddChannelConsumption(int, int)
}
type Usecase struct{ counter Counter }

func NewUsecase(counter Counter) Usecase { return Usecase{counter: counter} }

type Moment int

const (
	BeforeSettlement Moment = iota
	AfterSettlement
)

type Input struct {
	Moment                    Moment
	UserID, ChannelID         int
	Billable, ProviderCost    bool
	QuotedQuota, AppliedQuota int
}

// Execute is deliberately void: counters enqueue batch updates or log direct-write errors without reporting them.
func (u Usecase) Execute(in Input) {
	quota := in.QuotedQuota
	switch in.Moment {
	case BeforeSettlement:
		if in.ProviderCost || !in.Billable {
			return
		}
	case AfterSettlement:
		if !in.ProviderCost {
			return
		}
		quota = in.AppliedQuota
	default:
		return
	}
	u.counter.AddUserConsumption(in.UserID, quota)
	u.counter.AddChannelConsumption(in.ChannelID, quota)
}
