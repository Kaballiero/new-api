package domain

func CanManageTargetRole(actorRole, targetRole, rootRole int) bool {
	return actorRole == rootRole || actorRole > targetRole
}
