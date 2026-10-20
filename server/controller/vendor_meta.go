package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

func GetAllVendors(c *gin.Context)          { app.NewGetAllVendorsHTTP().Handle(c) }
func SearchVendors(c *gin.Context)          { app.NewSearchVendorsHTTP().Handle(c) }
func GetVendorMeta(c *gin.Context)          { app.NewGetVendorMetaHTTP().Handle(c) }
func CreateVendorMeta(c *gin.Context)       { app.NewCreateVendorMetaHTTP(catalogEffects(c)).Handle(c) }
func UpdateVendorMeta(c *gin.Context)       { app.NewUpdateVendorMetaHTTP(catalogEffects(c)).Handle(c) }
func DeleteVendorMeta(c *gin.Context)       { app.NewDeleteVendorMetaHTTP(catalogEffects(c)).Handle(c) }
func PreviewVendorOperation(c *gin.Context) { app.NewPreviewVendorOperationHTTP().Handle(c) }
func ApplyVendorOperation(c *gin.Context) {
	app.NewApplyVendorOperationHTTP(catalogEffects(c)).Handle(c)
}
