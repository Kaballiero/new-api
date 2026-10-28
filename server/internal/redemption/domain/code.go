package domain

const (
	StatusEnabled  = 1
	StatusDisabled = 2
	StatusUsed     = 3
)

type Code struct {
	ID, IssuerID              int
	Key                       string
	Status                    int
	Name                      string
	Quota                     int
	CreatedTime, RedeemedTime int64
	UsedUserID                int
	ExpiredTime               int64
}

type Page struct {
	Codes []Code
	Total int64
}

type Query struct {
	Prefix string
	ID     *int
	Status *int
	Expiry ExpiryFilter
	Now    int64
}

type ExpiryFilter int

const (
	AnyExpiry ExpiryFilter = iota
	ActiveExpiry
	PastExpiry
)

// PageReader counts and fetches rows in the single read transaction owned by list or search.
type PageReader interface {
	Count(query Query, total *int64) error
	Find(query Query, start int, limit int, codes *[]Code) error
}

type InvalidCodes struct {
	Statuses       []int
	ExpiringStatus int
	Before         int64
}

type Redemption struct {
	ID, UserID, FromStatus, ToStatus int
	RedeemedTime                     int64
}
