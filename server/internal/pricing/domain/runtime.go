package domain

const (
	BillingModeRatio      = "ratio"
	BillingModeTieredExpr = "tiered_expr"
)

type OverrideContext struct {
	UserID                                       int
	UserGroup, UsingGroup, Model, Path, Upstream string
}
type ProjectionRuntime struct {
	UsableGroups  func(string) map[string]string
	GroupRatio    func(string, string) float64
	AutoGroups    func(string) []string
	ApplyFX       func(float64, float64) (float64, error)
	QuotaUnit     func() float64
	ApplyOverride func([]byte, map[string]any, OverrideContext) ([]byte, error)
}
type Projection struct{ runtime ProjectionRuntime }

func NewProjection(runtime ProjectionRuntime) *Projection { return &Projection{runtime: runtime} }
