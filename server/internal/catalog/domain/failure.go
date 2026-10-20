package domain

type Failure struct {
	Kind, Message string
	Cause         error
	Counts        map[int]int64
}

func (f *Failure) Error() string {
	if f.Cause != nil {
		return f.Cause.Error()
	}
	return f.Message
}
