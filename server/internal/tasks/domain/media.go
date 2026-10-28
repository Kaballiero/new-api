package domain

// MediaRequest is the provider-selected outbound content descriptor.
type MediaRequest struct {
	URL            string
	Method         string
	Headers        map[string]string
	Body           []byte
	Credentialless bool
}

type MediaTask struct {
	TaskID    string
	ChannelID int
}

// MediaError retains the content endpoint's existing status and public error.
type MediaError struct {
	MessageID   string
	MessageData map[string]any
	Status      int
	Code        string
	Message     string
	Cause       error
}

func (e *MediaError) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return e.Message + ": " + e.Cause.Error()
}
func (e *MediaError) Unwrap() error { return e.Cause }
