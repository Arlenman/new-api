package controller

import (
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type pixelFlowTokenPayload struct {
	ID                 int    `json:"id"`
	Key                string `json:"key"`
	ModelLimits        string `json:"model_limits"`
	ModelLimitsEnabled bool   `json:"model_limits_enabled"`
	Name               string `json:"name"`
	Status             int    `json:"status"`
}

type pixelFlowSessionSyncPayload struct {
	Binding struct {
		BaseURL       string `json:"baseUrl"`
		SessionCookie string `json:"sessionCookie"`
		UserID        int    `json:"userId"`
		Username      string `json:"username"`
	} `json:"binding"`
	Tokens []pixelFlowTokenPayload `json:"tokens"`
}

type pixelFlowSessionSyncMessage struct {
	Type    string                      `json:"type"`
	Origin  string                      `json:"origin"`
	Payload pixelFlowSessionSyncPayload `json:"payload"`
}

func getAllowedPixelFlowOrigins() map[string]bool {
	allowed := map[string]bool{
		"http://127.0.0.1:3000": true,
		"http://127.0.0.1:3030": true,
		"http://localhost":      true,
		"http://localhost:3000": true,
		"http://localhost:3030": true,
	}

	for _, origin := range strings.Split(os.Getenv("PIXELFLOW_ALLOWED_ORIGINS"), ",") {
		trimmed := strings.TrimSpace(origin)
		if trimmed != "" {
			allowed[strings.TrimRight(trimmed, "/")] = true
		}
	}

	return allowed
}

func normalizePixelFlowOrigin(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}

	return parsed.Scheme + "://" + parsed.Host, true
}

func PixelFlowSessionTokenSync(c *gin.Context) {
	origin, ok := normalizePixelFlowOrigin(c.Query("origin"))
	if !ok || !getAllowedPixelFlowOrigins()[origin] {
		c.String(http.StatusForbidden, "不允许同步到该站点")
		return
	}
	originBytes, err := common.Marshal(origin)
	if err != nil {
		c.String(http.StatusInternalServerError, "生成 PixelFlow 授权数据失败")
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <title>PixelFlow 授权同步</title>
</head>
<body>
  <p id="status">正在同步 PixelFlow 密钥，请稍候。</p>
  <script>
    const targetOrigin = %s;
    async function syncPixelFlow() {
      const refreshResponse = await fetch('/api/user/auth/refresh', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { Accept: 'application/json' },
      });
      const refreshBody = await refreshResponse.json().catch(() => null);
      const auth = refreshBody && refreshBody.data;
      if (!refreshResponse.ok || refreshBody.success !== true || !auth || !auth.access_token) {
        throw new Error('请先登录 NewAPI');
      }

      const headers = {
        Accept: 'application/json',
        Authorization: 'Bearer ' + auth.access_token,
      };
      if (auth.session && auth.session.sid) {
        headers['X-Auth-Session'] = auth.session.sid;
      }
      const dataResponse = await fetch(
        '/api/pixelflow/session-token-sync/data?origin=' + encodeURIComponent(targetOrigin),
        { credentials: 'same-origin', headers },
      );
      const message = await dataResponse.json().catch(() => null);
      if (!dataResponse.ok || !message) {
        throw new Error('同步 PixelFlow 密钥失败');
      }
      if (window.opener) {
        window.opener.postMessage(message, targetOrigin);
        window.close();
      }
    }

    syncPixelFlow().catch((error) => {
      document.getElementById('status').textContent = error.message || '同步 PixelFlow 密钥失败';
    });
  </script>
</body>
</html>`, string(originBytes))
}

func PixelFlowSessionTokenSyncData(c *gin.Context) {
	origin, ok := normalizePixelFlowOrigin(c.Query("origin"))
	if !ok || !getAllowedPixelFlowOrigins()[origin] {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "不允许同步到该站点"})
		return
	}

	userID := c.GetInt("id")
	if userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "请先登录 NewAPI"})
		return
	}
	user, err := model.GetUserById(userID, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "当前 NewAPI 用户不可用"})
		return
	}

	tokens, err := model.GetAllUserTokens(userID, 0, 100)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	payload := pixelFlowSessionSyncPayload{
		Tokens: make([]pixelFlowTokenPayload, 0, len(tokens)),
	}
	payload.Binding.BaseURL = "http"
	if c.Request.TLS != nil {
		payload.Binding.BaseURL = "https"
	}
	payload.Binding.BaseURL += "://" + c.Request.Host
	payload.Binding.SessionCookie = ""
	payload.Binding.UserID = userID
	payload.Binding.Username = user.Username
	if payload.Binding.Username == "" {
		payload.Binding.Username = "newapi-user-" + strconv.Itoa(userID)
	}

	for _, token := range tokens {
		payload.Tokens = append(payload.Tokens, pixelFlowTokenPayload{
			ID:                 token.Id,
			Key:                token.GetFullKey(),
			ModelLimits:        token.ModelLimits,
			ModelLimitsEnabled: token.ModelLimitsEnabled,
			Name:               token.Name,
			Status:             token.Status,
		})
	}

	c.JSON(http.StatusOK, pixelFlowSessionSyncMessage{
		Type:    "pixelflow:newapi-token-sync",
		Origin:  origin,
		Payload: payload,
	})
}
