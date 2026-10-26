package connector

import "strings"

// Endpoints and read scopes are compiled in. Deployment inputs contain client
// identities only; a model/browser cannot select an API host or OAuth scope.
func DefaultProviders(clients map[string][2]string) []Provider {
	providers := []Provider{
		{Name: "github", AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token", APIURL: "https://api.github.com", ProfileURL: "https://api.github.com/user", PKCE: true},
		{Name: "gmail", AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", APIURL: "https://gmail.googleapis.com/gmail/v1/users/me", ProfileURL: "https://openidconnect.googleapis.com/v1/userinfo", Scopes: []string{"openid", "email", "https://www.googleapis.com/auth/gmail.readonly"}, PKCE: true},
		{Name: "x", AuthURL: "https://x.com/i/oauth2/authorize", TokenURL: "https://api.x.com/2/oauth2/token", APIURL: "https://api.x.com/2", ProfileURL: "https://api.x.com/2/users/me", Scopes: []string{"tweet.read", "users.read", "offline.access"}, PKCE: true},
		{Name: "feishu", AuthURL: "https://accounts.feishu.cn/open-apis/authen/v1/authorize", TokenURL: "https://open.feishu.cn/open-apis/authen/v2/oauth/token", APIURL: "https://open.feishu.cn/open-apis", ProfileURL: "https://open.feishu.cn/open-apis/authen/v1/user_info", Scopes: []string{"offline_access", "contact:user.base:readonly", "im:chat:readonly", "im:message:readonly", "docx:document:readonly"}, PKCE: true},
		{Name: "slack", AuthURL: "https://slack.com/oauth/v2/authorize", TokenURL: "https://slack.com/api/oauth.v2.access", APIURL: "https://slack.com/api", ProfileURL: "https://slack.com/api/auth.test", Scopes: []string{"search:read", "channels:history", "groups:history", "im:history", "mpim:history"}},
	}
	for i := range providers {
		credentials := clients[providers[i].Name]
		providers[i].ClientID = strings.TrimSpace(credentials[0])
		providers[i].ClientSecret = credentials[1]
	}
	return providers
}
