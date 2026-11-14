// Package httpserver 实现 message-hub 的全部 HTTP 接口与内嵌 Web 管理台。
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/linuxsuren/message-hub/internal/auth"
	"github.com/linuxsuren/message-hub/internal/model"
)

// codeSender 抽象验证码邮件发送；生产环境注入 *auth.Mailer，测试可注入捕获器。
type codeSender interface {
	SendCode(to, code string) error
	Configured() bool
}

// Config 服务端配置。
type Config struct {
	BaseURL          string // 对外基础 URL，用于 OAuth 回调
	SessionSecret    []byte // 会话签名密钥
	Providers        map[string]auth.OAuthProvider
	Mailer           codeSender
	RegistrationOpen bool
	SecureCookie     bool // HTTPS 下置 true
	Version          string
}

// Server 聚合数据库、配置与全部处理器。
type Server struct {
	db       *gorm.DB
	cfg      Config
	sessions *auth.SessionManager
	limiter  *tokenLimiter
	hub      *sseHub
	handler  http.Handler
}

// New 创建服务；webFS 为内嵌前端文件系统（根对应前端构建产物目录）。
func New(db *gorm.DB, cfg Config, webFS fs.FS) *Server {
	if len(cfg.SessionSecret) == 0 {
		panic("session secret 不能为空")
	}
	s := &Server{
		db:       db,
		cfg:      cfg,
		sessions: auth.NewSessionManager(cfg.SessionSecret),
		limiter:  newTokenLimiter(),
		hub:      newSSEHub(),
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux, webFS)
	s.handler = s.withRecovery(s.withLogging(mux))
	return s
}

// Handler 返回根处理器。
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) registerRoutes(mux *http.ServeMux, webFS fs.FS) {
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/config", s.handleConfig)
	mux.HandleFunc("POST /api/v1/auth/verification-code", s.handleSendCode)
	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/auth/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("GET /api/v1/auth/oauth/{provider}/login", s.handleOAuthLogin)
	mux.HandleFunc("GET /api/v1/auth/oauth/{provider}/callback", s.handleOAuthCallback)

	mux.HandleFunc("GET /api/v1/messages", s.requireAuth(s.handleListMessages))
	mux.HandleFunc("POST /api/v1/messages", s.requireAuth(s.handleCreateMessage))
	mux.HandleFunc("GET /api/v1/stream", s.requireAuth(s.handleStream))

	mux.HandleFunc("GET /api/v1/tokens", s.requireAuth(s.handleListTokens))
	mux.HandleFunc("POST /api/v1/tokens", s.requireAuth(s.handleCreateToken))
	mux.HandleFunc("DELETE /api/v1/tokens/{id}", s.requireAuth(s.handleDeleteToken))

	mux.HandleFunc("GET /api/v1/tenants", s.requireAuth(s.requireRole(s.handleListTenants, model.RoleAdmin)))
	mux.HandleFunc("POST /api/v1/tenants", s.requireAuth(s.requireRole(s.handleCreateTenant, model.RoleAdmin)))
	mux.HandleFunc("PUT /api/v1/tenants/{id}", s.requireAuth(s.requireRole(s.handleUpdateTenant, model.RoleAdmin)))
	mux.HandleFunc("DELETE /api/v1/tenants/{id}", s.requireAuth(s.requireRole(s.handleDeleteTenant, model.RoleAdmin)))

	mux.HandleFunc("GET /api/v1/users", s.requireAuth(s.requireRole(s.handleListUsers, model.RoleAdmin, model.RoleTenantAdmin)))
	mux.HandleFunc("POST /api/v1/users", s.requireAuth(s.requireRole(s.handleCreateUser, model.RoleAdmin, model.RoleTenantAdmin)))
	mux.HandleFunc("PUT /api/v1/users/{id}", s.requireAuth(s.requireRole(s.handleUpdateUser, model.RoleAdmin, model.RoleTenantAdmin)))
	mux.HandleFunc("DELETE /api/v1/users/{id}", s.requireAuth(s.requireRole(s.handleDeleteUser, model.RoleAdmin, model.RoleTenantAdmin)))

	mux.HandleFunc("GET /api/v1/stats", s.requireAuth(s.requireRole(s.handleStats, model.RoleAdmin)))

	if webFS != nil {
		mux.Handle("GET /", http.FileServerFS(webFS))
	}
}

// ---- 中间件 ----

type ctxKey int

const (
	ctxUserKey ctxKey = iota
	ctxTokenKey
)

var errUnauthorized = errors.New("unauthorized")

// resolveAuth 解析 Bearer 令牌或会话 Cookie；返回用户与令牌 ID（会话登录时为 0）。
func (s *Server) resolveAuth(r *http.Request) (*model.User, uint64, error) {
	header := r.Header.Get("Authorization")
	if tokenStr, ok := strings.CutPrefix(header, "Bearer "); ok && strings.HasPrefix(tokenStr, auth.TokenPrefix) {
		var tk model.Token
		if err := s.db.Where("token_hash = ? AND revoked_at IS NULL",
			auth.HashToken(tokenStr)).First(&tk).Error; err != nil {
			return nil, 0, errUnauthorized
		}
		var u model.User
		if err := s.db.Preload("Tenant").First(&u, tk.UserID).Error; err != nil {
			return nil, 0, errUnauthorized
		}
		now := time.Now()
		if tk.LastUsedAt == nil || now.Sub(*tk.LastUsedAt) > time.Minute {
			s.db.Model(&tk).Update("last_used_at", now)
		}
		return &u, tk.ID, nil
	}
	cookie, err := r.Cookie(auth.SessionCookie)
	if err != nil || cookie.Value == "" {
		return nil, 0, errUnauthorized
	}
	uid, ok := s.sessions.Verify(cookie.Value)
	if !ok {
		return nil, 0, errUnauthorized
	}
	var u model.User
	if err := s.db.Preload("Tenant").First(&u, uid).Error; err != nil {
		return nil, 0, errUnauthorized
	}
	return &u, 0, nil
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, tokenID, err := s.resolveAuth(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "未登录或凭证无效")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserKey, u)
		if tokenID != 0 {
			ctx = context.WithValue(ctx, ctxTokenKey, tokenID)
		}
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) requireRole(next http.HandlerFunc, roles ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.currentUser(r)
		for _, role := range roles {
			if u != nil && u.Role == role {
				next(w, r)
				return
			}
		}
		writeError(w, http.StatusForbidden, "没有权限执行该操作")
	}
}

func (s *Server) currentUser(r *http.Request) *model.User {
	u, _ := r.Context().Value(ctxUserKey).(*model.User)
	return u
}

func (s *Server) currentTokenID(r *http.Request) uint64 {
	id, _ := r.Context().Value(ctxTokenKey).(uint64)
	return id
}

// statusWriter 记录状态码；透传 Flush 以支持 SSE。
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if r.URL.Path != "/api/v1/stream" { // SSE 长连接不记录完成日志
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status,
				time.Since(start).Round(time.Millisecond))
		}
	})
}

func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic %s %s: %v", r.Method, r.URL.Path, rec)
				writeError(w, http.StatusInternalServerError, "服务内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- 工具 ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("写响应失败: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeBody 解析 JSON 请求体（上限 1MB）；空请求体视为合法（全默认值）。
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取请求体失败")
		return false
	}
	if len(body) == 0 {
		return true
	}
	if len(body) > 1<<20 {
		writeError(w, http.StatusRequestEntityTooLarge, "请求体过大")
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return false
	}
	return true
}

func toUserDTO(u *model.User) model.UserDTO {
	dto := model.UserDTO{
		ID: u.ID, Email: u.Email, DisplayName: u.DisplayName,
		Role: u.Role, CreatedAt: u.CreatedAt,
	}
	if u.Tenant != nil {
		dto.Tenant = model.TenantRef{ID: u.Tenant.ID, Name: u.Tenant.Name}
	}
	return dto
}

func toTokenDTO(t model.Token) model.TokenDTO {
	return model.TokenDTO{
		ID: t.ID, Name: t.Name, RateLimitPerMin: t.RateLimitPerMin,
		LastUsedAt: t.LastUsedAt, CreatedAt: t.CreatedAt,
	}
}

func clampInt(raw string, def, min, max int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func parseUint(raw string) uint64 {
	n, _ := strconv.ParseUint(raw, 10, 64)
	return n
}

func validEmail(email string) bool {
	if email == "" || len(email) > 255 {
		return false
	}
	parsed, err := mail.ParseAddress(email)
	return err == nil && parsed.Address == email && strings.Contains(email, "@")
}
