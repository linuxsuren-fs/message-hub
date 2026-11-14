package httpserver

import (
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/linuxsuren/message-hub/internal/auth"
	"github.com/linuxsuren/message-hub/internal/model"
)

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	var tokens []model.Token
	if err := s.db.Where("user_id = ? AND revoked_at IS NULL", user.ID).
		Order("id DESC").Find(&tokens).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "查询令牌失败")
		return
	}
	items := make([]model.TokenDTO, 0, len(tokens))
	for _, t := range tokens {
		items = append(items, toTokenDTO(t))
	}
	writeJSON(w, http.StatusOK, items)
}

// handleCreateToken 创建令牌；明文仅本次响应返回。
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string `json:"name"`
		RateLimitPerMin int    `json:"rate_limit_per_min"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "默认令牌"
	}
	if utf8.RuneCountInString(name) > 64 {
		writeError(w, http.StatusBadRequest, "名称不超过 64 字")
		return
	}
	if req.RateLimitPerMin == 0 {
		req.RateLimitPerMin = 60
	}
	if req.RateLimitPerMin < 1 || req.RateLimitPerMin > 100000 {
		writeError(w, http.StatusBadRequest, "每分钟上限取值 1 ~ 100000")
		return
	}
	plain, err := auth.GenerateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成令牌失败")
		return
	}
	tk := model.Token{
		UserID: s.currentUser(r).ID, Name: name,
		TokenHash: auth.HashToken(plain), RateLimitPerMin: req.RateLimitPerMin,
	}
	if err := s.db.Create(&tk).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "保存令牌失败")
		return
	}
	dto := toTokenDTO(tk)
	dto.Token = plain
	writeJSON(w, http.StatusCreated, dto)
}

// handleDeleteToken 吊销令牌（仅本人）。
func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	id := parseUint(r.PathValue("id"))
	if id == 0 {
		writeError(w, http.StatusBadRequest, "令牌 ID 不合法")
		return
	}
	res := s.db.Model(&model.Token{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", id, s.currentUser(r).ID).
		Update("revoked_at", time.Now())
	if res.Error != nil || res.RowsAffected == 0 {
		writeError(w, http.StatusNotFound, "令牌不存在")
		return
	}
	s.limiter.remove(id)
	log.Printf("令牌已吊销 id=%d", id)
	w.WriteHeader(http.StatusNoContent)
}
