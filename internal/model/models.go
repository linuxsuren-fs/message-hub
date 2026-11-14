// Package model 定义持久化数据模型与对外传输对象（DTO）。
package model

import "time"

// 角色取值。
const (
	RoleAdmin        = "admin"         // 平台管理员
	RoleTenantAdmin  = "tenant_admin"  // 租户管理员
	RoleUser         = "user"          // 普通用户
)

// 消息级别取值。
const (
	LevelInfo    = "info"
	LevelWarning = "warning"
	LevelError   = "error"
)

// Levels 返回合法的消息级别。
func Levels() []string {
	return []string{LevelInfo, LevelWarning, LevelError}
}

// Tenant 租户。
type Tenant struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:64;uniqueIndex;not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// User 用户。
type User struct {
	ID           uint64 `gorm:"primaryKey" json:"id"`
	TenantID     uint64 `gorm:"index;not null" json:"tenant_id"`
	Tenant       *Tenant `gorm:"foreignKey:TenantID" json:"-"`
	Email        string `gorm:"size:255;uniqueIndex;not null" json:"email"`
	PasswordHash string `gorm:"size:255" json:"-"` // OAuth 账号可能为空
	DisplayName  string `gorm:"size:64" json:"display_name"`
	Role         string `gorm:"size:32;index;not null;default:user" json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

// OAuthIdentity 第三方登录身份（provider + provider_uid 唯一）。
type OAuthIdentity struct {
	ID          uint64 `gorm:"primaryKey" json:"id"`
	UserID      uint64 `gorm:"index;not null" json:"user_id"`
	Provider    string `gorm:"size:32;uniqueIndex:idx_oauth_provider_uid" json:"provider"`
	ProviderUID string `gorm:"size:128;uniqueIndex:idx_oauth_provider_uid" json:"provider_uid"`
	CreatedAt   time.Time `json:"created_at"`
}

// Token 推送令牌（只存哈希，明文仅创建时返回一次）。
type Token struct {
	ID              uint64 `gorm:"primaryKey" json:"id"`
	UserID          uint64 `gorm:"index;not null" json:"user_id"`
	Name            string `gorm:"size:64" json:"name"`
	TokenHash       string `gorm:"size:64;uniqueIndex;not null" json:"-"`
	RateLimitPerMin int    `gorm:"not null;default:60" json:"rate_limit_per_min"` // 每分钟推送上限
	LastUsedAt      *time.Time `json:"last_used_at"`
	RevokedAt       *time.Time `json:"-"`
	CreatedAt       time.Time `json:"created_at"`
}

// Message 消息。
type Message struct {
	ID        uint64 `gorm:"primaryKey" json:"id"`
	TenantID  uint64 `gorm:"index;not null" json:"tenant_id"`
	UserID    uint64 `gorm:"index" json:"user_id"`
	Topic     string `gorm:"size:64" json:"topic"`
	Title     string `gorm:"size:128;not null" json:"title"`
	Body      string `gorm:"size:4096" json:"body"`
	Level     string `gorm:"size:16;not null;default:info" json:"level"`
	Public    bool   `gorm:"index" json:"public"`
	CreatedAt time.Time `json:"created_at"`
}

// VerifyCode 邮箱验证码（只存哈希）。
type VerifyCode struct {
	ID        uint64 `gorm:"primaryKey" json:"id"`
	Email     string `gorm:"size:255;index;not null" json:"email"`
	CodeHash  string `gorm:"size:64;not null" json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	UsedAt    *time.Time `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// TenantDTO 租户对外视图。
type TenantDTO struct {
	ID        uint64 `json:"id"`
	Name      string `json:"name"`
	UserCount int64  `json:"user_count,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// UserDTO 用户对外视图（不含敏感字段）。
type UserDTO struct {
	ID          uint64    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	Tenant      TenantRef `json:"tenant"`
	CreatedAt   time.Time `json:"created_at"`
}

// TenantRef 用户视图中的租户引用。
type TenantRef struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

// TokenDTO 令牌对外视图；Token 字段仅创建时返回一次。
type TokenDTO struct {
	ID              uint64 `json:"id"`
	Name            string `json:"name"`
	RateLimitPerMin int    `json:"rate_limit_per_min"`
	Token           string `json:"token,omitempty"`
	LastUsedAt      *time.Time `json:"last_used_at"`
	CreatedAt       time.Time `json:"created_at"`
}
