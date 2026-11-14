package httpserver

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/linuxsuren/message-hub/internal/auth"
	"github.com/linuxsuren/message-hub/internal/model"
	"github.com/linuxsuren/message-hub/internal/store"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.cfg.Version})
}

// handleConfig 返回前端/客户端需要的公开配置。
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	providers := make([]string, 0, len(s.cfg.Providers))
	for name := range s.cfg.Providers {
		providers = append(providers, name)
	}
	sortStrings(providers)
	writeJSON(w, http.StatusOK, map[string]any{
		"oauth_providers":   providers,
		"registration_open": s.cfg.RegistrationOpen,
		"version":           s.cfg.Version,
	})
}

func sortStrings(arr []string) {
	for i := 1; i < len(arr); i++ {
		for j := i; j > 0 && arr[j] < arr[j-1]; j-- {
			arr[j], arr[j-1] = arr[j-1], arr[j]
		}
	}
}

// handleSendCode 发送注册验证码。
func (s *Server) handleSendCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	if !validEmail(email) {
		writeError(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}
	if !s.cfg.RegistrationOpen {
		writeError(w, http.StatusForbidden, "当前未开放注册")
		return
	}
	var count int64
	s.db.Model(&model.User{}).Where("email = ?", email).Count(&count)
	if count > 0 {
		writeError(w, http.StatusConflict, "该邮箱已注册，请直接登录")
		return
	}
	code, err := auth.IssueVerificationCode(s.db, email)
	if err != nil {
		if errors.Is(err, auth.ErrCodeTooFrequent) {
			writeError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		log.Printf("生成验证码失败 email=%s: %v", email, err)
		writeError(w, http.StatusInternalServerError, "生成验证码失败")
		return
	}
	if s.cfg.Mailer != nil && s.cfg.Mailer.Configured() {
		if err := s.cfg.Mailer.SendCode(email, code); err != nil {
			log.Printf("发送验证码邮件失败 email=%s: %v", email, err)
			writeError(w, http.StatusBadGateway, "验证码邮件发送失败，请稍后重试")
			return
		}
	} else {
		log.Printf("SMTP 未配置，验证码仅输出到日志：email=%s code=%s", email, code)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRegister 邮箱自注册；首个注册用户自动成为平台管理员（无需验证码）。
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	if !validEmail(email) {
		writeError(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "密码至少 8 位")
		return
	}
	if !s.cfg.RegistrationOpen {
		writeError(w, http.StatusForbidden, "当前未开放注册")
		return
	}
	var userCount int64
	if err := s.db.Model(&model.User{}).Count(&userCount).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "初始化数据异常")
		return
	}
	var dup int64
	s.db.Model(&model.User{}).Where("email = ?", email).Count(&dup)
	if dup > 0 {
		writeError(w, http.StatusConflict, "该邮箱已注册")
		return
	}
	role := model.RoleUser
	if userCount == 0 {
		role = model.RoleAdmin // 引导：首个注册用户即平台管理员
	} else if !auth.CheckVerificationCode(s.db, email, strings.TrimSpace(req.Code)) {
		writeError(w, http.StatusForbidden, "验证码错误或已过期")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	tenantID, err := store.DefaultTenantID(s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "默认租户缺失")
		return
	}
	user := model.User{
		TenantID: tenantID, Email: email, PasswordHash: hash,
		DisplayName: emailDisplayName(email), Role: role,
	}
	if err := s.db.Create(&user).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "创建用户失败")
		return
	}
	var tenant model.Tenant
	s.db.First(&tenant, tenantID)
	user.Tenant = &tenant
	s.sessions.SetCookie(w, user.ID, s.cfg.SecureCookie)
	writeJSON(w, http.StatusCreated, toUserDTO(&user))
}

// handleLogin 邮箱密码登录。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	var user model.User
	err := s.db.Preload("Tenant").Where("email = ?", email).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusUnauthorized, "邮箱或密码错误")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询用户失败")
		return
	}
	if user.PasswordHash == "" {
		writeError(w, http.StatusForbidden, "该账号未设置密码，请使用第三方登录")
		return
	}
	if !auth.VerifyPassword(user.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "邮箱或密码错误")
		return
	}
	s.sessions.SetCookie(w, user.ID, s.cfg.SecureCookie)
	writeJSON(w, http.StatusOK, toUserDTO(&user))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.ClearCookie(w, s.cfg.SecureCookie)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toUserDTO(s.currentUser(r)))
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// emailDisplayName 取邮箱本地部分作为默认昵称。
func emailDisplayName(email string) string {
	if idx := strings.IndexByte(email, '@'); idx > 0 {
		return email[:idx]
	}
	return email
}
