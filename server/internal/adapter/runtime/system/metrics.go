package system

import (
	"github.com/QuantumNous/new-api/internal/system/domain"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
)

type Metrics struct{}

func (Metrics) Query(p domain.QueryParams) (domain.QueryResult, error) {
	v, e := perfmetrics.Query(perfmetrics.QueryParams{Model: p.Model, Group: p.Group, Hours: p.Hours})
	out := domain.QueryResult{ModelName: v.ModelName, SeriesSchema: v.SeriesSchema}
	if v.Groups != nil {
		out.Groups = make([]domain.GroupResult, len(v.Groups))
		for i, g := range v.Groups {
			r := domain.GroupResult{Group: g.Group, AvgTtftMs: g.AvgTtftMs, AvgLatencyMs: g.AvgLatencyMs, SuccessRate: g.SuccessRate, AvgTps: g.AvgTps}
			if g.Series != nil {
				r.Series = make([]domain.BucketPoint, len(g.Series))
				for j, p := range g.Series {
					r.Series[j] = domain.BucketPoint{Ts: p.Ts, AvgTtftMs: p.AvgTtftMs, AvgLatencyMs: p.AvgLatencyMs, SuccessRate: p.SuccessRate, AvgTps: p.AvgTps}
				}
			}
			out.Groups[i] = r
		}
	}
	return out, e
}
func (Metrics) Summary(hours int, groups []string) (domain.SummaryAllResult, error) {
	v, e := perfmetrics.QuerySummaryAll(hours, groups)
	out := domain.SummaryAllResult{}
	if v.Models != nil {
		out.Models = make([]domain.ModelSummary, len(v.Models))
		for i, m := range v.Models {
			r := domain.ModelSummary{ModelName: m.ModelName, AvgLatencyMs: m.AvgLatencyMs, SuccessRate: m.SuccessRate, AvgTps: m.AvgTps, RequestCount: m.RequestCount}
			if m.RecentSuccessSeries != nil {
				r.RecentSuccessSeries = make([]domain.SuccessRatePoint, len(m.RecentSuccessSeries))
				for j, p := range m.RecentSuccessSeries {
					r.RecentSuccessSeries[j] = domain.SuccessRatePoint{Ts: p.Ts, SuccessRate: p.SuccessRate}
				}
			}
			out.Models[i] = r
		}
	}
	return out, e
}
