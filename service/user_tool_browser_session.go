package service

import (
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const UserToolAccessCookieName = "new_api_user_tool_access"

var ErrUserToolBrowserSessionInvalid = errors.New("invalid user tool browser session")

func UserToolAccessPath(tool string) (string, bool) {
	switch tool {
	case model.UserToolImagePlayground:
		return "/_tools/gpt-image-playground", true
	case model.UserToolInfiniteCanvas:
		return "/_tools/infinite-canvas", true
	default:
		return "", false
	}
}

func IssueUserToolBrowserSession(c *gin.Context, identity AuthIdentity, tool string) (int64, error) {
	cookiePath, ok := UserToolAccessPath(tool)
	if !ok {
		return 0, ErrUserToolBrowserSessionInvalid
	}
	accessToken, expiresAt, err := IssueAccessToken(identity)
	if err != nil {
		return 0, err
	}
	expiresAtTime := time.Unix(expiresAt, 0)
	maxAge := int(time.Until(expiresAtTime) / time.Second)
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     UserToolAccessCookieName,
		Value:    accessToken,
		Path:     cookiePath,
		MaxAge:   maxAge,
		Expires:  expiresAtTime,
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	return expiresAt, nil
}
