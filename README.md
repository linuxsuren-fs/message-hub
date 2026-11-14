# message-hub

<p align="center">
  <img src="logo.svg" width="160" alt="message-hub logo" />
</p>

[![CI](https://github.com/LinuxSuRen/message-hub/actions/workflows/ci.yml/badge.svg)](https://github.com/LinuxSuRen/message-hub/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

自托管的**消息接收与转发中间件**（Golang + Vue）：服务端程序通过 API 推送消息进来，Web 管理台与移动端 App 实时订阅属于自己的（或公开的）消息。

单二进制一把梭：Go 单文件内嵌 Web 管理台，SQLite 存数据，无外部依赖；也提供容器镜像。实现 #1。

- **OAuth 登录**：GitHub、Gitee 授权登录（可配 API 地址适配企业版）
- **多租户**：租户隔离的消息可见性，平台管理员 / 租户管理员 / 用户三级角色
- **用户邮件自注册**：邮箱验证码（SMTP 可配，未配置时验证码输出到日志）
- **HTTPS 双向认证**：内置 TLS（证书缺失自动生成自签），`-client-ca` 一键开启 mTLS，**不依赖 Nginx**
- **API 推送消息**：`POST /api/v1/messages` 令牌（Bearer）认证，**令牌级流量限制**（每分钟配额可配）
- **移动端订阅**：SSE 实时推送本人租户与公开消息
  - 安卓客户端：[LinuxSuRen/message-android](https://github.com/LinuxSuRen/message-android)
  - 鸿蒙客户端：[LinuxSuRen/message-harmony](https://github.com/LinuxSuRen/message-harmony)
- **单二进制启动、容器启动**

## 快速开始

二进制（[Releases](https://github.com/LinuxSuRen/message-hub/releases) 提供 linux/windows/darwin × amd64/arm64）：

```bash
./message-hub -addr :8080 -data ./data
# 浏览器打开 http://localhost:8080，注册的第一个用户自动成为平台管理员
```

源码构建：

```bash
make && ./message-hub
# 等价于：cd web && npm install && npm run build && cd .. && go build .
```

Docker 部署：

```bash
docker compose up -d --build
```

或：

```bash
docker run -d -p 8080:8080 -v ./data:/data ghcr.io/linuxsuren/message-hub:latest
```

## 使用流程

1. 打开 Web 管理台，注册首个账号（自动成为**平台管理员**，请部署后立即完成）
2. 管理台 →「推送令牌」创建令牌（可设每分钟推送上限），令牌明文只显示一次
3. 服务端程序携带令牌推送消息：

```bash
curl -H "Authorization: Bearer mh_xxxxxxxx" \
     -H "Content-Type: application/json" \
     -d '{"title":"构建失败","body":"pipeline #42 failed","level":"error","topic":"ci"}' \
     https://your-host/api/v1/messages
```

4. Web 管理台或移动端 App 登录后即可实时收到本人租户与公开消息

### 多租户与角色

- **平台管理员**：管理全部租户与用户、任命租户管理员
- **租户管理员**：管理本租户用户，可发送**公开消息**（全员可见）
- **用户**：管理自己的令牌，收发本租户消息

### HTTPS 与双向认证（mTLS）

```bash
# 未提供证书时自动生成自签证书（适合内网/自签 CA 环境）
./message-hub -addr :8443 -cert ./certs/server.crt -key ./certs/server.key

# 提供客户端 CA 后强制双向认证：没有客户端证书连不上（不依赖 Nginx）
./message-hub -addr :8443 -cert ./certs/server.crt -key ./certs/server.key \
    -client-ca ./certs/ca.crt -base-url https://msg.example.com
```

## 配置项

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `-addr` | `:8080` | 监听地址 |
| `-data` | `./data` | 数据目录（SQLite、自签证书、会话密钥） |
| `-cert` / `-key` | 空 | TLS 证书/私钥；同时提供即启用 HTTPS，文件缺失自动生成自签 |
| `-client-ca` | 空 | 客户端 CA 证书，提供后启用双向认证 |
| `-base-url` | 自动推导 | 对外基础 URL（OAuth 回调、跳转） |
| `-session-secret` | 自动生成并持久化 | 会话签名密钥 |
| `-registration-open` | `true` | 是否开放邮箱自注册 |
| `-smtp-host` `-smtp-port` `-smtp-username` `-smtp-password` `-smtp-from` | 空 | SMTP 邮件（验证码）；未配置时验证码输出到服务日志 |
| `-oauth-github-client-id` `-oauth-github-client-secret` `-oauth-github-api` | 空 | GitHub OAuth（api 可覆盖适配企业版） |
| `-oauth-gitee-client-id` `-oauth-gitee-client-secret` `-oauth-gitee-api` | 空 | Gitee OAuth |

## REST API

所有接口 JSON；错误响应统一为 `{"error":"原因"}`。认证方式：浏览器会话 Cookie（登录获得）或 `Authorization: Bearer <令牌>`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/health` | 健康检查 |
| GET | `/api/v1/config` | 公开配置（可用的 OAuth 提供方等） |
| POST | `/api/v1/auth/verification-code` | 发送注册验证码 `{"email"}` |
| POST | `/api/v1/auth/register` | 注册 `{"email","password","code"}` |
| POST | `/api/v1/auth/login` | 登录 `{"email","password"}` |
| POST | `/api/v1/auth/logout` | 登出 |
| GET | `/api/v1/auth/me` | 当前用户 |
| GET | `/api/v1/auth/oauth/{provider}/login` | OAuth 登录跳转（github / gitee） |
| GET | `/api/v1/auth/oauth/{provider}/callback` | OAuth 回调 |
| POST | `/api/v1/messages` | 推送消息（令牌认证，按令牌限流）`{"title","body","topic","level","public"}` |
| GET | `/api/v1/messages?limit=&cursor=&scope=all\|own\|public` | 查询消息（本人租户 + 公开；id 降序，游标翻页） |
| GET | `/api/v1/stream` | SSE 实时订阅：`event: message` 推消息、`event: heartbeat` 保活（约 20s） |
| GET/POST | `/api/v1/tokens` | 令牌列表 / 创建（明文仅创建时返回） |
| DELETE | `/api/v1/tokens/{id}` | 吊销令牌 |
| GET/POST/PUT/DELETE | `/api/v1/tenants` | 租户管理（平台管理员） |
| GET/POST/PUT/DELETE | `/api/v1/users` | 用户管理（平台/租户管理员） |
| GET | `/api/v1/stats` | 概览统计（平台管理员） |

SSE 订阅示例：

```bash
curl -N -H "Authorization: Bearer mh_xxxxxxxx" https://your-host/api/v1/stream
```

```
retry: 5000

event: message
data: {"id":1,"tenant_id":1,"topic":"ci","title":"构建失败","body":"pipeline #42 failed","level":"error","public":false,"created_at":"2026-11-14T10:00:00Z"}
```

消息推送（令牌认证）受令牌级限流保护，超限返回 `429` 与 `Retry-After: 1`。

## 安全模型

- 密码 bcrypt 哈希存储；推送令牌与验证码只存 SHA-256 哈希，令牌明文仅创建时返回一次
- 会话为 HMAC-SHA256 签名的无状态 Cookie（HttpOnly / SameSite=Lax / HTTPS 下 Secure）
- 租户隔离在查询层强制：任何人无法读取其他租户的私有消息；公开消息仅管理员角色可发
- mTLS 场景下没有客户端证书的连接在 TLS 握手阶段即被拒绝

## 开发

```bash
make web test   # 前端构建 + 全量测试
cd web && npm run dev   # 前端开发（代理到 127.0.0.1:8080）
```

- 后端：Go 1.26 + [gorm](https://gorm.io/) + 纯 Go SQLite（[glebarez/sqlite](https://github.com/glebarez/sqlite)，CGO 关闭）
- 前端：Vue 3 + [Element Plus](https://element-plus.org/)，构建产物经 `go:embed` 打进单二进制
- 打 tag（`v*`）自动发布：goreleaser 多平台二进制 + GHCR 多架构镜像（[release.yml](.github/workflows/release.yml)）

## License

[Apache-2.0](LICENSE)
