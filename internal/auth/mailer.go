package auth

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// Mailer SMTP 邮件发送器；Host 为空表示未配置。
type Mailer struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// Configured 是否已配置 SMTP。
func (m *Mailer) Configured() bool {
	return m != nil && m.Host != ""
}

func (m *Mailer) addr() string {
	return net.JoinHostPort(m.Host, fmt.Sprintf("%d", m.Port))
}

// SendCode 发送注册验证码邮件。465 端口走隐式 TLS，其余端口走 STARTTLS（服务器支持时）。
func (m *Mailer) SendCode(to, code string) error {
	if !m.Configured() {
		return fmt.Errorf("SMTP 未配置")
	}
	from := m.From
	if from == "" {
		from = m.Username
	}
	subject := mimeEncode("message-hub 注册验证码")
	body := strings.Join([]string{
		"您好，",
		"",
		"您正在注册 message-hub 账号，验证码为：",
		"",
		"    " + code,
		"",
		"验证码 10 分钟内有效。若非本人操作，请忽略本邮件。",
	}, "\r\n")
	msg := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
	}, "\r\n")

	var auth smtp.Auth
	if m.Username != "" {
		auth = smtp.PlainAuth("", m.Username, m.Password, m.Host)
	}
	if m.Port == 465 {
		return sendImplicitTLS(m.addr(), auth, from, to, msg, m.Host)
	}
	return smtp.SendMail(m.addr(), auth, from, []string{to}, []byte(msg))
}

func sendImplicitTLS(addr string, auth smtp.Auth, from, to, msg, host string) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if auth != nil {
		if err = client.Auth(auth); err != nil {
			return err
		}
	}
	if err = client.Mail(from); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write([]byte(msg)); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// mimeEncode 将非 ASCII 文本编码为 MIME B 编码，保证邮件头合法。
func mimeEncode(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
		}
	}
	return s
}
