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
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

// channelSupplierRequest runs handler as actor signed in with a dashboard
// session, the only way a channel supplier reaches the channel routes.
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
	c.Set("session_id", "dashboard-session")
	c.Set("auth_version", int64(1))
	c.Set("session_version", int64(1))
	handler(c)
	return response
}

// storedChannelState is a channel row, key included, and its abilities as the
// database holds them.
type storedChannelState struct {
	Channel   *model.Channel
	Abilities []model.Ability
}

func channelState(t *testing.T, id int) storedChannelState {
	t.Helper()
	channel, err := model.GetChannelById(id, true)
	require.NoError(t, err)
	var abilities []model.Ability
	require.NoError(t, model.DB.Where("channel_id = ?", id).Order("model").Find(&abilities).Error)
	return storedChannelState{Channel: channel, Abilities: abilities}
}

// createdChannelID returns the channel ID a successful create or copy
// response reports, and deletes that channel when the test ends.
func createdChannelID(t *testing.T, response *httptest.ResponseRecorder) int {
	t.Helper()
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Id int `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success, response.Body.String())
	t.Cleanup(func() { deleteChannelWithAbilities(t, result.Data.Id) })
	return result.Data.Id
}

func countChannels(t *testing.T) int64 {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Count(&count).Error)
	return count
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

// missingChannelID names no channel in the supplier tests.
const missingChannelID = 999999

func TestSupplierCannotReachOtherChannelsByID(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	// A handler that reached otherB fails at once on this address instead of
	// calling a real upstream.
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", fixture.otherB.Id).Update("base_url", "http://127.0.0.1:1").Error)
	otherBBefore, channelsBefore := channelState(t, fixture.otherB.Id), countChannels(t)

	// {id} stands for the channel ID, which is also the id path parameter.
	for _, test := range []struct {
		name, method, target, body string
		handler                    gin.HandlerFunc
		memoryCache                bool
	}{
		{name: "get", method: http.MethodGet, target: "/api/channel/{id}", handler: GetChannel},
		{name: "plugin credentials", method: http.MethodGet, target: "/api/channel/{id}/plugin_credentials", handler: GetPluginCredentials},
		{name: "vllm status", method: http.MethodGet, target: "/api/channel/{id}/vllm/status", handler: GetVLLMChannelStatus},
		{name: "sglang status", method: http.MethodGet, target: "/api/channel/{id}/sglang/status", handler: GetSGLangChannelStatus},
		{name: "codex usage", method: http.MethodGet, target: "/api/channel/{id}/codex/usage", handler: GetCodexChannelUsage},
		{name: "codex reset credits", method: http.MethodGet, target: "/api/channel/{id}/codex/usage/reset-credits", handler: GetCodexChannelRateLimitResetCredits},
		{name: "codex usage reset", method: http.MethodPost, target: "/api/channel/{id}/codex/usage/reset", handler: ResetCodexChannelUsage},
		{name: "ollama version", method: http.MethodGet, target: "/api/channel/ollama/version/{id}", handler: OllamaVersion},
		{name: "test", method: http.MethodGet, target: "/api/channel/test/{id}", handler: TestChannel},
		{name: "test from memory cache", method: http.MethodGet, target: "/api/channel/test/{id}", handler: TestChannel, memoryCache: true},
		{name: "update balance", method: http.MethodGet, target: "/api/channel/update_balance/{id}", handler: UpdateChannelBalance},
		{name: "update balance from memory cache", method: http.MethodGet, target: "/api/channel/update_balance/{id}", handler: UpdateChannelBalance, memoryCache: true},
		{name: "fetch upstream models", method: http.MethodGet, target: "/api/channel/fetch_models/{id}", handler: FetchUpstreamModels},
		{name: "start plugin login", method: http.MethodPost, target: "/api/channel/{id}/plugin_login", body: `{}`, handler: StartPluginLogin},
		{name: "continue plugin login", method: http.MethodPost, target: "/api/channel/{id}/plugin_login/continue", body: `{"flow":"unknown"}`, handler: ContinuePluginLogin},
		{name: "refresh codex credential", method: http.MethodPost, target: "/api/channel/{id}/codex/refresh", handler: RefreshCodexChannelCredential},
		{name: "manage multi keys", method: http.MethodPost, target: "/api/channel/multi_key/manage", body: `{"channel_id":{id},"action":"get_key_status"}`, handler: ManageMultiKeys},
		{name: "ollama pull", method: http.MethodPost, target: "/api/channel/ollama/pull", body: `{"channel_id":{id},"model_name":"m"}`, handler: OllamaPullModel},
		{name: "ollama pull stream", method: http.MethodPost, target: "/api/channel/ollama/pull/stream", body: `{"channel_id":{id},"model_name":"m"}`, handler: OllamaPullModelStream},
		{name: "ollama delete", method: http.MethodDelete, target: "/api/channel/ollama/delete", body: `{"channel_id":{id},"model_name":"m"}`, handler: OllamaDeleteModel},
		{name: "detect upstream updates", method: http.MethodPost, target: "/api/channel/upstream_updates/detect", body: `{"id":{id}}`, handler: DetectChannelUpstreamModelUpdates},
		{name: "apply upstream updates", method: http.MethodPost, target: "/api/channel/upstream_updates/apply", body: `{"id":{id},"add_models":["m-b1"]}`, handler: ApplyChannelUpstreamModelUpdates},
		{name: "fetch models of a saved channel", method: http.MethodPost, target: "/api/channel/fetch_models", body: `{"channel_id":{id}}`, handler: FetchModels},
		{name: "copy", method: http.MethodPost, target: "/api/channel/copy/{id}", handler: CopyChannel},
		{name: "update", method: http.MethodPut, target: "/api/channel/", body: `{"id":{id},"name":"taken-over"}`, handler: UpdateChannel},
		{name: "update status", method: http.MethodPost, target: "/api/channel/{id}/status", body: `{"status":2}`, handler: UpdateChannelStatus},
		{name: "delete", method: http.MethodDelete, target: "/api/channel/{id}", handler: DeleteChannel},
	} {
		t.Run(test.name, func(t *testing.T) {
			common.MemoryCacheEnabled = test.memoryCache
			if test.memoryCache {
				model.InitChannelCache()
			}
			var responses [2]*httptest.ResponseRecorder
			for i, id := range []int{fixture.otherB.Id, missingChannelID} {
				idText := strconv.Itoa(id)
				responses[i] = channelSupplierRequest(t, test.method, strings.ReplaceAll(test.target, "{id}", idText), strings.ReplaceAll(test.body, "{id}", idText),
					fixture.supplierA, gin.Params{{Key: "id", Value: idText}}, test.handler)
			}
			foreign, missing := responses[0], responses[1]
			assert.Equal(t, missing.Code, foreign.Code)
			assert.JSONEq(t, missing.Body.String(), foreign.Body.String())
			assert.Contains(t, foreign.Body.String(), "Channel does not exist")
		})
	}
	assert.Equal(t, otherBBefore, channelState(t, fixture.otherB.Id))
	assert.Equal(t, channelsBefore, countChannels(t))

	params := gin.Params{{Key: "id", Value: strconv.Itoa(fixture.otherB.Id)}}
	response := channelSupplierRequest(t, http.MethodGet, "/api/channel/"+params[0].Value, "", fixture.admin, params, GetChannel)
	var adminView struct {
		Success bool            `json:"success"`
		Data    channelListItem `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &adminView))
	assert.True(t, adminView.Success, response.Body.String())
	assert.Equal(t, fixture.otherB.Id, adminView.Data.Id)
}

func TestSupplierCreateIgnoresNamedChannelID(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", fixture.otherB.Id).Update("setting", `{"proxy":"","system_prompt":"kept"}`).Error)
	otherBBefore := channelState(t, fixture.otherB.Id)
	otherB := strconv.Itoa(fixture.otherB.Id)

	// AddChannel reads the submitted setting before validating it; reading an
	// invalid one must not clear the setting of the channel the supplier named.
	channelSupplierRequest(t, http.MethodPost, "/api/channel", `{"mode":"single","channel":{"id":`+otherB+`,"type":1,"setting":"{"}}`, fixture.supplierA, nil, AddChannel)
	// The new channel gets an ID of its own, so the insert does not tell
	// whether otherB exists.
	response := channelSupplierRequest(t, http.MethodPost, "/api/channel",
		`{"mode":"single","channel":{"id":`+otherB+`,"type":1,"name":"named-id","key":"sk-named-id","models":"m","group":"default"}}`,
		fixture.supplierA, nil, AddChannel)
	assert.Equal(t, otherBBefore, channelState(t, fixture.otherB.Id))
	assert.NotEqual(t, fixture.otherB.Id, createdChannelID(t, response))
}

func TestSupplierBatchRejectsForeignOrMissingIDs(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	ownA1, otherB := strconv.Itoa(fixture.ownA1.Id), strconv.Itoa(fixture.otherB.Id)
	ownA1Before, otherBBefore := channelState(t, fixture.ownA1.Id), channelState(t, fixture.otherB.Id)

	// {ids} stands for the channel IDs of the request.
	for _, test := range []struct {
		name, target, body string
		handler            gin.HandlerFunc
	}{
		{name: "set tag", target: "/api/channel/batch/tag", body: `{"ids":[{ids}],"tag":"taken"}`, handler: BatchSetChannelTag},
		{name: "update status", target: "/api/channel/status/batch", body: `{"ids":[{ids}],"status":2}`, handler: BatchUpdateChannelStatus},
		{name: "delete", target: "/api/channel/batch", body: `{"ids":[{ids}]}`, handler: DeleteChannelBatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			foreign := channelSupplierRequest(t, http.MethodPost, test.target, strings.ReplaceAll(test.body, "{ids}", ownA1+","+otherB), fixture.supplierA, nil, test.handler)
			missing := channelSupplierRequest(t, http.MethodPost, test.target, strings.ReplaceAll(test.body, "{ids}", ownA1+","+strconv.Itoa(missingChannelID)), fixture.supplierA, nil, test.handler)
			assert.Equal(t, missing.Code, foreign.Code)
			assert.JSONEq(t, missing.Body.String(), foreign.Body.String())
			assert.Contains(t, foreign.Body.String(), "One or more channels do not exist")
		})
	}
	assert.Equal(t, ownA1Before, channelState(t, fixture.ownA1.Id))
	assert.Equal(t, otherBBefore, channelState(t, fixture.otherB.Id))

	// A repeated ID names one of the supplier's channels, not a missing one.
	response := channelSupplierRequest(t, http.MethodPost, "/api/channel/status/batch", `{"ids":[`+ownA1+`,`+ownA1+`],"status":2}`, fixture.supplierA, nil, BatchUpdateChannelStatus)
	assert.Contains(t, response.Body.String(), `"success":true`)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, channelState(t, fixture.ownA1.Id).Channel.Status)

	response = channelSupplierRequest(t, http.MethodPost, "/api/channel/batch", `{"ids":[`+ownA1+`,`+otherB+`]}`, fixture.admin, nil, DeleteChannelBatch)
	assert.JSONEq(t, `{"success":true,"message":"","data":2}`, response.Body.String())
	for _, id := range []int{fixture.ownA1.Id, fixture.otherB.Id} {
		_, err := model.GetChannelById(id, false)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	}
}

func TestSupplierTagOperationsStayInScope(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	// otherB and legacy share the tag with ownA1; no tag operation of
	// supplierA may change them or their abilities.
	otherBBefore, legacyBefore := channelState(t, fixture.otherB.Id), channelState(t, fixture.legacy.Id)
	abilityEnabled := func(ability model.Ability, _ int) bool { return ability.Enabled }

	response := channelSupplierRequest(t, http.MethodPost, "/api/channel/tag/disabled", `{"tag":"shared"}`, fixture.supplierA, nil, DisableTagChannels)
	require.Contains(t, response.Body.String(), `"success":true`)
	ownA1 := channelState(t, fixture.ownA1.Id)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, ownA1.Channel.Status)
	assert.Equal(t, []bool{false, false}, lo.Map(ownA1.Abilities, abilityEnabled))
	assert.Equal(t, otherBBefore, channelState(t, fixture.otherB.Id))
	assert.Equal(t, legacyBefore, channelState(t, fixture.legacy.Id))

	// An edit that keeps the models updates the abilities in place.
	response = channelSupplierRequest(t, http.MethodPut, "/api/channel/tag", `{"tag":"shared","priority":9}`, fixture.supplierA, nil, EditTagChannels)
	require.Contains(t, response.Body.String(), `"success":true`)
	ownA1 = channelState(t, fixture.ownA1.Id)
	assert.Equal(t, int64(9), ownA1.Channel.GetPriority())
	assert.Equal(t, []int64{9, 9}, lo.Map(ownA1.Abilities, func(ability model.Ability, _ int) int64 { return lo.FromPtr(ability.Priority) }))
	assert.Equal(t, otherBBefore, channelState(t, fixture.otherB.Id))
	assert.Equal(t, legacyBefore, channelState(t, fixture.legacy.Id))

	// An edit of the models rebuilds the abilities.
	response = channelSupplierRequest(t, http.MethodPut, "/api/channel/tag", `{"tag":"shared","models":"m-new"}`, fixture.supplierA, nil, EditTagChannels)
	require.Contains(t, response.Body.String(), `"success":true`)
	ownA1 = channelState(t, fixture.ownA1.Id)
	assert.Equal(t, []string{"m-new"}, lo.Map(ownA1.Abilities, func(ability model.Ability, _ int) string { return ability.Model }))
	assert.Equal(t, otherBBefore, channelState(t, fixture.otherB.Id))
	assert.Equal(t, legacyBefore, channelState(t, fixture.legacy.Id))

	// Enabling the tag leaves a disabled channel of someone else disabled.
	require.True(t, model.UpdateChannelStatus(fixture.legacy.Id, "", common.ChannelStatusManuallyDisabled, "manual operation"))
	legacyBefore = channelState(t, fixture.legacy.Id)
	response = channelSupplierRequest(t, http.MethodPost, "/api/channel/tag/enabled", `{"tag":"shared"}`, fixture.supplierA, nil, EnableTagChannels)
	require.Contains(t, response.Body.String(), `"success":true`)
	ownA1 = channelState(t, fixture.ownA1.Id)
	assert.Equal(t, common.ChannelStatusEnabled, ownA1.Channel.Status)
	assert.Equal(t, []bool{true}, lo.Map(ownA1.Abilities, abilityEnabled))
	assert.Equal(t, otherBBefore, channelState(t, fixture.otherB.Id))
	assert.Equal(t, legacyBefore, channelState(t, fixture.legacy.Id))

	// An administrator's tag operation still covers every channel under the tag.
	response = channelSupplierRequest(t, http.MethodPost, "/api/channel/tag/disabled", `{"tag":"shared"}`, fixture.admin, nil, DisableTagChannels)
	require.Contains(t, response.Body.String(), `"success":true`)
	for _, channel := range []*model.Channel{fixture.ownA1, fixture.otherB, fixture.legacy} {
		state := channelState(t, channel.Id)
		assert.Equal(t, common.ChannelStatusManuallyDisabled, state.Channel.Status, channel.Name)
		assert.NotContains(t, lo.Map(state.Abilities, abilityEnabled), true, channel.Name)
	}
}

func TestSupplierCannotChangeRequestPlugins(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	channelsBefore := countChannels(t)
	response := channelSupplierRequest(t, http.MethodPost, "/api/channel",
		`{"mode":"single","channel":{"type":1,"name":"bound","key":"sk-bound","models":"m","group":"default","setting":"{\"request_plugins\":[\"inst-1\"]}"}}`,
		fixture.supplierA, nil, AddChannel)
	assert.Contains(t, response.Body.String(), "Channel suppliers cannot change request plugins")
	assert.Equal(t, channelsBefore, countChannels(t))

	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", fixture.ownA1.Id).Update("setting", `{"request_plugins":["inst-1"]}`).Error)
	ownA1 := strconv.Itoa(fixture.ownA1.Id)
	response = channelSupplierRequest(t, http.MethodPut, "/api/channel/", `{"id":`+ownA1+`,"setting":"{\"request_plugins\":[]}"}`, fixture.supplierA, nil, UpdateChannel)
	assert.Contains(t, response.Body.String(), "Channel suppliers cannot change request plugins")
	assert.Equal(t, []string{"inst-1"}, channelState(t, fixture.ownA1.Id).Channel.GetSetting().RequestPlugins)

	params := gin.Params{{Key: "id", Value: ownA1}}
	response = channelSupplierRequest(t, http.MethodPost, "/api/channel/copy/"+ownA1, "", fixture.supplierA, params, CopyChannel)
	assert.Empty(t, channelState(t, createdChannelID(t, response)).Channel.GetSetting().RequestPlugins)

	// An edit that does not submit the setting leaves the binding alone.
	response = channelSupplierRequest(t, http.MethodPut, "/api/channel/", `{"id":`+ownA1+`,"name":"renamed"}`, fixture.supplierA, nil, UpdateChannel)
	assert.Contains(t, response.Body.String(), `"success":true`)
	assert.Equal(t, "renamed", channelState(t, fixture.ownA1.Id).Channel.Name)
}

func TestSupplierCannotPreviewTaskPluginModels(t *testing.T) {
	fixture := setupChannelSupplierTest(t)
	pluginType := strconv.Itoa(constant.ChannelTypeTaskPlugin)
	added := channelSupplierRequest(t, http.MethodPost, "/api/channel",
		`{"mode":"single","channel":{"type":`+pluginType+`,"name":"plugin","key":"sk-plugin","models":"m","group":"default","setting":"{\"task_plugin_key\":\"some-plugin\"}"}}`,
		fixture.supplierA, nil, AddChannel)
	require.Contains(t, added.Body.String(), "task_plugin.bind")

	preview := channelSupplierRequest(t, http.MethodPost, "/api/channel/fetch_models",
		`{"type":`+pluginType+`,"task_plugin_key":"some-plugin","base_url":"http://127.0.0.1:1"}`, fixture.supplierA, nil, FetchModels)
	assert.Equal(t, added.Code, preview.Code)
	assert.JSONEq(t, added.Body.String(), preview.Body.String())
}
