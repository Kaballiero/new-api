package systemmetrics

import (
	"github.com/QuantumNous/new-api/internal/system/domain"
	"sync"
	"sync/atomic"
)

type Buckets struct{ sync.Map }
type Bucket struct {
	requestCount   atomic.Int64
	successCount   atomic.Int64
	totalLatencyMs atomic.Int64
	ttftSumMs      atomic.Int64
	ttftCount      atomic.Int64
	outputTokens   atomic.Int64
	generationMs   atomic.Int64
}

func (b *Bucket) Add(sample domain.Sample) {
	b.requestCount.Add(1)
	if sample.Success {
		b.successCount.Add(1)
	}
	if sample.LatencyMs > 0 {
		b.totalLatencyMs.Add(sample.LatencyMs)
	}
	if sample.HasTtft && sample.TtftMs >= 0 {
		b.ttftSumMs.Add(sample.TtftMs)
		b.ttftCount.Add(1)
	}
	if sample.OutputTokens > 0 && sample.GenerationMs > 0 {
		b.outputTokens.Add(sample.OutputTokens)
		b.generationMs.Add(sample.GenerationMs)
	}
}

func (b *Bucket) Snapshot() domain.MetricCounters {
	return domain.MetricCounters{
		RequestCount:   b.requestCount.Load(),
		SuccessCount:   b.successCount.Load(),
		TotalLatencyMs: b.totalLatencyMs.Load(),
		TtftSumMs:      b.ttftSumMs.Load(),
		TtftCount:      b.ttftCount.Load(),
		OutputTokens:   b.outputTokens.Load(),
		GenerationMs:   b.generationMs.Load(),
	}
}

func (b *Bucket) Drain() domain.MetricCounters {
	return domain.MetricCounters{
		RequestCount:   b.requestCount.Swap(0),
		SuccessCount:   b.successCount.Swap(0),
		TotalLatencyMs: b.totalLatencyMs.Swap(0),
		TtftSumMs:      b.ttftSumMs.Swap(0),
		TtftCount:      b.ttftCount.Swap(0),
		OutputTokens:   b.outputTokens.Swap(0),
		GenerationMs:   b.generationMs.Swap(0),
	}
}

func (b *Bucket) Restore(c domain.MetricCounters) {
	if c.RequestCount != 0 {
		b.requestCount.Add(c.RequestCount)
	}
	if c.SuccessCount != 0 {
		b.successCount.Add(c.SuccessCount)
	}
	if c.TotalLatencyMs != 0 {
		b.totalLatencyMs.Add(c.TotalLatencyMs)
	}
	if c.TtftSumMs != 0 {
		b.ttftSumMs.Add(c.TtftSumMs)
	}
	if c.TtftCount != 0 {
		b.ttftCount.Add(c.TtftCount)
	}
	if c.OutputTokens != 0 {
		b.outputTokens.Add(c.OutputTokens)
	}
	if c.GenerationMs != 0 {
		b.generationMs.Add(c.GenerationMs)
	}
}
