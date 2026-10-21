package controller

import (
	"sync"

	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

// authorizationHTTP builds the permission catalog handler once; the catalog is
// read per request by the injected functions.
var authorizationHTTP = sync.OnceValue(app.NewAuthorizationHTTP)

func GetPermissionCatalog(c *gin.Context) { authorizationHTTP().Handle(c) }
