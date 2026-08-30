package anilist

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Account struct {
	ID         int
	Name       string
	AvatarURL  string
	ProfileURL string
}

// TokenExpiresAt reports the expiry encoded in an AniList JWT access token,
// or the zero time when the claim is absent or unparseable.
func TokenExpiresAt(accessToken string) time.Time {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.RawStdEncoding.DecodeString(parts[1])
		if err != nil {
			return time.Time{}
		}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}
	}
	if claims.Exp <= 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0).UTC()
}

func (c *Client) Viewer(ctx context.Context) (Account, error) {
	var response struct {
		Data struct {
			Viewer struct {
				ID     int    `json:"id"`
				Name   string `json:"name"`
				Avatar struct {
					Large string `json:"large"`
				} `json:"avatar"`
				SiteURL string `json:"siteUrl"`
			} `json:"Viewer"`
		} `json:"data"`
	}
	if err := c.do(ctx, `query { Viewer { id name avatar { large } siteUrl } }`, nil, &response); err != nil {
		return Account{}, err
	}
	if response.Data.Viewer.ID == 0 || response.Data.Viewer.Name == "" {
		return Account{}, fmt.Errorf("AniList Viewer response is incomplete")
	}
	return Account{
		ID:         response.Data.Viewer.ID,
		Name:       response.Data.Viewer.Name,
		AvatarURL:  response.Data.Viewer.Avatar.Large,
		ProfileURL: response.Data.Viewer.SiteURL,
	}, nil
}
