package aliases

import "sync/atomic"

func ViewPointerForTest(v *View) *atomic.Pointer[taskAliasView] { return &v.ptr }
func StaleViewForTest(values map[string]TaskAliasTarget) *taskAliasView {
	return &taskAliasView{byFold: values}
}
func FoldedAliasesForTest(v *taskAliasView) map[string]TaskAliasTarget { return v.byFold }
