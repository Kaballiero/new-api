package system

import "github.com/QuantumNous/new-api/internal/system/domain"

type Metrics struct {
	QueryMetrics func(domain.QueryParams) (domain.QueryResult, error)
	QuerySummary func(int, []string) (domain.SummaryAllResult, error)
}

func (m Metrics) Query(p domain.QueryParams) (domain.QueryResult, error) { return m.QueryMetrics(p) }
func (m Metrics) Summary(h int, g []string) (domain.SummaryAllResult, error) {
	return m.QuerySummary(h, g)
}
