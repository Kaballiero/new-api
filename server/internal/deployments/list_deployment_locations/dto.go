package list_deployment_locations

type Location struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	ISO2        string  `json:"iso2,omitempty"`
	Region      string  `json:"region,omitempty"`
	Country     string  `json:"country,omitempty"`
	Latitude    float64 `json:"latitude,omitempty"`
	Longitude   float64 `json:"longitude,omitempty"`
	Available   int     `json:"available,omitempty"`
	Description string  `json:"description,omitempty"`
}

type LocationsResponse struct {
	Locations []Location `json:"locations"`
	Total     int        `json:"total"`
}
