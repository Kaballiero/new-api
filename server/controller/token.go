package controller

import (
	"sync"

	"github.com/QuantumNous/new-api/internal/apikeys/delete_keys_batch"
	"github.com/QuantumNous/new-api/internal/apikeys/reveal_key"
	"github.com/QuantumNous/new-api/internal/app"

	"github.com/gin-gonic/gin"
)

// apiKeysHTTP builds the API key handlers once; settings and the database are
// read per request by the injected functions.
var apiKeysHTTP = sync.OnceValue(func() app.APIKeysHTTP {
	return app.NewAPIKeysHTTP(app.APIKeyEffects{Audit: func(c *gin.Context) map[string]any { return tokenAuditParams(c) }, BatchAudit: func(c *gin.Context, ids []int) map[string]any { return tokenBatchAuditParams(c, ids) }})
})

func GetAllTokens(c *gin.Context)       { apiKeysHTTP().ListKeys.Handle(c) }
func SearchTokens(c *gin.Context)       { apiKeysHTTP().SearchKeys.Handle(c) }
func GetToken(c *gin.Context)           { apiKeysHTTP().GetKey.Handle(c) }
func GetTokenKey(c *gin.Context)        { apiKeysHTTP().RevealKey.Handle(c) }
func DeleteToken(c *gin.Context)        { apiKeysHTTP().DeleteKey.Handle(c) }
func GetTokenStatus(c *gin.Context)     { apiKeysHTTP().GetKeyStatus.Handle(c) }
func GetTokenUsage(c *gin.Context)      { apiKeysHTTP().GetKeyUsage.Handle(c) }
func AddToken(c *gin.Context)           { apiKeysHTTP().CreateKey.Handle(c) }
func UpdateToken(c *gin.Context)        { apiKeysHTTP().UpdateKey.Handle(c) }
func DeleteTokenBatch(c *gin.Context)   { apiKeysHTTP().DeleteKeysBatch.Handle(c) }
func GetTokenKeysBatch(c *gin.Context)  { apiKeysHTTP().RevealKeysBatch.Handle(c) }
func GetTokenAutoGroups(c *gin.Context) { apiKeysHTTP().GetKeyGroups.Handle(c) }

type TokenKeyResponse = reveal_key.TokenKeyResponse
type TokenBatch = delete_keys_batch.Request
