package controller

import (
	"github.com/QuantumNous/new-api/internal/apikeys/delete_keys_batch"
	"github.com/QuantumNous/new-api/internal/apikeys/reveal_key"
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetAllTokens(c *gin.Context) { httpapi.APIKeys().ListKeys.Handle(c) }

func SearchTokens(c *gin.Context) { httpapi.APIKeys().SearchKeys.Handle(c) }

func GetToken(c *gin.Context) { httpapi.APIKeys().GetKey.Handle(c) }

func GetTokenKey(c *gin.Context) { httpapi.APIKeys().RevealKey.Handle(c) }

func DeleteToken(c *gin.Context) { httpapi.APIKeys().DeleteKey.Handle(c) }

func GetTokenUsage(c *gin.Context) { httpapi.APIKeys().GetKeyUsage.Handle(c) }

func AddToken(c *gin.Context) { httpapi.APIKeys().CreateKey.Handle(c) }

func UpdateToken(c *gin.Context) { httpapi.APIKeys().UpdateKey.Handle(c) }

func DeleteTokenBatch(c *gin.Context) { httpapi.APIKeys().DeleteKeysBatch.Handle(c) }

func GetTokenKeysBatch(c *gin.Context) { httpapi.APIKeys().RevealKeysBatch.Handle(c) }

func GetTokenAutoGroups(c *gin.Context) { httpapi.APIKeys().GetKeyGroups.Handle(c) }

type TokenKeyResponse = reveal_key.TokenKeyResponse

type TokenBatch = delete_keys_batch.Request
