package record_consumption

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/shopspring/decimal"
)

type Writer interface {
	WriteConsumption(context.Context, domain.Consumption) error
}
type Renderer interface {
	RenderNote(domain.Note) string
	RenderContent([]domain.Note) string
}
type AudioPrices struct{ Completion, Input, Output func(string) float64 }
type Usecase struct {
	writer      Writer
	renderer    Renderer
	audioPrices AudioPrices
}

func NewUsecase(writer Writer, renderer Renderer, prices AudioPrices) Usecase {
	return Usecase{writer: writer, renderer: renderer, audioPrices: prices}
}

type Step int

const (
	Describe Step = iota
	Persist
)

type AudioDescription struct {
	InputModel         string
	UsePrice, Billable bool
}
type Input struct {
	Step        Step
	Consumption domain.Consumption
	Extra       []string
	Text        domain.TextDescription
	Audio       AudioDescription
}

// Execute has explicit steps around the gateway-owned billing boundary. Describe
// freezes late display reads before settlement; Persist finalizes and writes.
func (u Usecase) Execute(ctx context.Context, in Input) (domain.Consumption, error) {
	consumption := in.Consumption
	if in.Step != Describe && in.Step != Persist {
		return consumption, errors.New("invalid consumption recording step")
	}
	if in.Step == Describe {
		var notes []domain.Note
		switch consumption.Kind {
		case domain.TextConsumption:
			for _, extra := range in.Extra {
				notes = append(notes, domain.Note{Kind: domain.LiteralContent, Name: extra})
			}
			notes = append(notes, domain.TextNotes(in.Text)...)
		case domain.AudioConsumption:
			if consumption.Audio == nil {
				return consumption, errors.New("audio consumption requires audio facts")
			}
			audioFacts := *consumption.Audio
			consumption.Audio = &audioFacts
			model := consumption.Record.ModelName
			completion := u.audioPrices.Completion(model)
			audio := u.audioPrices.Input(in.Audio.InputModel)
			output := u.audioPrices.Output(model)
			// Preserve the old decimal display projection after the three late reads.
			consumption.Prices.CompletionRatio = decimal.NewFromFloat(completion).InexactFloat64()
			consumption.Audio.AudioRatio = decimal.NewFromFloat(audio).InexactFloat64()
			consumption.Audio.AudioCompletionRatio = decimal.NewFromFloat(output).InexactFloat64()
			notes = domain.AudioNotes(domain.AudioDescription{
				Prices:               consumption.Prices,
				AudioRatio:           consumption.Audio.AudioRatio,
				AudioCompletionRatio: consumption.Audio.AudioCompletionRatio,
				UsePrice:             in.Audio.UsePrice, Billable: in.Audio.Billable,
			})
			for _, extra := range in.Extra {
				if extra != "" {
					notes = append(notes, domain.Note{Kind: domain.LiteralContent, Name: extra})
				}
			}
		}
		// Freeze only template rendering here, preserving both settings-read timing
		// and empty extra notes. Joining happens once, in the renderer at Persist.
		consumption.Notes = make([]domain.Note, len(notes))
		for i, note := range notes {
			consumption.Notes[i] = domain.Note{
				Kind: domain.LiteralContent, Name: u.renderer.RenderNote(note), Join: note.Join,
			}
		}
		return consumption, nil
	}
	if consumption.Kind == domain.TextConsumption && consumption.Text != nil {
		model, notes := domain.FinalTextNotes(consumption.Record.ModelName, consumption.Text.Outcome)
		consumption.Record.ModelName = model
		consumption.Notes = append(consumption.Notes, notes...)
	}
	if consumption.Kind == domain.ChannelTestConsumption {
		consumption.Notes = []domain.Note{{Kind: domain.LiteralContent, Name: consumption.ProbeLabel}}
	}
	consumption.RenderedContent = u.renderer.RenderContent(consumption.Notes)
	return consumption, u.writer.WriteConsumption(ctx, consumption)
}
