package controller

import (
	"github.com/gin-gonic/gin"
)

func GetAllVendors(c *gin.Context)          { catalogHTTP().ListVendors.Handle(c) }
func SearchVendors(c *gin.Context)          { catalogHTTP().SearchVendors.Handle(c) }
func GetVendorMeta(c *gin.Context)          { catalogHTTP().GetVendor.Handle(c) }
func CreateVendorMeta(c *gin.Context)       { catalogHTTP().CreateVendor.Handle(c) }
func UpdateVendorMeta(c *gin.Context)       { catalogHTTP().UpdateVendor.Handle(c) }
func DeleteVendorMeta(c *gin.Context)       { catalogHTTP().DeleteVendor.Handle(c) }
func PreviewVendorOperation(c *gin.Context) { catalogHTTP().PreviewVendorOperation.Handle(c) }
func ApplyVendorOperation(c *gin.Context)   { catalogHTTP().ApplyVendorOperation.Handle(c) }
