package reset_model_ratios

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeSettings struct {
	persistErr, publishErr error
	calls                  []string
}

func (s *fakeSettings) DefaultRatios() string { return `{"gpt":1}` }
func (s *fakeSettings) Persist(key, value string) error {
	s.calls = append(s.calls, "persist "+key+" "+value)
	return s.persistErr
}
func (s *fakeSettings) PublishRatios(value string) error {
	s.calls = append(s.calls, "publish "+value)
	return s.publishErr
}

func TestExecutePersistsBeforePublishing(t *testing.T) {
	settings := &fakeSettings{}
	assert.NoError(t, NewUsecase(settings).Execute())
	assert.Equal(t, []string{`persist ModelRatio {"gpt":1}`, `publish {"gpt":1}`}, settings.calls)

	settings = &fakeSettings{persistErr: errors.New("db down")}
	assert.EqualError(t, NewUsecase(settings).Execute(), "db down")
	assert.Equal(t, []string{`persist ModelRatio {"gpt":1}`}, settings.calls)

	settings = &fakeSettings{publishErr: errors.New("bad json")}
	assert.EqualError(t, NewUsecase(settings).Execute(), "bad json")
}
