package httpserver

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/linuxsuren/message-hub/internal/auth"
	"github.com/linuxsuren/message-hub/internal/model"
	"github.com/linuxsuren/message-hub/internal/store"
)

func validRole(role string) bool {
	return role == model.RoleAdmin || role == model.RoleTenantAdmin || role == model.RoleUser
}

func adminCount(db *gorm.DB) int64 {
	var n int64
	db.Model(&model.User{}).Where("role = ?", model.RoleAdmin).Count(&n)
	return n
}

// handleListUsers 平台管理员看全部，租户管理员看本租户。
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	current := s.currentUser(r)
	q := s.db.Preload("Tenant").Model(&model.User{})
	if current.Role != model.RoleAdmin {
		q = q.Where("tenant_id = ?", current.TenantID)
	} else if tid := parseUint(r.URL.Query().Get("tenant_id")); tid > 0 {
		q = q.Where("tenant_id = ?", tid)
	}
	var users []model.User
	if err := q.Order("id ASC").Find(&users).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "查询用户失败")
		return
	}
	items := make([]model.UserDTO, 0, len(users))
	for i := range users {
		items = append(items, toUserDTO(&users[i]))
	}
	writeJSON(w, http.StatusOK, items)
}

// handleCreateUser 管理员/租户管理员创建用户。
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	current := s.currentUser(r)
	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		TenantID    uint64 `json:"tenant_id"`
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
	role := req.Role
	if role == "" {
		role = model.RoleUser
	}
	if !validRole(role) {
		writeError(w, http.StatusBadRequest, "角色不合法")
		return
	}
	tenantID := req.TenantID
	if current.Role != model.RoleAdmin {
		tenantID = current.TenantID // 租户管理员只能在本租户创建用户
		role = model.RoleUser
	} else if tenantID == 0 {
		tenantID, _ = store.DefaultTenantID(s.db)
	}
	var tenant model.Tenant
	if err := s.db.First(&tenant, tenantID).Error; err != nil {
		writeError(w, http.StatusBadRequest, "所属租户不存在")
		return
	}
	var dup int64
	s.db.Model(&model.User{}).Where("email = ?", email).Count(&dup)
	if dup > 0 {
		writeError(w, http.StatusConflict, "该邮箱已存在")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = emailDisplayName(email)
	}
	user := model.User{
		TenantID: tenantID, Email: email, PasswordHash: hash,
		DisplayName: displayName, Role: role,
	}
	if err := s.db.Create(&user).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "创建用户失败")
		return
	}
	user.Tenant = &tenant
	writeJSON(w, http.StatusCreated, toUserDTO(&user))
}

// handleUpdateUser 更新用户资料/角色/租户/密码。
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	current := s.currentUser(r)
	id := parseUint(r.PathValue("id"))
	var req struct {
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		TenantID    uint64 `json:"tenant_id"`
		Password    string `json:"password"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	var user model.User
	if err := s.db.First(&user, id).Error; err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if current.Role != model.RoleAdmin {
		// 租户管理员：仅能改本租户的普通用户，不能改租户、不能授予管理员
		if user.TenantID != current.TenantID || user.Role != model.RoleUser {
			writeError(w, http.StatusForbidden, "没有权限修改该用户")
			return
		}
		if req.Role != "" && req.Role != model.RoleUser && req.Role != model.RoleTenantAdmin {
			writeError(w, http.StatusForbidden, "没有权限设置该角色")
			return
		}
		req.TenantID = 0
	}
	updates := map[string]any{}
	if req.DisplayName != "" {
		name := strings.TrimSpace(req.DisplayName)
		if utf8.RuneCountInString(name) > 64 {
			writeError(w, http.StatusBadRequest, "昵称不超过 64 字")
			return
		}
		updates["display_name"] = name
	}
	if req.Role != "" {
		if !validRole(req.Role) {
			writeError(w, http.StatusBadRequest, "角色不合法")
			return
		}
		if user.Role == model.RoleAdmin && req.Role != model.RoleAdmin && adminCount(s.db) <= 1 {
			writeError(w, http.StatusConflict, "系统至少保留一名平台管理员")
			return
		}
		updates["role"] = req.Role
	}
	if req.TenantID != 0 {
		var tenant model.Tenant
		if err := s.db.First(&tenant, req.TenantID).Error; err != nil {
			writeError(w, http.StatusBadRequest, "目标租户不存在")
			return
		}
		updates["tenant_id"] = req.TenantID
	}
	if req.Password != "" {
		if len(req.Password) < 8 {
			writeError(w, http.StatusBadRequest, "密码至少 8 位")
			return
		}
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "密码处理失败")
			return
		}
		updates["password_hash"] = hash
	}
	if len(updates) == 0 {
		writeError(w, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	if err := s.db.Model(&user).Updates(updates).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "更新用户失败")
		return
	}
	s.db.Preload("Tenant").First(&user, id)
	writeJSON(w, http.StatusOK, toUserDTO(&user))
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	current := s.currentUser(r)
	id := parseUint(r.PathValue("id"))
	if id == current.ID {
		writeError(w, http.StatusBadRequest, "不能删除当前登录账号")
		return
	}
	var user model.User
	if err := s.db.First(&user, id).Error; err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if current.Role != model.RoleAdmin {
		if user.TenantID != current.TenantID || user.Role != model.RoleUser {
			writeError(w, http.StatusForbidden, "没有权限删除该用户")
			return
		}
	} else if user.Role == model.RoleAdmin && adminCount(s.db) <= 1 {
		writeError(w, http.StatusConflict, "系统至少保留一名平台管理员")
		return
	}
	if err := s.db.Delete(&user).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "删除用户失败")
		return
	}
	s.db.Where("user_id = ?", id).Delete(&model.OAuthIdentity{})
	s.db.Where("user_id = ?", id).Delete(&model.Token{})
	w.WriteHeader(http.StatusNoContent)
}

// handleStats 平台概览统计。
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	var tenants, users, tokens, messages int64
	s.db.Model(&model.Tenant{}).Count(&tenants)
	s.db.Model(&model.User{}).Count(&users)
	s.db.Model(&model.Token{}).Where("revoked_at IS NULL").Count(&tokens)
	s.db.Model(&model.Message{}).Count(&messages)
	writeJSON(w, http.StatusOK, map[string]int64{
		"tenants": tenants, "users": users, "tokens": tokens, "messages": messages,
	})
}
