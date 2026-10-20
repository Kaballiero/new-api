package clear_user_permissions_in_tx

import "github.com/QuantumNous/new-api/internal/authorization/domain"

type Writer interface{ RemoveResource(int, string) error }
type Usecase struct {
	catalog func() domain.Catalog
	writer  Writer
}

func NewUsecase(catalog func() domain.Catalog, writer Writer) Usecase {
	return Usecase{catalog: catalog, writer: writer}
}
func (usecase Usecase) Execute(userID int) error {
	for _, resource := range usecase.catalog().Resources {
		if err := usecase.writer.RemoveResource(userID, resource.Resource); err != nil {
			return err
		}
	}
	return nil
}
