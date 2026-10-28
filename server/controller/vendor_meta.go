package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/gin-gonic/gin"
)

func GetAllVendors(c *gin.Context) { httpapi.Catalog().ListVendors.Handle(c) }

func SearchVendors(c *gin.Context) { httpapi.Catalog().SearchVendors.Handle(c) }

func GetVendorMeta(c *gin.Context) { httpapi.Catalog().GetVendor.Handle(c) }

func CreateVendorMeta(c *gin.Context) { httpapi.Catalog().CreateVendor.Handle(c) }

func UpdateVendorMeta(c *gin.Context) { httpapi.Catalog().UpdateVendor.Handle(c) }

func DeleteVendorMeta(c *gin.Context) { httpapi.Catalog().DeleteVendor.Handle(c) }

func PreviewVendorOperation(c *gin.Context) { httpapi.Catalog().PreviewVendorOperation.Handle(c) }

func ApplyVendorOperation(c *gin.Context) { httpapi.Catalog().ApplyVendorOperation.Handle(c) }
