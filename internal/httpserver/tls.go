package httpserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"time"
)

// SetupTLS 组装服务端 TLS 配置：
//   - certPath/keyPath 文件存在则加载；不存在则生成自签证书并写回文件（开箱即用 HTTPS）；
//   - clientCAPath 非空时启用双向认证（mTLS）：要求并校验客户端证书，无需 Nginx。
//
// 两者皆空返回 nil（以 HTTP 启动，适合反向代理后或内网调试）。
func SetupTLS(certPath, keyPath, clientCAPath string) (*tls.Config, error) {
	if certPath == "" && keyPath == "" {
		if clientCAPath != "" {
			return nil, errors.New("启用双向认证需要同时提供 -cert 与 -key")
		}
		return nil, nil
	}
	if certPath == "" || keyPath == "" {
		return nil, errors.New("-cert 与 -key 必须同时提供")
	}
	var cert tls.Certificate
	var err error
	if fileExists(certPath) && fileExists(keyPath) {
		cert, err = tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("加载 TLS 证书失败: %w", err)
		}
	} else {
		cert, err = selfSignedCert()
		if err != nil {
			return nil, fmt.Errorf("生成自签证书失败: %w", err)
		}
		if err := writeKeyPair(certPath, keyPath, cert); err != nil {
			return nil, err
		}
		log.Printf("已生成自签名证书 %s（生产环境请替换为受信证书）", certPath)
	}
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if clientCAPath != "" {
		pemBytes, err := os.ReadFile(clientCAPath)
		if err != nil {
			return nil, fmt.Errorf("读取客户端 CA 失败: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, errors.New("客户端 CA 证书解析失败（PEM 格式错误）")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
		log.Printf("HTTPS 双向认证（mTLS）已启用，客户端 CA: %s", clientCAPath)
	}
	return cfg, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func selfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	host, _ := os.Hostname()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "message-hub"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{host, "localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

func writeKeyPair(certPath, keyPath string, cert tls.Certificate) error {
	certOut := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
	if err := os.WriteFile(certPath, certOut, 0o644); err != nil {
		return fmt.Errorf("写入证书失败: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(cert.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		return err
	}
	keyOut := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyOut, 0o600); err != nil {
		return fmt.Errorf("写入私钥失败: %w", err)
	}
	return nil
}
