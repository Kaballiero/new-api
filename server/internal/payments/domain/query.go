package domain

type Query struct {
	UserID     *int
	Cutoff     int64
	Pattern    string
	Search     bool
	CountLimit int
}

type Page struct {
	Orders []Order
	Total  int64
}

// PageReader executes the selected Count/Find with caller-owned destinations, including partial panic results.
type PageReader interface {
	Count(query Query, total *int64) error
	Find(query Query, start int, limit int, orders *[]Order) error
}

// Reader preserves the legacy read transaction and rollback-only panic recovery.
type Reader interface {
	Read(run func(reader PageReader) error) error
}
