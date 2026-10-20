package get_permission_catalog

type Usecase struct {
	resources func() []ResourceDefinition
	roles     func() []RoleDescriptor
}

func NewUsecase(resources func() []ResourceDefinition, roles func() []RoleDescriptor) Usecase {
	return Usecase{resources: resources, roles: roles}
}
func (usecase Usecase) Execute() Output {
	return Output{Resources: usecase.resources(), Roles: usecase.roles()}
}
