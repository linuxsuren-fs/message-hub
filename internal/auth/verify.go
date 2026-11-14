package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"gorm.io/gorm"

	"github.com/linuxsuren/message-hub/internal/model"
)

// ErrCodeTooFrequent 验证码发送过于频繁。
var ErrCodeTooFrequent = errors.New("发送过于频繁，请 1 分钟后再试")

// codeTTL 验证码有效期。
const codeTTL = 10 * time.Minute

// IssueVerificationCode 为邮箱生成 6 位数字验证码（60 秒内不可重复发送）。
func IssueVerificationCode(db *gorm.DB, email string) (string, error) {
	var recent model.VerifyCode
	err := db.Where("email = ? AND created_at > ?", email, time.Now().Add(-time.Minute)).
		First(&recent).Error
	if err == nil {
		return "", ErrCodeTooFrequent
	}
	if err != gorm.ErrRecordNotFound {
		return "", err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	now := time.Now()
	if err := db.Where("email = ?", email).Delete(&model.VerifyCode{}).Error; err != nil {
		return "", err
	}
	if err := db.Create(&model.VerifyCode{
		Email: email, CodeHash: HashToken(code), ExpiresAt: now.Add(codeTTL), CreatedAt: now,
	}).Error; err != nil {
		return "", err
	}
	return code, nil
}

// CheckVerificationCode 校验并消费验证码。
func CheckVerificationCode(db *gorm.DB, email, code string) bool {
	var rec model.VerifyCode
	err := db.Where("email = ? AND code_hash = ? AND used_at IS NULL AND expires_at > ?",
		email, HashToken(code), time.Now()).First(&rec).Error
	if err != nil {
		return false
	}
	now := time.Now()
	return db.Model(&rec).Update("used_at", now).Error == nil
}
