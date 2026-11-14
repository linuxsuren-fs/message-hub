package httpserver

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/linuxsuren/message-hub/internal/model"
)

// handleListMessages GET /api/v1/messages?limit=&cursor=&scope=all|own|public
// 可见范围：本人租户消息 + 公开消息；scope 可进一步收窄。
func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	limit := clampInt(r.URL.Query().Get("limit"), 50, 1, 100)
	q := s.db.Model(&model.Message{}).Order("id DESC").Limit(limit + 1)
	switch r.URL.Query().Get("scope") {
	case "own":
		q = q.Where("tenant_id = ?", user.TenantID)
	case "public":
		q = q.Where("public = ?", true)
	default: // all
		q = q.Where("tenant_id = ? OR public = ?", user.TenantID, true)
	}
	if cursor := parseUint(r.URL.Query().Get("cursor")); cursor > 0 {
		q = q.Where("id < ?", cursor)
	}
	var msgs []model.Message
	if err := q.Find(&msgs).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "查询消息失败")
		return
	}
	var next any
	if len(msgs) > limit {
		msgs = msgs[:limit]
		next = msgs[limit-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": msgs, "next_cursor": next})
}

// handleCreateMessage POST /api/v1/messages：推送消息（令牌认证时按令牌限流）。
func (s *Server) handleCreateMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Topic string `json:"topic"`
		Level string `json:"level"`
		Public bool  `json:"public"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Topic = strings.TrimSpace(req.Topic)
	if req.Title == "" || utf8.RuneCountInString(req.Title) > 128 {
		writeError(w, http.StatusBadRequest, "标题不能为空且不超过 128 字")
		return
	}
	if utf8.RuneCountInString(req.Body) > 4096 {
		writeError(w, http.StatusBadRequest, "正文不超过 4096 字")
		return
	}
	if utf8.RuneCountInString(req.Topic) > 64 {
		writeError(w, http.StatusBadRequest, "主题不超过 64 字")
		return
	}
	level := req.Level
	if level == "" {
		level = model.LevelInfo
	}
	valid := false
	for _, l := range model.Levels() {
		if level == l {
			valid = true
			break
		}
	}
	if !valid {
		writeError(w, http.StatusBadRequest, "级别只支持 info、warning、error")
		return
	}
	user := s.currentUser(r)
	if req.Public && user.Role != model.RoleAdmin && user.Role != model.RoleTenantAdmin {
		writeError(w, http.StatusForbidden, "仅管理员角色可发送公开消息")
		return
	}
	if tokenID := s.currentTokenID(r); tokenID != 0 {
		var tk model.Token
		if err := s.db.First(&tk, tokenID).Error; err == nil {
			if !s.limiter.allow(tokenID, tk.RateLimitPerMin) {
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusTooManyRequests, "已触发流量限制，请降低推送频率")
				return
			}
		}
	}
	msg := model.Message{
		TenantID: user.TenantID, UserID: user.ID,
		Topic: req.Topic, Title: req.Title, Body: req.Body,
		Level: level, Public: req.Public, CreatedAt: time.Now(),
	}
	if err := s.db.Create(&msg).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "消息保存失败")
		return
	}
	go s.hub.publish(&msg)
	writeJSON(w, http.StatusCreated, msg)
}
