package search_accounts

type PageInput struct {
	Page     int
	PageSize int
}

type SortInput struct {
	By    string
	Order string
}

type Input struct {
	Keyword string
	Group   string
	Role    *int
	Status  *int
	Page    PageInput
	Sort    SortInput
}
