package domain

type Vendor struct {
	ModelCount  int64  `json:"model_count"`
	Version     string `json:"version,omitempty"`
	Id          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Status      int    `json:"status"`
	CreatedTime int64  `json:"created_time"`
	UpdatedTime int64  `json:"updated_time"`
}
