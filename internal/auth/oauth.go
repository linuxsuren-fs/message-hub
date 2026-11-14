package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OAuthUser 第三方返回的用户身份。
type OAuthUser struct {
	UID   string
	Login string
	Name  string
	Email string
}

// OAuthProvider 抽象一个 OAuth2 提供方（授权码流程）。
type OAuthProvider interface {
	Name() string
	AuthCodeURL(redirectURI, state string) string
	Exchange(ctx context.Context, code, redirectURI string) (string, error)
	FetchUser(ctx context.Context, accessToken string) (OAuthUser, error)
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// GitHubProvider GitHub OAuth（AuthorizeURL/TokenURL/APIURL 可覆盖以适配企业版）。
type GitHubProvider struct {
	ClientID     string
	ClientSecret string
	AuthorizeURL string
	TokenURL     string
	APIURL       string
}

// NewGitHubProvider 创建 GitHub 提供方，空串使用官方默认地址。
func NewGitHubProvider(clientID, clientSecret, authorizeURL, tokenURL, apiURL string) *GitHubProvider {
	p := &GitHubProvider{
		ClientID: clientID, ClientSecret: clientSecret,
		AuthorizeURL: authorizeURL, TokenURL: tokenURL, APIURL: apiURL,
	}
	if p.AuthorizeURL == "" {
		p.AuthorizeURL = "https://github.com/login/oauth/authorize"
	}
	if p.TokenURL == "" {
		p.TokenURL = "https://github.com/login/oauth/access_token"
	}
	if p.APIURL == "" {
		p.APIURL = "https://api.github.com"
	}
	return p
}

// Name 实现OAuthProvider。
func (p *GitHubProvider) Name() string { return "github" }

// AuthCodeURL 实现OAuthProvider。
func (p *GitHubProvider) AuthCodeURL(redirectURI, state string) string {
	q := url.Values{}
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "read:user user:email")
	q.Set("state", state)
	return p.AuthorizeURL + "?" + q.Encode()
}

// Exchange 实现OAuthProvider。
func (p *GitHubProvider) Exchange(ctx context.Context, code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("client_id", p.ClientID)
	form.Set("client_secret", p.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	return exchangeToken(ctx, p.TokenURL, form)
}

// FetchUser 实现OAuthProvider。
func (p *GitHubProvider) FetchUser(ctx context.Context, accessToken string) (OAuthUser, error) {
	body, err := getJSON(ctx, p.APIURL+"/user", accessToken)
	if err != nil {
		return OAuthUser{}, err
	}
	var user OAuthUser
	user.UID = jsonFloat(body["id"])
	user.Login, _ = body["login"].(string)
	user.Name, _ = body["name"].(string)
	user.Email, _ = body["email"].(string)
	if user.Email == "" { // 私有邮箱时查 emails 接口
		emails, err := getJSON(ctx, p.APIURL+"/user/emails", accessToken)
		if err == nil {
			if arr, ok := emails["__array__"].([]any); ok {
				for _, it := range arr {
					if m, ok := it.(map[string]any); ok {
						primary, _ := m["primary"].(bool)
						verified, _ := m["verified"].(bool)
						email, _ := m["email"].(string)
						if primary && verified && email != "" {
							user.Email = email
							break
						}
					}
				}
			}
		}
	}
	if user.UID == "" {
		return OAuthUser{}, fmt.Errorf("无法获取 GitHub 用户信息")
	}
	return user, nil
}

// GiteeProvider Gitee OAuth。
type GiteeProvider struct {
	ClientID     string
	ClientSecret string
	AuthorizeURL string
	TokenURL     string
	APIURL       string
}

// NewGiteeProvider 创建 Gitee 提供方。
func NewGiteeProvider(clientID, clientSecret, authorizeURL, tokenURL, apiURL string) *GiteeProvider {
	p := &GiteeProvider{
		ClientID: clientID, ClientSecret: clientSecret,
		AuthorizeURL: authorizeURL, TokenURL: tokenURL, APIURL: apiURL,
	}
	if p.AuthorizeURL == "" {
		p.AuthorizeURL = "https://gitee.com/oauth/authorize"
	}
	if p.TokenURL == "" {
		p.TokenURL = "https://gitee.com/oauth/token"
	}
	if p.APIURL == "" {
		p.APIURL = "https://gitee.com/api/v5"
	}
	return p
}

// Name 实现OAuthProvider。
func (p *GiteeProvider) Name() string { return "gitee" }

// AuthCodeURL 实现OAuthProvider。
func (p *GiteeProvider) AuthCodeURL(redirectURI, state string) string {
	q := url.Values{}
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "user_info")
	q.Set("state", state)
	q.Set("response_type", "code")
	return p.AuthorizeURL + "?" + q.Encode()
}

// Exchange 实现OAuthProvider。
func (p *GiteeProvider) Exchange(ctx context.Context, code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", p.ClientID)
	form.Set("client_secret", p.ClientSecret)
	form.Set("redirect_uri", redirectURI)
	return exchangeToken(ctx, p.TokenURL, form)
}

// FetchUser 实现OAuthProvider。
func (p *GiteeProvider) FetchUser(ctx context.Context, accessToken string) (OAuthUser, error) {
	body, err := getJSON(ctx, p.APIURL+"/user", accessToken)
	if err != nil {
		return OAuthUser{}, err
	}
	var user OAuthUser
	user.UID = jsonFloat(body["id"])
	user.Login, _ = body["login"].(string)
	user.Name, _ = body["name"].(string)
	user.Email, _ = body["email"].(string)
	if user.UID == "" {
		return OAuthUser{}, fmt.Errorf("无法获取 Gitee 用户信息")
	}
	return user, nil
}

func exchangeToken(ctx context.Context, tokenURL string, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("换取访问令牌失败：%d", resp.StatusCode)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return "", err
	}
	token, _ := m["access_token"].(string)
	if token == "" {
		return "", fmt.Errorf("响应中没有访问令牌")
	}
	return token, nil
}

// getJSON 发起带 Bearer 的 GET 请求；数组响应包装为 {"__array__": [...]}。
func getJSON(ctx context.Context, uri, accessToken string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("请求 %s 失败：%d", uri, resp.StatusCode)
	}
	m := map[string]any{}
	if err := json.Unmarshal(body, &m); err == nil {
		return m, nil
	}
	arr := []any{}
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, err
	}
	return map[string]any{"__array__": arr}, nil
}

// jsonFloat 把 JSON 数字统一转成十进制字符串。
func jsonFloat(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case string:
		return n
	default:
		return ""
	}
}
