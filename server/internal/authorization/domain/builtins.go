package domain

func BuiltInResources() []ResourceDefinition {
	return []ResourceDefinition{auditResource(), channelResource(), task_pluginResource()}
}
