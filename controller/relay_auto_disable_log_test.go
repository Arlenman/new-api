package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildChannelErrorLogOtherRecordsAutoDisableTrigger(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name                 string
		autoDisableTriggered bool
		wantMarker           bool
	}{
		{
			name:                 "triggered",
			autoDisableTriggered: true,
			wantMarker:           true,
		},
		{
			name:                 "not triggered",
			autoDisableTriggered: false,
			wantMarker:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			ctx.Set("channel_id", 12)
			ctx.Set("channel_name", "primary")
			ctx.Set("channel_type", 1)
			ctx.Set("use_channel", []string{"8", "12"})

			err := types.NewErrorWithStatusCode(
				errors.New("upstream rejected the credential"),
				types.ErrorCodeBadResponseStatusCode,
				http.StatusUnauthorized,
			)
			other := buildChannelErrorLogOther(ctx, err, tt.autoDisableTriggered)

			assert.Equal(t, "/v1/chat/completions", other["request_path"])
			assert.Equal(t, http.StatusUnauthorized, other["status_code"])
			assert.Equal(t, 12, other["channel_id"])

			adminInfo, ok := other["admin_info"].(map[string]interface{})
			require.True(t, ok)
			marker, exists := adminInfo["channel_auto_disable_triggered"]
			assert.Equal(t, tt.wantMarker, exists)
			if tt.wantMarker {
				assert.Equal(t, true, marker)
			}
		})
	}
}
