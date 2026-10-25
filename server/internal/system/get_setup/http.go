package get_setup

import "github.com/gin-gonic/gin"

type Setup struct {
	Status       bool   `json:"status"`
	RootInit     bool   `json:"root_init"`
	DatabaseType string `json:"database_type"`
}

type Handler struct {
	setup, root func() bool
	database    func() string
}

func NewHandler(setup, root func() bool, database func() string) Handler {
	return Handler{setup: setup, root: root, database: database}
}
func (h Handler) Handle(c *gin.Context) {
	setup := Setup{
		Status: h.setup(),
	}
	if h.setup() {
		c.JSON(200, gin.H{
			"success": true,
			"data":    setup,
		})
		return
	}
	setup.RootInit = h.root()
	setup.DatabaseType = h.database()
	c.JSON(200, gin.H{
		"success": true,
		"data":    setup,
	})
}
