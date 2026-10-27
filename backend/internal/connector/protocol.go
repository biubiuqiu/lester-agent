package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 2 << 20

func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if n, ok := v.(json.Number); ok {
		return n.String()
	}
	return ""
}
func number(v any) int64 { n, _ := strconv.ParseInt(text(v), 10, 64); return n }
func items(v any) []any  { a, _ := v.([]any); return a }
func splitScopes(v any) []string {
	result := strings.FieldsFunc(text(v), func(r rune) bool { return r == ' ' || r == ',' })
	if result == nil {
		return []string{}
	}
	return result
}
func hasScope(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// This never follows Location, logs response bodies, or returns OAuth responses
// to callers. Resource adapters return only selected, normalized public fields.
func (s *Service) request(ctx context.Context, method, target, access string, body io.Reader, contentType string, basic *Provider) (any, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, ErrProvider
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Lester-Agent/1.0")
	if access != "" {
		req.Header.Set("Authorization", "Bearer "+access)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if basic != nil {
		req.SetBasicAuth(basic.ClientID, basic.ClientSecret)
	}
	if strings.HasPrefix(target, "https://api.github.com/") {
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	response, err := s.client.Do(req)
	if err != nil {
		return nil, ErrProvider
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case 401:
		return nil, ErrReauthorize
	case 403:
		if limitedByHeaders(response.Header) {
			return nil, ErrRateLimit
		}
		return nil, ErrPermission
	case 402:
		return nil, ErrPermission
	case 429:
		return nil, ErrRateLimit
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, ErrProvider
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, ErrProvider
	}
	if len(raw) > maxResponseBytes {
		return nil, ErrResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrResponse
	}
	m := object(value)
	if ok, exists := m["ok"].(bool); exists && !ok {
		switch text(m["error"]) {
		case "invalid_auth", "token_revoked", "token_expired", "account_inactive", "invalid_grant", "invalid_refresh_token":
			return nil, ErrReauthorize
		case "missing_scope", "not_allowed_token_type", "not_in_channel", "restricted_action":
			return nil, ErrPermission
		case "ratelimited":
			return nil, ErrRateLimit
		default:
			return nil, ErrProvider
		}
	}
	if code := number(m["code"]); code != 0 {
		switch code {
		case 99991663, 99991664, 99991668, 99991671, 99991672:
			return nil, ErrReauthorize
		case 99991669, 99991679, 99991680:
			return nil, ErrPermission
		default:
			return nil, ErrProvider
		}
	}
	if text(m["error"]) != "" {
		return nil, ErrReauthorize
	}
	return value, nil
}

func limitedByHeaders(headers http.Header) bool {
	for _, key := range []string{"X-RateLimit-Remaining", "RateLimit-Remaining"} {
		if strings.TrimSpace(headers.Get(key)) == "0" {
			return true
		}
	}
	if retry := strings.TrimSpace(headers.Get("Retry-After")); retry != "" {
		if _, err := strconv.ParseUint(retry, 10, 64); err == nil {
			return true
		}
		if _, err := http.ParseTime(retry); err == nil {
			return true
		}
	}
	return false
}

func (s *Service) get(ctx context.Context, p Provider, access, path string, values url.Values) (map[string]any, error) {
	u := strings.TrimRight(p.APIURL, "/") + path
	if len(values) > 0 {
		u += "?" + values.Encode()
	}
	value, err := s.request(ctx, http.MethodGet, u, access, nil, "", nil)
	if err != nil {
		return nil, err
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, ErrResponse
	}
	m := object(value)
	if p.Name == "slack" && m["ok"] != true {
		return nil, ErrResponse
	}
	if p.Name == "feishu" {
		if m["code"] == nil {
			return nil, ErrResponse
		}
		if _, ok := m["data"].(map[string]any); !ok {
			return nil, ErrResponse
		}
	}
	return m, nil
}
func (s *Service) exchange(ctx context.Context, p Provider, code, verifier, refresh string) (token, error) {
	values := url.Values{"client_id": {p.ClientID}, "client_secret": {p.ClientSecret}}
	if refresh != "" {
		values.Set("grant_type", "refresh_token")
		values.Set("refresh_token", refresh)
	} else {
		values.Set("grant_type", "authorization_code")
		values.Set("code", code)
		values.Set("redirect_uri", s.callbackURL(p.Name))
		if p.PKCE {
			values.Set("code_verifier", verifier)
		}
	}
	var body io.Reader = strings.NewReader(values.Encode())
	contentType := "application/x-www-form-urlencoded"
	var basic *Provider
	if p.Name == "x" || p.Name == "slack" {
		values.Del("client_secret")
		values.Del("client_id")
		basic = &p
		body = strings.NewReader(values.Encode())
	}
	if p.Name == "feishu" {
		payload := map[string]string{}
		for key, v := range values {
			payload[key] = v[0]
		}
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
		contentType = "application/json"
	}
	value, err := s.request(ctx, http.MethodPost, p.TokenURL, "", body, contentType, basic)
	if err != nil {
		return token{}, err
	}
	m := object(value)
	if p.Name == "slack" {
		user := object(m["authed_user"])
		if text(user["access_token"]) != "" {
			m = user
		} else if refresh == "" || text(m["token_type"]) != "user" {
			return token{}, ErrProvider
		}
	}
	t := token{Access: text(m["access_token"]), Refresh: text(m["refresh_token"]), Scopes: splitScopes(m["scope"])}
	if len(t.Access) < 8 || len(t.Access) > 16000 || len(t.Refresh) > 16000 {
		return token{}, ErrProvider
	}
	if len(t.Scopes) > 64 {
		return token{}, ErrResponse
	}
	for _, scope := range t.Scopes {
		if len(scope) > 256 {
			return token{}, ErrResponse
		}
	}
	seconds := number(m["expires_in"])
	if seconds < 0 || seconds > 365*24*3600 {
		return token{}, ErrProvider
	}
	if seconds > 0 {
		t.Expires = time.Now().Add(time.Duration(seconds) * time.Second)
	}
	if refresh == "" {
		switch p.Name {
		case "gmail":
			if !hasScope(t.Scopes, "https://www.googleapis.com/auth/gmail.readonly") {
				return token{}, ErrPermission
			}
		case "slack":
			if !hasScope(t.Scopes, "search:read") {
				return token{}, ErrPermission
			}
		case "x":
			if !hasScope(t.Scopes, "tweet.read") || !hasScope(t.Scopes, "users.read") {
				return token{}, ErrPermission
			}
		case "feishu":
			if len(t.Scopes) == 0 {
				return token{}, ErrPermission
			}
		}
	}
	return t, nil
}
func (s *Service) fetchIdentity(ctx context.Context, p Provider, access string) (identity, error) {
	value, err := s.request(ctx, http.MethodGet, p.ProfileURL, access, nil, "", nil)
	if err != nil {
		return identity{}, err
	}
	m := object(value)
	var id, name string
	switch p.Name {
	case "github":
		id = text(m["id"])
		name = text(m["login"])
	case "gmail":
		id = text(m["sub"])
		name = text(m["email"])
	case "x":
		data := object(m["data"])
		id = text(data["id"])
		name = "@" + text(data["username"])
	case "slack":
		id = text(m["team_id"]) + ":" + text(m["user_id"])
		name = text(m["team"]) + " · " + text(m["user"])
		if text(m["team_id"]) == "" || text(m["user_id"]) == "" {
			id = ""
		}
	case "feishu":
		data := object(m["data"])
		id = text(data["open_id"])
		name = text(data["name"])
		if id != "" {
			if tenant := text(data["tenant_key"]); tenant != "" {
				id = tenant + ":" + id
			}
		}
	}
	if strings.TrimSpace(id) == "" || len(id) > 512 || strings.TrimSpace(name) == "" || len(name) > 2048 {
		return identity{}, ErrProvider
	}
	return identity{ID: id, Name: name}, nil
}
