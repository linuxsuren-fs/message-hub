package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/linuxsuren/message-hub/internal/model"
	"github.com/linuxsuren/message-hub/internal/store"
)

const oauthStateCookie = "mh_oauth_state"

// handleOAuthLogin GET /api/v1/auth/oauth/{provider}/login：跳转第三方授权页。
func (s *Server) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.cfg.Providers[r.PathValue("provider")]
	if !ok {
		writeError(w, http.StatusNotFound, "未配置的登录方式")
		return
	}
	state := randomHex(16)
	http.SetCookie(w, &http.Cookie{
		Name: oauthStateCookie, Value: state, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.cfg.SecureCookie, MaxAge: 600,
	})
	http.Redirect(w, r, provider.AuthCodeURL(s.redirectURI(provider.Name()), state), http.StatusFound)
}

// handleOAuthCallback GET /api/v1/auth/oauth/{provider}/callback：完成登录并回跳首页。
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.cfg.Providers[r.PathValue("provider")]
	if !ok {
		s.oauthFail(w, r, "未配置的登录方式")
		return
	}
	q := r.URL.Query()
	stateCookie, err := r.Cookie(oauthStateCookie)
	if err != nil || stateCookie.Value == "" || q.Get("state") == "" || stateCookie.Value != q.Get("state") {
		s.oauthFail(w, r, "登录状态校验失败，请重新登录")
		return
	}
	if q.Get("error") != "" {
		s.oauthFail(w, r, "第三方登录被拒绝")
		return
	}
	code := q.Get("code")
	if code == "" {
		s.oauthFail(w, r, "缺少授权码")
		return
	}
	accessToken, err := provider.Exchange(r.Context(), code, s.redirectURI(provider.Name()))
	if err != nil {
		log.Printf("OAuth 换取令牌失败 provider=%s: %v", provider.Name(), err)
		s.oauthFail(w, r, "换取登录凭证失败")
		return
	}
	ou, err := provider.FetchUser(r.Context(), accessToken)
	if err != nil {
		log.Printf("OAuth 获取用户信息失败 provider=%s: %v", provider.Name(), err)
		s.oauthFail(w, r, "获取用户信息失败")
		return
	}

	var user model.User
	var identity model.OAuthIdentity
	err = s.db.Where("provider = ? AND provider_uid = ?", provider.Name(), ou.UID).First(&identity).Error
	if err == nil {
		if err := s.db.Preload("Tenant").First(&user, identity.UserID).Error; err != nil {
			s.oauthFail(w, r, "账号状态异常")
			return
		}
	} else {
		email := normalizeEmail(ou.Email)
		if email != "" { // 已注册邮箱直接绑定（第三方已验证邮箱所有权）
			s.db.Preload("Tenant").Where("email = ?", email).First(&user)
		}
		if user.ID == 0 {
			if !validEmail(email) {
				email = fmt.Sprintf("%s-%s@oauth.message-hub.local", provider.Name(), ou.UID)
			}
			tenantID, err := store.DefaultTenantID(s.db)
			if err != nil {
				s.oauthFail(w, r, "默认租户缺失")
				return
			}
			display := ou.Name
			if display == "" {
				display = ou.Login
			}
			user = model.User{
				TenantID: tenantID, Email: email, DisplayName: display, Role: model.RoleUser,
			}
			if err := s.db.Create(&user).Error; err != nil {
				log.Printf("OAuth 创建用户失败: %v", err)
				s.oauthFail(w, r, "创建用户失败")
				return
			}
			var tenant model.Tenant
			s.db.First(&tenant, tenantID)
			user.Tenant = &tenant
		}
		if err := s.db.Create(&model.OAuthIdentity{
			UserID: user.ID, Provider: provider.Name(), ProviderUID: ou.UID,
		}).Error; err != nil {
			log.Printf("OAuth 绑定身份失败: %v", err)
		}
	}
	s.sessions.SetCookie(w, user.ID, s.cfg.SecureCookie)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) redirectURI(provider string) string {
	return strings.TrimSuffix(s.cfg.BaseURL, "/") + "/api/v1/auth/oauth/" + provider + "/callback"
}

func (s *Server) oauthFail(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/?login_error="+url.QueryEscape(msg), http.StatusFound)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(buf)
}
