package httpserver

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/linuxsuren/message-hub/internal/model"
	"github.com/linuxsuren/message-hub/internal/store"
)

// handleListTenants 列出租户（含用户数）。
func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	var tenants []model.Tenant
	if err := s.db.Order("id ASC").Find(&tenants).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "查询租户失败")
		return
	}
	type countRow struct {
		TenantID uint64
		C        int64
	}
	var rows []countRow
	s.db.Model(&model.User{}).Select("tenant_id, count(*) as c").
		Group("tenant_id").Scan(&rows)
	counts := map[uint64]int64{}
	for _, row := range rows {
		counts[row.TenantID] = row.C
	}
	items := make([]model.TenantDTO, 0, len(tenants))
	for _, t := range tenants {
		items = append(items, model.TenantDTO{
			ID: t.ID, Name: t.Name, UserCount: counts[t.ID], CreatedAt: t.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		writeError(w, http.StatusBadRequest, "租户名不能为空且不超过 64 字")
		return
	}
	var dup int64
	s.db.Model(&model.Tenant{}).Where("name = ?", name).Count(&dup)
	if dup > 0 {
		writeError(w, http.StatusConflict, "租户名已存在")
		return
	}
	tenant := model.Tenant{Name: name, CreatedAt: time.Now()}
	if err := s.db.Create(&tenant).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "创建租户失败")
		return
	}
	writeJSON(w, http.StatusCreated, model.TenantDTO{ID: tenant.ID, Name: tenant.Name, CreatedAt: tenant.CreatedAt})
}

func (s *Server) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	id := parseUint(r.PathValue("id"))
	var req struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		writeError(w, http.StatusBadRequest, "租户名不能为空且不超过 64 字")
		return
	}
	var tenant model.Tenant
	if err := s.db.First(&tenant, id).Error; err != nil {
		writeError(w, http.StatusNotFound, "租户不存在")
		return
	}
	var dup int64
	s.db.Model(&model.Tenant{}).Where("name = ? AND id != ?", name, id).Count(&dup)
	if dup > 0 {
		writeError(w, http.StatusConflict, "租户名已存在")
		return
	}
	if err := s.db.Model(&tenant).Update("name", name).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "更新租户失败")
		return
	}
	writeJSON(w, http.StatusOK, model.TenantDTO{ID: tenant.ID, Name: name, CreatedAt: tenant.CreatedAt})
}

func (s *Server) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	id := parseUint(r.PathValue("id"))
	var tenant model.Tenant
	if err := s.db.First(&tenant, id).Error; err != nil {
		writeError(w, http.StatusNotFound, "租户不存在")
		return
	}
	if tenant.Name == store.DefaultTenant {
		writeError(w, http.StatusBadRequest, "默认租户不可删除")
		return
	}
	var users int64
	s.db.Model(&model.User{}).Where("tenant_id = ?", id).Count(&users)
	if users > 0 {
		writeError(w, http.StatusConflict, "租户下仍有用户，请先迁移用户")
		return
	}
	if err := s.db.Delete(&tenant).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "删除租户失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
