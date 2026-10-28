package domain

const (
	ResourceTaskPlugin = "task_plugin"

	ActionBind = "bind"
)

func task_pluginResource() ResourceDefinition {
	return ResourceDefinition{
		Resource: ResourceTaskPlugin,
		LabelKey: "Task Plugin",
		Actions: []ActionDefinition{
			{
				Action:         ActionBind,
				LabelKey:       "Bind task plugins",
				DescriptionKey: "List registered task plugins and bind them when creating or editing task plugin channels.",
			},
		},
	}
}
