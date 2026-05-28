package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetAllVendors 获取供应商列表（分页）
func GetAllVendors(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	vendors, err := model.GetAllVendors(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	}
	var total int64
	model.DB.Model(&model.Vendor{}).Count(&total)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(vendors)
	common.ApiSuccess(c, pageInfo)
}

// SearchVendors 搜索供应商
func SearchVendors(c *gin.Context) {
	keyword := c.Query("keyword")
	pageInfo := common.GetPageQuery(c)
	vendors, total, err := model.SearchVendors(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(vendors)
	common.ApiSuccess(c, pageInfo)
}

// GetVendorMeta 根据 ID 获取供应商
func GetVendorMeta(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", "invalid id")
		return
	}
	v, err := model.GetVendorByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiErrorMsgStatusCode(c, http.StatusNotFound, "vendor_not_found", "vendor not found")
		return
	}
	if err != nil {
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	}
	common.ApiSuccess(c, v)
}

// CreateVendorMeta 新建供应商
func CreateVendorMeta(c *gin.Context) {
	var v model.Vendor
	if err := c.ShouldBindJSON(&v); err != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}
	if v.Name == "" {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "vendor_name_empty", "供应商名称不能为空")
		return
	}
	if dup, err := model.IsVendorNameDuplicated(0, v.Name); err != nil {
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	} else if dup {
		common.ApiErrorMsgStatusCode(c, http.StatusConflict, "vendor_name_exists", "供应商名称已存在")
		return
	}

	if err := v.Insert(); err != nil {
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	}
	common.ApiSuccessStatus(c, http.StatusCreated, &v)
}

// UpdateVendorMeta 更新供应商
func UpdateVendorMeta(c *gin.Context) {
	var v model.Vendor
	if err := c.ShouldBindJSON(&v); err != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}
	if v.Id == 0 {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "vendor_id_missing", "缺少供应商 ID")
		return
	}
	if dup, err := model.IsVendorNameDuplicated(v.Id, v.Name); err != nil {
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	} else if dup {
		common.ApiErrorMsgStatusCode(c, http.StatusConflict, "vendor_name_exists", "供应商名称已存在")
		return
	}

	if err := v.Update(); err != nil {
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	}
	common.ApiSuccess(c, &v)
}

// DeleteVendorMeta 删除供应商
func DeleteVendorMeta(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", "invalid id")
		return
	}
	res := model.DB.Delete(&model.Vendor{}, id)
	if res.Error != nil {
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		common.ApiErrorMsgStatusCode(c, http.StatusNotFound, "vendor_not_found", "vendor not found")
		return
	}
	common.ApiSuccess(c, nil)
}
