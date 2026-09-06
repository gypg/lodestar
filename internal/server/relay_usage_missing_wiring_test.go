package server

/*
WO-040 ①：usage-missing 守卫的生产接线测试。

守卫本体在 relay.SetInternalResponse（relay 包单测覆盖其判定），但单测对
"生产路由真的把响应送进了守卫"完全失明——本文件用 getProductionEngine
（RegisterAll 经 sync.Once 挂出的生产路由链，含 handlers/relay.go init() 注册的
/v1/chat/completions）发起一次真实请求：上游返回 200 + 完整内容但无 usage 对象，
断言 relay.UsageMissingObserver 被触发（守卫可达）且客户余额未被扣（免单决策）。

变异：删掉 relay.go 里任一 metrics.SetInternalResponse 调用 → observer 不触发 → 红。
*/

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	ch "github.com/gypg/lodestar/internal/op/channel"
	grp "github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/relay"
)

func TestChatRouteDeliversContentWithoutUsageAndSkipsCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := getProductionEngine(t)
	if productionEngineErr != nil {
		t.Fatalf("production engine: %v", productionEngineErr)
	}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := op.UserInit(); err != nil {
		t.Fatalf("user init: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.SetString(dbmodel.SettingKeyCommercialMode, "true"); err != nil {
		t.Fatalf("enable commercial mode: %v", err)
	}

	customer := dbmodel.User{Username: "wo040-wiring-" + t.Name(), Password: "x", Role: dbmodel.UserRoleUser, Quota: 10}
	if err := db.GetDB().Create(&customer).Error; err != nil {
		t.Fatalf("create customer: %v", err)
	}

	const apiKeyValue = "sk-lodestar-wo040-wiring-test-key"
	apiKey := dbmodel.APIKey{
		UserID: customer.ID,
		Name:   "wo040-wiring-key",
		APIKey: apiKeyValue,
	}
	if err := db.GetDB().Create(&apiKey).Error; err != nil {
		t.Fatalf("create api key: %v", err)
	}
	// GetByKey 走 keyIDMap 缓存，建行后必须重建缓存才能被鉴权看见。
	if err := op.InitCache(); err != nil {
		t.Fatalf("rebuild caches: %v", err)
	}

	const requestModel = "wo040-wiring-model"
	upstreamBody := `{"id":"wo040","choices":[{"index":0,"message":{"role":"assistant","content":"a full answer, no usage object"}}]}`

	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(upstreamBody))
	}))
	t.Cleanup(upstream.Close)

	channelID, groupID, itemID := 944001, 944002, 944003
	ch.GetCache().Clear()
	ch.GetCache().Set(channelID, dbmodel.Channel{
		ID:       channelID,
		Name:     "wo040-wiring-upstream",
		Type:     0, // OutboundTypeOpenAIChat
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
		Keys: []dbmodel.ChannelKey{
			{ID: 944011, ChannelID: channelID, Enabled: true, ChannelKey: "sk-wo040-upstream"},
		},
	})

	grp.GetCache().Clear()
	grp.GetCache().Set(groupID, dbmodel.Group{
		ID:           groupID,
		Name:         requestModel,
		EndpointType: dbmodel.EndpointTypeChat,
		Mode:         dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{
			{ID: itemID, GroupID: groupID, ChannelID: channelID, ModelName: requestModel, Priority: 1, Weight: 1},
		},
	})
	grp.RebuildIndexes()
	t.Cleanup(func() {
		ch.GetCache().Clear()
		grp.GetCache().Clear()
		grp.RebuildIndexes()
	})

	observed := 0
	var observedKeyID int
	prevObserver := relay.UsageMissingObserver
	relay.UsageMissingObserver = func(apiKeyID int, requestModel string) {
		observed++
		observedKeyID = apiKeyID
	}
	t.Cleanup(func() { relay.UsageMissingObserver = prevObserver })

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"`+requestModel+`","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKeyValue)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if hits == 0 {
		t.Fatal("upstream never hit — route wiring broken")
	}
	if !strings.Contains(rec.Body.String(), "a full answer") {
		t.Fatalf("client did not receive the delivered content; body=%s", rec.Body.String())
	}
	if observed != 1 {
		t.Fatalf("UsageMissingObserver fired %d times, want 1 — the guard is not reachable from the production route", observed)
	}
	if observedKeyID != int(apiKey.ID) {
		t.Fatalf("observer key id = %d, want %d", observedKeyID, int(apiKey.ID))
	}
	var balance float64
	if err := db.GetDB().Model(&dbmodel.User{}).Where("id = ?", customer.ID).Select("quota").Scan(&balance).Error; err != nil {
		t.Fatalf("read balance: %v", err)
	}
	if balance < 9.999999 || balance > 10.000001 {
		t.Fatalf("balance = %.9f, want ~10.0 — usage-missing delivery must be unbilled", balance)
	}
}
