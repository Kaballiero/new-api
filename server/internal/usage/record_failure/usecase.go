package record_failure

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/usage/domain"
)

type Writer interface {
	WriteFailure(context.Context, domain.Failure) error
}
type Renderer interface{ RenderNote(domain.Note) string }
type Usecase struct {
	writer   Writer
	renderer Renderer
	enabled  func() bool
}

func NewUsecase(writer Writer, renderer Renderer, enabled func() bool) Usecase {
	return Usecase{writer: writer, renderer: renderer, enabled: enabled}
}

type Step int

const (
	AdmitUpstream Step = iota
	Persist
)

// Admission captures the early upstream gate before attempt metadata and timing.
// Only Execute can grant admission; the zero value denies a write.
type Admission struct{ allowed bool }

func (a Admission) Allowed() bool { return a.allowed }

type Input struct {
	Step      Step
	Failure   domain.Failure
	Admission Admission
}

func (u Usecase) Execute(ctx context.Context, in Input) (Admission, error) {
	switch in.Step {
	case AdmitUpstream:
		return Admission{allowed: u.enabled()}, nil
	case Persist:
		failure := in.Failure
		if failure.Consumption != nil {
			if failure.DeliveryReason == "" || !u.enabled() {
				return Admission{}, nil
			}
			failure.Record = failure.Consumption.Record
			failure.Content = u.renderer.RenderNote(domain.Note{
				Kind: domain.DeliveryFailed, Name: failure.DeliveryReason,
			})
		} else if !in.Admission.allowed {
			return Admission{}, nil
		}
		return Admission{}, u.writer.WriteFailure(ctx, failure)
	default:
		return Admission{}, errors.New("invalid failure recording step")
	}
}
