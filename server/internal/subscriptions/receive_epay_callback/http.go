package receive_epay_callback

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	usecase    Usecase
	returnPath func(string) string
}

func NewHTTP(u Usecase, returnPath func(string) string) HTTP { return HTTP{u, returnPath} }
func (h HTTP) Notify(c *gin.Context) {
	params, err := callbackParams(c)
	if err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	paid, err := h.usecase.Execute(params)
	if err != nil || !paid {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	_, _ = c.Writer.Write([]byte("success"))
}
func (h HTTP) Return(c *gin.Context) {
	params, err := callbackParams(c)
	if err != nil {
		c.Redirect(http.StatusFound, h.returnPath("/wallet?pay=fail"))
		return
	}
	paid, err := h.usecase.Execute(params)
	if err != nil {
		c.Redirect(http.StatusFound, h.returnPath("/wallet?pay=fail"))
		return
	}
	if paid {
		c.Redirect(http.StatusFound, h.returnPath("/wallet?pay=success"))
		return
	}
	c.Redirect(http.StatusFound, h.returnPath("/wallet?pay=pending"))
}
func callbackParams(c *gin.Context) (map[string]string, error) {
	values := c.Request.URL.Query()
	if c.Request.Method == "POST" {
		if err := c.Request.ParseForm(); err != nil {
			return nil, err
		}
		values = c.Request.PostForm
	}
	params := make(map[string]string, len(values))
	for key := range values {
		params[key] = values.Get(key)
	}
	return params, nil
}
