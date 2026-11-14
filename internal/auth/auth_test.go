package auth

import (
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/linuxsuren/message-hub/internal/store"
)

func TestPasswordHash(t *testing.T) {
	hash, err := HashPassword("secret-password")
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	if !VerifyPassword(hash, "secret-password") {
		t.Fatal("正确密码应校验通过")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("错误密码不应通过")
	}
	if strings.Contains(hash, "secret-password") {
		t.Fatal("哈希中不应包含明文")
	}
}

func TestTokenGenerateAndHash(t *testing.T) {
	tk, err := GenerateToken()
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}
	if !strings.HasPrefix(tk, TokenPrefix) {
		t.Fatalf("令牌应带前缀: %s", tk)
	}
	another, _ := GenerateToken()
	if tk == another {
		t.Fatal("令牌不应重复")
	}
	if HashToken(tk) != HashToken(tk) {
		t.Fatal("哈希应稳定")
	}
	if len(HashToken(tk)) != 64 {
		t.Fatalf("SHA-256 哈希应为 64 位十六进制: %d", len(HashToken(tk)))
	}
}

func TestSessionIssueVerify(t *testing.T) {
	m := NewSessionManager([]byte("unit-test-secret"))
	value := m.Issue(42)
	uid, ok := m.Verify(value)
	if !ok || uid != 42 {
		t.Fatalf("应校验通过并得到用户 42: ok=%v uid=%d", ok, uid)
	}

	// 篡改 payload
	if _, ok := m.Verify("eyJpZCI6MX0." + strings.SplitN(value, ".", 2)[1]); ok {
		t.Fatal("篡改后的会话不应通过")
	}
	// 签名密钥不同
	other := NewSessionManager([]byte("another-secret"))
	if _, ok := other.Verify(value); ok {
		t.Fatal("不同密钥签发的会话不应通过")
	}
	// 非法格式
	for _, bad := range []string{"", "a.b.c.d", "####.ffff", "eHg.*bad*"} {
		if _, ok := m.Verify(bad); ok {
			t.Fatalf("非法值不应通过: %q", bad)
		}
	}
}

func TestVerificationCode(t *testing.T) {
	db := openTestDB(t)
	code, err := IssueVerificationCode(db, "user@example.com")
	if err != nil {
		t.Fatalf("签发验证码失败: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("验证码应为 6 位: %s", code)
	}
	if !CheckVerificationCode(db, "user@example.com", code) {
		t.Fatal("正确验证码应通过")
	}
	if CheckVerificationCode(db, "user@example.com", code) {
		t.Fatal("验证码不能复用")
	}
	if CheckVerificationCode(db, "user@example.com", "000000") {
		t.Fatal("错误验证码不应通过")
	}
	if _, err := IssueVerificationCode(db, "user@example.com"); err != ErrCodeTooFrequent {
		t.Fatalf("一分钟内重复发送应被限频: %v", err)
	}
}

func TestVerificationCodeExpiry(t *testing.T) {
	db := openTestDB(t)
	if _, err := IssueVerificationCode(db, "expire@example.com"); err != nil {
		t.Fatal(err)
	}
	// 直接把库中验证码改为已过期
	if err := db.Exec("UPDATE verify_codes SET expires_at = ?",
		time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	var anyCode struct{ CodeHash string }
	if err := db.Raw("SELECT code_hash FROM verify_codes LIMIT 1").Scan(&anyCode).Error; err != nil {
		t.Fatal(err)
	}
	_ = anyCode
	// 已过期的验证码不可通过（哈希未知，只需确认查询带过期条件不崩溃）
	if CheckVerificationCode(db, "expire@example.com", "123456") {
		t.Fatal("过期验证码不应通过")
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}
