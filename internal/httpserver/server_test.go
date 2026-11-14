package httpserver

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"gorm.io/gorm"

	"github.com/linuxsuren/message-hub/internal/auth"
	"github.com/linuxsuren/message-hub/internal/store"
)

// captureMailer 捕获验证码供测试断言。
type captureMailer struct {
	mu    sync.Mutex
	codes map[string]string
}

func (m *captureMailer) SendCode(to, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.codes == nil {
		m.codes = map[string]string{}
	}
	m.codes[to] = code
	return nil
}

func (m *captureMailer) Configured() bool { return true }

func (m *captureMailer) code(to string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.codes[to]
}

func newTestServer(t *testing.T) (*httptest.Server, *captureMailer) {
	ts, _, mailer := newTestServerWithProviders(t, nil)
	return ts, mailer
}

func newTestServerWithProviders(t *testing.T, providers map[string]auth.OAuthProvider) (*httptest.Server, *gorm.DB, *captureMailer) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, e := db.DB(); e == nil {
			sqlDB.Close()
		}
	})
	mailer := &captureMailer{}
	s := New(db, Config{
		BaseURL:          "http://hub.test",
		SessionSecret:    []byte("unit-test-secret"),
		Providers:        providers,
		Mailer:           mailer,
		RegistrationOpen: true,
		Version:          "test",
	}, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>message-hub</html>")}})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, db, mailer
}

func newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

func doJSON(t *testing.T, client *http.Client, method, target string, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func registerUser(t *testing.T, ts *httptest.Server, client *http.Client, email, password string) (int, map[string]any) {
	return doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/auth/register",
		map[string]string{"email": email, "password": password}, nil)
}

func loginUser(t *testing.T, ts *httptest.Server, client *http.Client, email, password string) (int, map[string]any) {
	return doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/auth/login",
		map[string]string{"email": email, "password": password}, nil)
}

func bearerAuth(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func createToken(t *testing.T, ts *httptest.Server, client *http.Client, ratePerMin int) string {
	t.Helper()
	status, body := doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/tokens",
		map[string]any{"name": "测试令牌", "rate_limit_per_min": ratePerMin}, nil)
	if status != http.StatusCreated {
		t.Fatalf("创建令牌失败: %d %v", status, body)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatal("创建响应应包含令牌明文")
	}
	return token
}

func titlesOf(body map[string]any) []string {
	items, _ := body["items"].([]any)
	out := []string{}
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			if title, _ := m["title"].(string); title != "" {
				out = append(out, title)
			}
		}
	}
	return out
}

func contains(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// getJSONList 请求并解析数组响应（如用户/令牌列表）。
func getJSONList(t *testing.T, client *http.Client, target string) []map[string]any {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	return list
}

func TestRegisterBootstrapAndLogin(t *testing.T) {
	ts, mailer := newTestServer(t)
	admin := newClient()

	// 首个用户自动成为平台管理员
	status, body := registerUser(t, ts, admin, "admin@ex.com", "password123")
	if status != http.StatusCreated || body["role"] != "admin" {
		t.Fatalf("首用户应为管理员: %d %v", status, body)
	}

	// 登出后会话失效
	if status, _ = doJSON(t, admin, http.MethodPost, ts.URL+"/api/v1/auth/logout", nil, nil); status != http.StatusNoContent {
		t.Fatalf("登出应为 204: %d", status)
	}
	if status, _ = doJSON(t, admin, http.MethodGet, ts.URL+"/api/v1/auth/me", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("登出后 me 应为 401: %d", status)
	}

	// 重新登录
	if status, body = loginUser(t, ts, admin, "admin@ex.com", "password123"); status != http.StatusOK || body["role"] != "admin" {
		t.Fatalf("登录失败: %d %v", status, body)
	}
	// 错误密码
	if status, _ = loginUser(t, ts, newClient(), "admin@ex.com", "wrong-pass"); status != http.StatusUnauthorized {
		t.Fatalf("错误密码应为 401: %d", status)
	}

	// 第二个用户必须验证码
	u2 := newClient()
	if status, body = registerUser(t, ts, u2, "u2@ex.com", "password123"); status != http.StatusForbidden {
		t.Fatalf("无验证码注册应为 403: %d %v", status, body)
	}
	if status, _ = doJSON(t, u2, http.MethodPost, ts.URL+"/api/v1/auth/verification-code",
		map[string]string{"email": "u2@ex.com"}, nil); status != http.StatusNoContent {
		t.Fatalf("发送验证码应为 204")
	}
	code := mailer.code("u2@ex.com")
	if len(code) != 6 {
		t.Fatalf("应捕获 6 位验证码: %q", code)
	}
	status, body = doJSON(t, u2, http.MethodPost, ts.URL+"/api/v1/auth/register",
		map[string]string{"email": "u2@ex.com", "password": "password123", "code": code}, nil)
	if status != http.StatusCreated || body["role"] != "user" {
		t.Fatalf("验证码注册失败: %d %v", status, body)
	}
	// 重复注册
	if status, _ = registerUser(t, ts, newClient(), "u2@ex.com", "password123"); status != http.StatusConflict {
		t.Fatalf("重复注册应为 409: %d", status)
	}
	// 非法邮箱
	if status, _ = registerUser(t, ts, newClient(), "not-an-email", "password123"); status != http.StatusBadRequest {
		t.Fatalf("非法邮箱应为 400: %d", status)
	}
	// 弱密码
	if status, _ = registerUser(t, ts, newClient(), "u3@ex.com", "short"); status != http.StatusBadRequest {
		t.Fatalf("弱密码应为 400: %d", status)
	}
}

func TestMessagePushScopingAndRateLimit(t *testing.T) {
	ts, _ := newTestServer(t)
	admin := newClient()
	if status, _ := registerUser(t, ts, admin, "admin@ex.com", "password123"); status != http.StatusCreated {
		t.Fatal("初始化管理员失败")
	}
	// 建租户 factory 与用户 worker
	status, tenant := doJSON(t, admin, http.MethodPost, ts.URL+"/api/v1/tenants", map[string]string{"name": "factory"}, nil)
	if status != http.StatusCreated {
		t.Fatalf("创建租户失败: %d %v", status, tenant)
	}
	tenantID := int(tenant["id"].(float64))
	if status, _ = doJSON(t, admin, http.MethodPost, ts.URL+"/api/v1/users",
		map[string]any{"email": "worker@ex.com", "password": "password123", "tenant_id": tenantID}, nil); status != http.StatusCreated {
		t.Fatalf("创建用户失败: %d", status)
	}
	worker := newClient()
	if status, body := loginUser(t, ts, worker, "worker@ex.com", "password123"); status != http.StatusOK || body["role"] != "user" {
		t.Fatalf("worker 登录失败: %d %v", status, body)
	}

	adminToken := createToken(t, ts, admin, 3) // 每分钟 3 次
	push := func(client *http.Client, token, title string, public bool) int {
		status, body := doJSON(t, client, http.MethodPost, ts.URL+"/api/v1/messages",
			map[string]any{"title": title, "body": title, "public": public}, bearerAuth(token))
		if status >= 400 && status != http.StatusTooManyRequests && status != http.StatusForbidden {
			t.Fatalf("推送 %s 失败: %d %v", title, status, body)
		}
		return status
	}
	if s := push(admin, adminToken, "admin-private", false); s != http.StatusCreated {
		t.Fatalf("私有消息应 201: %d", s)
	}
	if s := push(admin, adminToken, "broadcast", true); s != http.StatusCreated {
		t.Fatalf("公开消息应 201: %d", s)
	}

	// worker：可见公开不可见他人租户私有
	status, body := doJSON(t, worker, http.MethodGet, ts.URL+"/api/v1/messages?scope=all", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("查询失败: %d", status)
	}
	titles := titlesOf(body)
	if !contains(titles, "broadcast") || contains(titles, "admin-private") {
		t.Fatalf("worker 可见范围错误: %v", titles)
	}
	// scope=own：worker 尚无消息
	_, body = doJSON(t, worker, http.MethodGet, ts.URL+"/api/v1/messages?scope=own", nil, nil)
	if len(titlesOf(body)) != 0 {
		t.Fatalf("own 范围应为空: %v", titlesOf(body))
	}

	// worker 用令牌推送自己的消息；无权发公开消息
	workerToken := createToken(t, ts, worker, 1000)
	if s := push(worker, workerToken, "worker-msg", false); s != http.StatusCreated {
		t.Fatalf("worker 推送应 201: %d", s)
	}
	if s := push(worker, workerToken, "worker-public", true); s != http.StatusForbidden {
		t.Fatalf("worker 发公开消息应 403: %d", s)
	}
	_, body = doJSON(t, worker, http.MethodGet, ts.URL+"/api/v1/messages?scope=all", nil, nil)
	if !contains(titlesOf(body), "worker-msg") {
		t.Fatalf("worker 应看到自己的消息: %v", titlesOf(body))
	}
	// admin（default 租户）看不到 factory 租户的私有消息
	_, body = doJSON(t, admin, http.MethodGet, ts.URL+"/api/v1/messages?scope=own", nil, nil)
	if contains(titlesOf(body), "worker-msg") {
		t.Fatalf("租户隔离失效: %v", titlesOf(body))
	}

	// 限流：admin 令牌每分钟 3，已用 2
	if s := push(admin, adminToken, "third", false); s != http.StatusCreated {
		t.Fatalf("第三次应放行: %d", s)
	}
	if s := push(admin, adminToken, "fourth", false); s != http.StatusTooManyRequests {
		t.Fatalf("第四次应 429: %d", s)
	}
	// 无效令牌
	if status, _ = doJSON(t, admin, http.MethodPost, ts.URL+"/api/v1/messages",
		map[string]any{"title": "x"}, bearerAuth("mh_invalid-token-value")); status != http.StatusUnauthorized {
		t.Fatalf("无效令牌应 401: %d", status)
	}
	// 吊销令牌后立即失效（列表接口返回数组，单独解析）
	respTokens, err := admin.Get(ts.URL + "/api/v1/tokens")
	if err != nil {
		t.Fatal(err)
	}
	var tokenList []map[string]any
	_ = json.NewDecoder(respTokens.Body).Decode(&tokenList)
	respTokens.Body.Close()
	if len(tokenList) == 0 {
		t.Fatal("令牌列表不应为空")
	}
	tokenID := int(tokenList[0]["id"].(float64))
	if status, _ = doJSON(t, admin, http.MethodDelete,
		ts.URL+fmt.Sprintf("/api/v1/tokens/%d", tokenID), nil, nil); status != http.StatusNoContent {
		t.Fatalf("吊销令牌应 204: %d", status)
	}
	if status, _ = doJSON(t, admin, http.MethodPost, ts.URL+"/api/v1/messages",
		map[string]any{"title": "x"}, bearerAuth(adminToken)); status != http.StatusUnauthorized {
		t.Fatalf("吊销后令牌应 401: %d", status)
	}
}

func TestSSEStreamTenantIsolation(t *testing.T) {
	ts, _ := newTestServer(t)
	admin := newClient()
	registerUser(t, ts, admin, "admin@ex.com", "password123")
	_, tenant := doJSON(t, admin, http.MethodPost, ts.URL+"/api/v1/tenants", map[string]string{"name": "factory"}, nil)
	tenantID := int(tenant["id"].(float64))
	doJSON(t, admin, http.MethodPost, ts.URL+"/api/v1/users",
		map[string]any{"email": "worker@ex.com", "password": "password123", "tenant_id": tenantID}, nil)
	worker := newClient()
	loginUser(t, ts, worker, "worker@ex.com", "password123")
	workerToken := createToken(t, ts, worker, 1000)
	adminToken := createToken(t, ts, admin, 1000)

	// worker 以令牌订阅 SSE
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/stream", nil)
	req.Header.Set("Authorization", "Bearer "+workerToken)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("SSE 连接失败: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type 错误: %s", ct)
	}

	lines := make(chan string, 64)
	go func() {
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				close(lines)
				return
			}
			lines <- line
		}
	}()

	readLine := func() string {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("SSE 连接提前关闭")
			}
			return line
		case <-time.After(5 * time.Second):
			t.Fatal("等待 SSE 帧超时")
			return ""
		}
	}
	if line := strings.TrimSpace(readLine()); line != "retry: 5000" {
		t.Fatalf("首帧应为 retry: %q", line)
	}
	readLine() // 空行

	// admin 推私有消息（worker 不应收）与公开消息（应收）
	push := func(token, title string, public bool) {
		doJSON(t, admin, http.MethodPost, ts.URL+"/api/v1/messages",
			map[string]any{"title": title, "public": public}, bearerAuth(token))
	}
	push(adminToken, "admin-private", false)
	push(adminToken, "tenant-broadcast", true)

	deadline := time.After(5 * time.Second)
	for {
		line := readLine()
		if strings.Contains(line, `"title":`) { // data 行
			if strings.Contains(line, "admin-private") {
				t.Fatalf("租户私有消息不应推给其他租户: %s", line)
			}
			if strings.Contains(line, "tenant-broadcast") {
				break // 收到目标公开消息
			}
		}
		select {
		case <-deadline:
			t.Fatal("未在期限内收到公开消息")
		default:
		}
	}
}

func TestOAuthFlow(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if r.Form.Get("code") != "good-code" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"at-123"}`))
		case "/user":
			if r.Header.Get("Authorization") != "Bearer at-123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42,"login":"octocat","name":"Octo Cat","email":"octo@ex.com"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(fake.Close)

	providers := map[string]auth.OAuthProvider{
		"github": auth.NewGitHubProvider("cid", "csecret", fake.URL+"/authorize", fake.URL+"/token", fake.URL),
	}
	ts, db, _ := newTestServerWithProviders(t, providers)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}
	// 第一步：跳转授权页并拿到 state
	resp, err := client.Get(ts.URL + "/api/v1/auth/oauth/github/login")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("应为 302: %d", resp.StatusCode)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if !strings.HasPrefix(resp.Header.Get("Location"), fake.URL+"/authorize") {
		t.Fatalf("授权地址错误: %s", resp.Header.Get("Location"))
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("缺少 state")
	}

	// 第二步：回调完成登录
	callback := ts.URL + "/api/v1/auth/oauth/github/callback?code=good-code&state=" + state
	resp, err = client.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("回调应 302 到 /: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	hasSession := false
	for _, c := range resp.Cookies() {
		if c.Name == auth.SessionCookie {
			hasSession = true
		}
	}
	if !hasSession {
		t.Fatal("回调应设置会话 Cookie")
	}
	me := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	status, body := doJSON(t, me, http.MethodGet, ts.URL+"/api/v1/auth/me", nil, nil)
	if status != http.StatusOK || body["email"] != "octo@ex.com" {
		t.Fatalf("OAuth 登录态异常: %d %v", status, body)
	}

	// 错误 state 拒绝
	resp, err = client.Get(ts.URL + "/api/v1/auth/oauth/github/callback?code=good-code&state=bad")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Location"), "login_error") {
		t.Fatalf("错误 state 应回跳错误提示: %s", resp.Header.Get("Location"))
	}

	// 同一身份重复登录不重复建号
	var count int64
	db.Table("users").Count(&count)
	if count != 1 {
		t.Fatalf("用户数应为 1: %d", count)
	}
}

func TestUserManagementPermissions(t *testing.T) {
	ts, mailer := newTestServer(t)
	admin := newClient()
	registerUser(t, ts, admin, "admin@ex.com", "password123")

	// 普通用户注册（带验证码）
	doJSON(t, newClient(), http.MethodPost, ts.URL+"/api/v1/auth/verification-code",
		map[string]string{"email": "plain@ex.com"}, nil)
	code := mailer.code("plain@ex.com")
	plain := newClient()
	doJSON(t, plain, http.MethodPost, ts.URL+"/api/v1/auth/register",
		map[string]string{"email": "plain@ex.com", "password": "password123", "code": code}, nil)

	// 普通用户无权访问管理接口
	if status, _ := doJSON(t, plain, http.MethodGet, ts.URL+"/api/v1/users", nil, nil); status != http.StatusForbidden {
		t.Fatalf("普通用户应 403: %d", status)
	}
	if status, _ := doJSON(t, plain, http.MethodGet, ts.URL+"/api/v1/tenants", nil, nil); status != http.StatusForbidden {
		t.Fatalf("普通用户租户接口应 403: %d", status)
	}

	// 建租户 team + 租户管理员 boss
	_, tenant := doJSON(t, admin, http.MethodPost, ts.URL+"/api/v1/tenants", map[string]string{"name": "team"}, nil)
	teamID := int(tenant["id"].(float64))
	status, bossBody := doJSON(t, admin, http.MethodPost, ts.URL+"/api/v1/users",
		map[string]any{"email": "boss@ex.com", "password": "password123", "role": "tenant_admin", "tenant_id": teamID}, nil)
	if status != http.StatusCreated || bossBody["role"] != "tenant_admin" {
		t.Fatalf("创建租户管理员失败: %d %v", status, bossBody)
	}
	bossID := int(bossBody["id"].(float64))
	boss := newClient()
	loginUser(t, ts, boss, "boss@ex.com", "password123")

	// 租户管理员只能看本租户；创建用户角色强制为 user
	userList := getJSONList(t, boss, ts.URL+"/api/v1/users")
	if len(userList) != 1 { // 只有 boss 自己
		t.Fatalf("租户管理员应只看到本租户用户: %v", userList)
	}
	status, created := doJSON(t, boss, http.MethodPost, ts.URL+"/api/v1/users",
		map[string]any{"email": "mate@ex.com", "password": "password123", "role": "admin"}, nil)
	if status != http.StatusCreated || created["role"] != "user" {
		t.Fatalf("租户管理员建用户应强制 user 角色: %d %v", status, created)
	}
	mateID := int(created["id"].(float64))

	// 租户管理员不能改其他租户用户、不能删其他租户用户
	if status, _ = doJSON(t, boss, http.MethodPut, ts.URL+fmt.Sprintf("/api/v1/users/%d", 1),
		map[string]any{"display_name": "hack"}, nil); status != http.StatusForbidden {
		t.Fatalf("越权修改应 403: %d", status)
	}
	if status, _ = doJSON(t, boss, http.MethodDelete, ts.URL+fmt.Sprintf("/api/v1/users/%d", 2), nil, nil); status != http.StatusForbidden {
		t.Fatalf("越权删除应 403: %d", status)
	}

	// 平台管理员：不能删自己；唯一管理员不能降级；可以删除租户管理员
	if status, _ = doJSON(t, admin, http.MethodDelete, ts.URL+"/api/v1/users/1", nil, nil); status != http.StatusBadRequest {
		t.Fatalf("删除自己应 400: %d", status)
	}
	if status, _ = doJSON(t, admin, http.MethodPut, ts.URL+"/api/v1/users/1",
		map[string]any{"role": "user"}, nil); status != http.StatusConflict {
		t.Fatalf("唯一管理员降级应 409: %d", status)
	}
	if status, _ = doJSON(t, admin, http.MethodDelete, ts.URL+fmt.Sprintf("/api/v1/users/%d", bossID), nil, nil); status != http.StatusNoContent {
		t.Fatalf("删除租户管理员应 204: %d", status)
	}
	if status, _ = doJSON(t, admin, http.MethodDelete, ts.URL+fmt.Sprintf("/api/v1/users/%d", mateID), nil, nil); status != http.StatusNoContent {
		t.Fatalf("删除用户应 204: %d", status)
	}

	// 租户保护：默认租户不可删；空租户可删
	if status, _ = doJSON(t, admin, http.MethodDelete, ts.URL+fmt.Sprintf("/api/v1/tenants/%d", teamID), nil, nil); status != http.StatusNoContent {
		t.Fatalf("空租户删除应 204: %d", status)
	}
	if status, _ = doJSON(t, admin, http.MethodDelete, ts.URL+"/api/v1/tenants/1", nil, nil); status != http.StatusBadRequest {
		t.Fatalf("默认租户删除应 400: %d", status)
	}

	// 统计
	if status, stats := doJSON(t, admin, http.MethodGet, ts.URL+"/api/v1/stats", nil, nil); status != http.StatusOK || stats["tenants"] == nil {
		t.Fatalf("统计接口异常: %d %v", status, stats)
	}
}

func TestSetupTLSAutoGenerationAndMTLS(t *testing.T) {
	dir := t.TempDir()
	certPath := dir + "/server.crt"
	keyPath := dir + "/server.key"

	// 首次调用自动生成自签证书并落盘
	cfg, err := SetupTLS(certPath, keyPath, "")
	if err != nil || cfg == nil {
		t.Fatalf("自签生成失败: %v", err)
	}
	// 再次调用从文件加载
	cfg2, err := SetupTLS(certPath, keyPath, "")
	if err != nil || cfg2 == nil {
		t.Fatalf("加载证书失败: %v", err)
	}
	// 只传一个参数报错
	if _, err = SetupTLS(dir+"/x.crt", "", ""); err == nil {
		t.Fatal("缺私钥应报错")
	}

	// mTLS：CA + 服务端 + 客户端证书，验证握手准入
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)

	newLeaf := func(cn string, usage x509.ExtKeyUsage) tls.Certificate {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: cn},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage},
			IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"},
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	}
	serverCert := newLeaf("server", x509.ExtKeyUsageServerAuth)
	clientCert := newLeaf("client-1", x509.ExtKeyUsageClientAuth)

	// 写客户端 CA 文件并启用双向认证
	caPath := dir + "/ca.crt"
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: caDER,
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	mtlsCfg, err := SetupTLS(certPath, keyPath, caPath)
	if err != nil || mtlsCfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("mTLS 配置错误: %v", err)
	}
	mtlsCfg.Certificates = []tls.Certificate{serverCert}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	srv.TLS = mtlsCfg
	srv.StartTLS()
	t.Cleanup(srv.Close)

	// 带客户端证书可访问
	withCert := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{clientCert}, RootCAs: poolOf(caCert),
	}}}
	resp, err := withCert.Get(srv.URL)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("携带客户端证书应成功: %v", err)
	}
	resp.Body.Close()

	// 不带客户端证书握手失败
	bare := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: poolOf(caCert),
	}}}
	if _, err = bare.Get(srv.URL); err == nil {
		t.Fatal("无客户端证书应被拒绝")
	}
}

func poolOf(cert *x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return pool
}

func TestStaticWebServed(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "message-hub") {
		t.Fatalf("首页应返回内嵌页面: %d %s", resp.StatusCode, body)
	}
}
