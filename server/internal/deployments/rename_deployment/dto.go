package rename_deployment

type UpdateClusterNameRequest struct {
	Name string `json:"cluster_name"`
}

type UpdateClusterNameResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type Input struct{ APIKey, ID, Name string }
