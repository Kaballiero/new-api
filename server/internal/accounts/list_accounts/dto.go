package list_accounts

type PageInput struct {
	Page     int
	PageSize int
}

type SortInput struct {
	By    string
	Order string
}
