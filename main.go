// message-hub：消息接收与转发中间件（单二进制，内嵌 Web 管理台）。
//
// 核心能力：OAuth 登录、多租户、邮箱自注册、HTTPS 双向认证（不依赖 Nginx）、
// 令牌认证的消息推送 API（限流）、SSE 实时订阅、移动端客户端。
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/linuxsuren/message-hub/internal/auth"
	"github.com/linuxsuren/message-hub/internal/httpserver"
	"github.com/linuxsuren/message-hub/internal/store"
	"github.com/linuxsuren/message-hub/internal/version"
	"github.com/linuxsuren/message-hub/web"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP(S) 监听地址")
	dataDir := flag.String("data", "./data", "数据目录（SQLite、自签证书、会话密钥）")
	baseURL := flag.String("base-url", "", "对外基础 URL（OAuth 回调使用，例如 https://msg.example.com）")
	certPath := flag.String("cert", "", "TLS 证书 PEM 路径；文件不存在时自动生成自签证书")
	keyPath := flag.String("key", "", "TLS 私钥 PEM 路径（与 -cert 同时提供）")
	clientCA := flag.String("client-ca", "", "客户端 CA 证书 PEM，启用后要求双向认证（mTLS）")
	sessionSecret := flag.String("session-secret", "", "会话签名密钥；缺省自动生成并持久化到数据目录")
	registrationOpen := flag.Bool("registration-open", true, "是否开放邮箱自注册")

	smtpHost := flag.String("smtp-host", "", "SMTP 服务器地址（空则验证码输出到日志）")
	smtpPort := flag.Int("smtp-port", 587, "SMTP 端口（465 走隐式 TLS，其余 STARTTLS）")
	smtpUser := flag.String("smtp-username", "", "SMTP 用户名")
	smtpPass := flag.String("smtp-password", "", "SMTP 密码")
	smtpFrom := flag.String("smtp-from", "", "发件人地址（缺省用 SMTP 用户名）")

	githubID := flag.String("oauth-github-client-id", "", "GitHub OAuth Client ID")
	githubSecret := flag.String("oauth-github-client-secret", "", "GitHub OAuth Client Secret")
	githubAPI := flag.String("oauth-github-api", "", "GitHub API 覆盖地址（GitHub Enterprise 用）")
	giteeID := flag.String("oauth-gitee-client-id", "", "Gitee OAuth Client ID")
	giteeSecret := flag.String("oauth-gitee-client-secret", "", "Gitee OAuth Client Secret")
	giteeAPI := flag.String("oauth-gitee-api", "", "Gitee API 覆盖地址")

	showVersion := flag.Bool("version", false, "打印版本")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Version, version.Platform())
		return
	}
	if err := run(runOptions{
		addr: *addr, dataDir: *dataDir, baseURL: *baseURL,
		certPath: *certPath, keyPath: *keyPath, clientCA: *clientCA,
		sessionSecret: *sessionSecret, registrationOpen: *registrationOpen,
		smtp: &auth.Mailer{
			Host: *smtpHost, Port: *smtpPort,
			Username: *smtpUser, Password: *smtpPass, From: *smtpFrom,
		},
		githubID: *githubID, githubSecret: *githubSecret, githubAPI: *githubAPI,
		giteeID: *giteeID, giteeSecret: *giteeSecret, giteeAPI: *giteeAPI,
	}); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}

type runOptions struct {
	addr             string
	dataDir          string
	baseURL          string
	certPath         string
	keyPath          string
	clientCA         string
	sessionSecret    string
	registrationOpen bool
	smtp             *auth.Mailer
	githubID         string
	githubSecret     string
	githubAPI        string
	giteeID          string
	giteeSecret      string
	giteeAPI         string
}

func run(opts runOptions) error {
	if err := os.MkdirAll(opts.dataDir, 0o755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}
	db, err := store.Open(opts.dataDir)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	secret, err := loadOrCreateSecret(opts.dataDir, opts.sessionSecret)
	if err != nil {
		return err
	}
	baseURL := opts.baseURL
	if baseURL == "" {
		baseURL = guessBaseURL(opts.addr)
	}

	providers := map[string]auth.OAuthProvider{}
	if opts.githubID != "" && opts.githubSecret != "" {
		providers["github"] = auth.NewGitHubProvider(opts.githubID, opts.githubSecret, "", "", opts.githubAPI)
	}
	if opts.giteeID != "" && opts.giteeSecret != "" {
		providers["gitee"] = auth.NewGiteeProvider(opts.giteeID, opts.giteeSecret, "", "", opts.giteeAPI)
	}

	tlsConfig, err := httpserver.SetupTLS(opts.certPath, opts.keyPath, opts.clientCA)
	if err != nil {
		return err
	}

	webFS, err := fs.Sub(web.Dist(), "dist")
	if err != nil {
		return fmt.Errorf("内嵌前端缺失: %w", err)
	}
	srv := httpserver.New(db, httpserver.Config{
		BaseURL:          baseURL,
		SessionSecret:    secret,
		Providers:        providers,
		Mailer:           opts.smtp,
		RegistrationOpen: opts.registrationOpen,
		SecureCookie:     tlsConfig != nil,
		Version:          version.Version,
	}, webFS)

	server := &http.Server{
		Addr:              opts.addr,
		Handler:           withHSTS(srv.Handler()),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		TLSConfig:         tlsConfig,
	}

	scheme := "http"
	if tlsConfig != nil {
		scheme = "https"
	}
	log.Printf("message-hub %s 启动：%s://0.0.0.0%s（数据目录 %s，OAuth：%s）",
		version.Version, scheme, opts.addr, filepath.Clean(opts.dataDir), providerNames(providers))

	errCh := make(chan error, 1)
	go func() {
		if tlsConfig != nil {
			errCh <- server.ListenAndServeTLS("", "")
		} else {
			errCh <- server.ListenAndServe()
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	case sig := <-stop:
		log.Printf("收到信号 %v，正在退出……", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
	return nil
}

// loadOrCreateSecret 加载或持久化会话密钥（优先命令行显式指定）。
func loadOrCreateSecret(dataDir, explicit string) ([]byte, error) {
	if explicit != "" {
		return []byte(explicit), nil
	}
	path := filepath.Join(dataDir, "session_secret")
	if data, err := os.ReadFile(path); err == nil && len(data) >= 16 {
		return data, nil
	}
	secret, err := auth.GenerateSecret(32)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		return nil, fmt.Errorf("持久化会话密钥失败: %w", err)
	}
	return secret, nil
}

// guessBaseURL 无显式配置时推导回调地址（容器/内网场景建议显式配置）。
func guessBaseURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}

// withHSTS 在 HTTPS 响应上附加严格传输安全头。
func withHSTS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func providerNames(providers map[string]auth.OAuthProvider) string {
	if len(providers) == 0 {
		return "未配置"
	}
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}
