// Package store 负责数据库的打开、迁移与初始化。
package store

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/linuxsuren/message-hub/internal/model"
)

// DefaultTenant 默认租户名：自注册用户归属该租户。
const DefaultTenant = "default"

// Open 打开（必要时创建）数据目录下的 SQLite 数据库，完成迁移与默认租户初始化。
func Open(dataDir string) (*gorm.DB, error) {
	dsn := filepath.Join(dataDir, "message-hub.db") +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags),
			logger.Config{
				LogLevel:                  logger.Warn,
				IgnoreRecordNotFoundError: true,
			},
		),
	})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(
		&model.Tenant{}, &model.User{}, &model.OAuthIdentity{},
		&model.Token{}, &model.Message{}, &model.VerifyCode{},
	); err != nil {
		return nil, err
	}
	var count int64
	if err := db.Model(&model.Tenant{}).Where("name = ?", DefaultTenant).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		if err := db.Create(&model.Tenant{Name: DefaultTenant, CreatedAt: time.Now()}).Error; err != nil {
			return nil, err
		}
	}
	return db, nil
}

// DefaultTenantID 返回默认租户 ID。
func DefaultTenantID(db *gorm.DB) (uint64, error) {
	var t model.Tenant
	if err := db.Where("name = ?", DefaultTenant).First(&t).Error; err != nil {
		return 0, err
	}
	return t.ID, nil
}
