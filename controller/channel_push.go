package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetChannelPushTasks(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	tasks, total, err := model.ListChannelPushTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tasks)
	common.ApiSuccess(c, pageInfo)
}

func CreateChannelPushTask(c *gin.Context) {
	var input model.ChannelPushTaskInput
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	task, err := model.CreateChannelPushTask(input)
	if err != nil {
		respondChannelPushError(c, err)
		return
	}
	common.ApiSuccess(c, task)
}

func UpdateChannelPushTask(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid ID")
		return
	}
	var input model.ChannelPushTaskInput
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	task, err := model.UpdateChannelPushTask(id, input)
	if err != nil {
		respondChannelPushError(c, err)
		return
	}
	common.ApiSuccess(c, task)
}

func SetChannelPushTaskEnabled(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid ID")
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := common.DecodeJson(c.Request.Body, &body); err != nil || body.Enabled == nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	task, err := model.SetChannelPushTaskEnabled(id, *body.Enabled)
	if err != nil {
		respondChannelPushError(c, err)
		return
	}
	common.ApiSuccess(c, task)
}

func GetChannelPushOptions(c *gin.Context) {
	options, err := model.GetChannelPushOptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, options)
}

// respondChannelPushError answers a validation failure with the offending
// field, a missing task or site with its own message, and any other error as
// a plain API error.
func respondChannelPushError(c *gin.Context, err error) {
	var fieldErr *model.ChannelPushFieldError
	if errors.As(err, &fieldErr) {
		common.ApiErrorStatus(c, http.StatusOK, common.NewMessage(fieldErr.Message, fieldErr.Params), gin.H{"field": fieldErr.Field})
		return
	}
	if errors.Is(err, model.ErrChannelPushTaskNotFound) {
		common.ApiErrorT(c, "Task does not exist")
		return
	}
	if errors.Is(err, model.ErrThirdPartySiteNotFound) {
		common.ApiErrorT(c, "Site does not exist")
		return
	}
	common.ApiError(c, err)
}
