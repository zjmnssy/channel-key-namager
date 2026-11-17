package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelSupplierFixture struct {
	root, admin, supplierA, supplierB        *model.User
	legacy, ownA1, ownA2, otherB, adminOwned *model.Channel
}

// setupChannelSupplierTest builds the users and channels shared by the channel
// supplier tests, with the authorization baseline seeded.
func setupChannelSupplierTest(t *testing.T) channelSupplierFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousCache, previousRedis, previousSQLite := common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled, common.SQLitePath
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousType, previousLogType)
		common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled, common.SQLitePath = previousMaster, previousCache, previousRedis, previousSQLite
	})
	t.Setenv("SQL_DSN", os.Getenv("TEST_CHANNEL_SQL_DSN"))
	t.Setenv("LOG_SQL_DSN", "")
	common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled = false, false, false
	common.SQLitePath = filepath.Join(t.TempDir(), "channel.db")
	require.NoError(t, model.InitDB())
	database := model.DB
	sqlDB, err := database.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	model.LOG_DB = database
	common.SetLogDatabaseType(common.MainDatabaseType())
	require.NoError(t, database.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.User{}, &model.Log{}, &model.AuditLog{}, &model.CasbinRule{}, &model.AuthzRole{}))

	// Seeding the built-in roles and default policies needs the master node.
	common.IsMasterNode = true
	require.NoError(t, authz.Init(database))
	common.IsMasterNode = false

	newUser := func(username string, role int) *model.User {
		user := &model.User{Username: username, Role: role, Status: common.UserStatusEnabled, AffCode: username}
		require.NoError(t, database.Create(user).Error)
		t.Cleanup(func() { require.NoError(t, database.Unscoped().Delete(user).Error) })
		return user
	}
	fixture := channelSupplierFixture{
		root:      newUser("fixture-root", common.RoleRootUser),
		admin:     newUser("fixture-admin", common.RoleAdminUser),
		supplierA: newUser("supplier-a", common.RoleChannelSupplierUser),
		supplierB: newUser("supplier-b", common.RoleChannelSupplierUser),
	}

	newChannel := func(name, models, tag string, priority *int64, creator *model.User) *model.Channel {
		channel := &model.Channel{
			Type: 1, Key: "sk-" + name, Name: name, Status: common.ChannelStatusEnabled,
			Models: models, Group: "default", Priority: priority,
		}
		if tag != "" {
			channel.Tag = &tag
		}
		if creator != nil {
			channel.CreatorUserId, channel.CreatorRole = creator.Id, creator.Role
		}
		require.NoError(t, channel.Insert())
		t.Cleanup(func() { deleteChannelWithAbilities(t, channel.Id) })
		return channel
	}
	priority100, priority0 := int64(100), int64(0)
	fixture.legacy = newChannel("legacy", "m-legacy", "shared", nil, nil)
	fixture.ownA1 = newChannel("own-a1", "m-a1,m-shared", "shared", &priority100, fixture.supplierA)
	fixture.ownA2 = newChannel("own-a2", "m-a2", "", nil, fixture.supplierA)
	fixture.otherB = newChannel("other-b", "m-b1,m-b2,m-b3", "shared", nil, fixture.supplierB)
	fixture.adminOwned = newChannel("admin-owned", "m-admin,m-shared", "", &priority0, fixture.admin)
	return fixture
}

func deleteChannelWithAbilities(t *testing.T, id int) {
	t.Helper()
	require.NoError(t, model.DB.Unscoped().Where("channel_id = ?", id).Delete(&model.Ability{}).Error)
	require.NoError(t, model.DB.Unscoped().Delete(&model.Channel{}, id).Error)
}

func channelSupplierRequest(t *testing.T, method, target, body string, actor *model.User, params gin.Params, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = params
	c.Set("id", actor.Id)
	c.Set("role", actor.Role)
	c.Set("username", actor.Username)
	handler(c)
	return response
}

// channelSupplierData sends a GET request as actor and returns the data of the
// successful response.
func channelSupplierData[T any](t *testing.T, target string, actor *model.User, handler gin.HandlerFunc) T {
	t.Helper()
	response := channelSupplierRequest(t, http.MethodGet, target, "", actor, nil, handler)
	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Success bool `json:"success"`
		Data    T    `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success, response.Body.String())
	return result.Data
}

type channelListItem struct {
	Id              int    `json:"id"`
	CreatorUserId   int    `json:"creator_user_id"`
	CreatorUsername string `json:"creator_username"`
}

type channelListData struct {
	Items      []channelListItem `json:"items"`
	Total      int               `json:"total"`
	TypeCounts map[string]int    `json:"type_counts"`
}

func (data channelListData) ids() []int {
	return lo.Map(data.Items, func(item channelListItem, _ int) int { return item.Id })
}

func TestChannelCreatorIsSetByServer(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	const body = `{"mode":"batch","channel":{"type":1,"name":"batch","key":"k1\nk2","models":"m","group":"default","creator_user_id":999,"creator_role":100}}`

	for _, test := range []struct {
		actor        *model.User
		wantSupplier bool
	}{{fixture.supplierA, true}, {fixture.admin, false}} {
		response := channelSupplierRequest(t, http.MethodPost, "/api/channel", body, test.actor, nil, AddChannel)
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Ids []int `json:"ids"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		require.True(t, result.Success, response.Body.String())
		require.Len(t, result.Data.Ids, 2)
		for _, id := range result.Data.Ids {
			t.Cleanup(func() { deleteChannelWithAbilities(t, id) })
			stored, err := model.GetChannelById(id, false)
			require.NoError(t, err)
			assert.Equal(t, test.actor.Id, stored.CreatorUserId)
			assert.Equal(t, test.actor.Role, stored.CreatorRole)
			assert.Equal(t, test.wantSupplier, stored.IsSupplierChannel())
		}
	}
}

func TestChannelCreatorIsReadOnly(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	body := `{"id":` + strconv.Itoa(fixture.ownA1.Id) + `,"name":"renamed","creator_user_id":` + strconv.Itoa(fixture.admin.Id) + `,"creator_role":10}`

	response := channelSupplierRequest(t, http.MethodPut, "/api/channel/", body, fixture.admin, nil, UpdateChannel)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"success":true`)

	stored, err := model.GetChannelById(fixture.ownA1.Id, false)
	require.NoError(t, err)
	assert.Equal(t, "renamed", stored.Name)
	assert.Equal(t, fixture.supplierA.Id, stored.CreatorUserId)
	assert.Equal(t, common.RoleChannelSupplierUser, stored.CreatorRole)
}

func TestCopiedChannelBelongsToCopier(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	params := gin.Params{{Key: "id", Value: strconv.Itoa(fixture.ownA1.Id)}}

	for _, test := range []struct {
		actor *model.User
		role  int
	}{{fixture.admin, common.RoleAdminUser}, {fixture.supplierA, common.RoleChannelSupplierUser}} {
		response := channelSupplierRequest(t, http.MethodPost, "/api/channel/copy/"+params[0].Value, "", test.actor, params, CopyChannel)
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Id int `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		require.True(t, result.Success, response.Body.String())
		t.Cleanup(func() { deleteChannelWithAbilities(t, result.Data.Id) })
		stored, err := model.GetChannelById(result.Data.Id, false)
		require.NoError(t, err)
		assert.Equal(t, test.actor.Id, stored.CreatorUserId)
		assert.Equal(t, test.role, stored.CreatorRole)
	}
}

func TestSupplierListSeesOnlyOwnChannels(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	// supplierA created this channel while still an administrator.
	sharedTag := "shared"
	adminEra := &model.Channel{
		Type: 1, Key: "sk-a-admin-era", Name: "a-admin-era", Status: common.ChannelStatusEnabled,
		Models: "m-a-admin-era", Group: "default", Tag: &sharedTag,
		CreatorUserId: fixture.supplierA.Id, CreatorRole: common.RoleAdminUser,
	}
	require.NoError(t, adminEra.Insert())
	t.Cleanup(func() { deleteChannelWithAbilities(t, adminEra.Id) })
	const target = "/api/channel/?p=1&page_size=50"

	list := channelSupplierData[channelListData](t, target, fixture.supplierA, GetAllChannels)
	assert.ElementsMatch(t, []int{fixture.ownA1.Id, fixture.ownA2.Id}, list.ids())
	assert.Equal(t, 2, list.Total)
	assert.Equal(t, 2, lo.Sum(lo.Values(list.TypeCounts)))

	list = channelSupplierData[channelListData](t, target+"&tag_mode=true", fixture.supplierA, GetAllChannels)
	assert.ElementsMatch(t, []int{fixture.ownA1.Id}, list.ids())

	// A supplier set back to a common user sees no channel.
	demoted := &model.User{Id: fixture.supplierA.Id, Username: fixture.supplierA.Username, Role: common.RoleCommonUser}
	list = channelSupplierData[channelListData](t, target, demoted, GetAllChannels)
	assert.Empty(t, list.Items)
	assert.Zero(t, list.Total)

	list = channelSupplierData[channelListData](t, target, fixture.admin, GetAllChannels)
	assert.ElementsMatch(t, []int{fixture.legacy.Id, fixture.ownA1.Id, fixture.ownA2.Id, fixture.otherB.Id, fixture.adminOwned.Id, adminEra.Id}, list.ids())
	assert.Equal(t, 6, list.Total)
	items := lo.KeyBy(list.Items, func(item channelListItem) int { return item.Id })
	assert.Equal(t, "supplier-a", items[fixture.ownA1.Id].CreatorUsername)
	assert.Zero(t, items[fixture.legacy.Id].CreatorUserId)
	assert.Empty(t, items[fixture.legacy.Id].CreatorUsername)
}

func TestSupplierSearchSeesOnlyOwnChannels(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	const target = "/api/channel/search?keyword="

	list := channelSupplierData[channelListData](t, target, fixture.supplierA, SearchChannels)
	assert.ElementsMatch(t, []int{fixture.ownA1.Id, fixture.ownA2.Id}, list.ids())

	list = channelSupplierData[channelListData](t, target+fixture.otherB.Name, fixture.supplierA, SearchChannels)
	assert.Empty(t, list.Items)
	assert.Zero(t, list.Total)

	list = channelSupplierData[channelListData](t, target+"&tag_mode=true", fixture.supplierA, SearchChannels)
	assert.ElementsMatch(t, []int{fixture.ownA1.Id}, list.ids())

	list = channelSupplierData[channelListData](t, target, fixture.admin, SearchChannels)
	assert.ElementsMatch(t, []int{fixture.legacy.Id, fixture.ownA1.Id, fixture.ownA2.Id, fixture.otherB.Id, fixture.adminOwned.Id}, list.ids())
}

func TestSupplierTagAndEnabledModelsStayInScope(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	const tagModels, enabledModels = "/api/channel/tag/models?tag=shared", "/api/channel/models_enabled"

	assert.Equal(t, "m-a1,m-shared", channelSupplierData[string](t, tagModels, fixture.supplierA, GetTagModels))
	assert.Equal(t, "m-b1,m-b2,m-b3", channelSupplierData[string](t, tagModels, fixture.admin, GetTagModels))

	assert.ElementsMatch(t, []string{"m-a1", "m-shared", "m-a2"}, channelSupplierData[[]string](t, enabledModels, fixture.supplierA, EnabledListModels))
	assert.Subset(t, channelSupplierData[[]string](t, enabledModels, fixture.admin, EnabledListModels), []string{"m-legacy", "m-b1", "m-admin"})
}

func TestChannelCreatorNameForDeletedUser(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	require.NoError(t, model.DB.Delete(fixture.supplierB).Error)

	list := channelSupplierData[channelListData](t, "/api/channel/?p=1&page_size=50", fixture.admin, GetAllChannels)
	items := lo.KeyBy(list.Items, func(item channelListItem) int { return item.Id })
	require.Contains(t, items, fixture.otherB.Id)
	assert.Equal(t, fixture.supplierB.Id, items[fixture.otherB.Id].CreatorUserId)
	assert.Empty(t, items[fixture.otherB.Id].CreatorUsername)
	// A creator who still exists keeps a name in the same list.
	assert.Equal(t, "supplier-a", items[fixture.ownA1.Id].CreatorUsername)
}
