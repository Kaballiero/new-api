package controller

import (
	"github.com/QuantumNous/new-api/internal/apikeys/delete_keys_batch"

	"github.com/QuantumNous/new-api/internal/apikeys/reveal_key"
	"github.com/QuantumNous/new-api/internal/app"

	"github.com/gin-gonic/gin"
)

func NewAPIKeysHTTP() app.APIKeysHTTP {
	return app.NewAPIKeysHTTP(app.APIKeyEffects{Audit: func(c *gin.Context) map[string]any { return tokenAuditParams(c) }, BatchAudit: func(c *gin.Context, ids []int) map[string]any { return tokenBatchAuditParams(c, ids) }})
}
func GetAllTokens(c *gin.Context)       { NewAPIKeysHTTP().ListKeys.Handle(c) }
func SearchTokens(c *gin.Context)       { NewAPIKeysHTTP().SearchKeys.Handle(c) }
func GetToken(c *gin.Context)           { NewAPIKeysHTTP().GetKey.Handle(c) }
func GetTokenKey(c *gin.Context)        { NewAPIKeysHTTP().RevealKey.Handle(c) }
func DeleteToken(c *gin.Context)        { NewAPIKeysHTTP().DeleteKey.Handle(c) }
func GetTokenStatus(c *gin.Context)     { NewAPIKeysHTTP().GetKeyStatus.Handle(c) }
func GetTokenUsage(c *gin.Context)      { NewAPIKeysHTTP().GetKeyUsage.Handle(c) }
func AddToken(c *gin.Context)           { NewAPIKeysHTTP().CreateKey.Handle(c) }
func UpdateToken(c *gin.Context)        { NewAPIKeysHTTP().UpdateKey.Handle(c) }
func DeleteTokenBatch(c *gin.Context)   { NewAPIKeysHTTP().DeleteKeysBatch.Handle(c) }
func GetTokenKeysBatch(c *gin.Context)  { NewAPIKeysHTTP().RevealKeysBatch.Handle(c) }
func GetTokenAutoGroups(c *gin.Context) { NewAPIKeysHTTP().GetKeyGroups.Handle(c) }

type TokenKeyResponse = reveal_key.TokenKeyResponse
type TokenBatch = delete_keys_batch.Request
