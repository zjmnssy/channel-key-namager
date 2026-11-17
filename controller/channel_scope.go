package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// channelScopeFromContext returns the channels the authenticated user may
// reach, from the user ID and role the auth middleware set: every channel for
// an administrator, the user's own channels for a channel supplier, and none
// for anyone else.
func channelScopeFromContext(c *gin.Context) model.ChannelScope {
	role := c.GetInt("role")
	if role >= common.RoleAdminUser {
		return model.AllChannelsScope()
	}
	if role == common.RoleChannelSupplierUser {
		return model.CreatorChannelsScope(c.GetInt("id"))
	}
	return model.CreatorChannelsScope(0)
}

// buildChannelListQuery selects the channels in scope that match the list
// filters; a negative typeFilter matches every type.
func buildChannelListQuery(scope model.ChannelScope, group string, statusFilter int, typeFilter int) *gorm.DB {
	query := scope.Apply(model.DB.Model(&model.Channel{}))
	query = model.ApplyChannelGroupFilter(query, group)
	query = applyChannelStatusFilter(query, statusFilter)
	if typeFilter >= 0 {
		query = query.Where("type = ?", typeFilter)
	}
	return query
}
