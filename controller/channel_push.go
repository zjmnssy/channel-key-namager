package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
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

func GetChannelPushRuns(c *gin.Context) {
	var filter model.ChannelPushRunFilter
	var supplierId, siteId int64
	var err error
	for _, field := range []struct {
		name string
		dest *int64
	}{{"start_from", &filter.StartFrom}, {"start_to", &filter.StartTo}, {"supplier_user_id", &supplierId}, {"site_id", &siteId}} {
		raw := c.Query(field.name)
		if raw == "" {
			continue
		}
		if *field.dest, err = strconv.ParseInt(raw, 10, 64); err != nil {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
	}
	filter.SupplierUserId, filter.SiteId = int(supplierId), int(siteId)
	pageInfo := common.GetPageQuery(c)
	runs, total, err := model.ListChannelPushRuns(filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(runs)
	common.ApiSuccess(c, pageInfo)
}

func GetChannelPushRunDetail(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	kind := c.Param("kind")
	if err != nil || id <= 0 || (kind != "site" && kind != "target" && kind != "actions") {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	detail, err := model.GetChannelPushRunDetail(id, kind)
	if err != nil {
		respondChannelPushError(c, err)
		return
	}
	if detail == "" {
		common.ApiSuccess(c, nil)
		return
	}
	common.ApiSuccess(c, json.RawMessage(detail))
}

// GetChannelPushSiteGroups reads the site's groups for the task dialog. The
// user is waiting, so it retries at most twice whatever the environment says,
// and waits out a rate limit for at most 5 seconds.
func GetChannelPushSiteGroups(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid ID")
		return
	}
	site, err := model.GetChannelPushSite(id)
	if err != nil {
		respondChannelPushError(c, err)
		return
	}
	options := service.ChannelPushDefaultOptions
	options.MaxRetries = 2
	options.MaxRetryAfter = 5 * time.Second
	groups, err := service.NewChannelPushClient(site, options).ListGroups(c.Request.Context())
	if err != nil {
		detail := err.Error()
		var remoteErr *service.ChannelPushRemoteError
		if errors.As(err, &remoteErr) {
			detail = remoteErr.Detail
		}
		common.ApiErrorT(c, "Failed to read the site's groups: {{detail}}", map[string]any{"detail": detail})
		return
	}
	common.ApiSuccess(c, groups)
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
	if errors.Is(err, model.ErrChannelPushRunNotFound) {
		common.ApiErrorT(c, "Run does not exist")
		return
	}
	if errors.Is(err, model.ErrThirdPartySiteNotFound) {
		common.ApiErrorT(c, "Site does not exist")
		return
	}
	common.ApiError(c, err)
}
