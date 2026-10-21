package identityhttp

import (
	"github.com/gin-gonic/gin"
)

func SetAuthNoStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
}
