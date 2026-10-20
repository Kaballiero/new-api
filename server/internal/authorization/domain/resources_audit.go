package domain

const ResourceAudit = "audit"

func auditResource() ResourceDefinition {
	return ResourceDefinition{
		Resource: ResourceAudit,
		LabelKey: "Audit Logs",
		Actions: []ActionDefinition{{
			Action:         ActionRead,
			LabelKey:       "View other accounts' audit logs",
			DescriptionKey: "View audit records from user and admin roles. Root records are always excluded.",
		}},
	}
}
