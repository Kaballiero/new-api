package fx

func ClearForTest() {
	modelCostFXState.Lock()
	modelCostFXState.snapshots = nil
	modelCostFXState.Unlock()
}
