package get_affinity_cache_stats

type Usecase struct{ read func() Output }

func NewUsecase(read func() Output) Usecase { return Usecase{read: read} }
func (usecase Usecase) Execute() Output     { return usecase.read() }
