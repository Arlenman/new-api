package middleware

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkipAdminAuditResponseCaptureKeepsSensitiveBodyOutOfAuditBuffer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	writer := &auditResponseWriter{
		ResponseWriter: context.Writer,
		body:           bytes.NewBuffer(nil),
		maxSize:        64 * 1024,
	}
	context.Writer = writer

	SkipAdminAuditResponseCapture(context)
	context.JSON(200, gin.H{"success": true, "data": gin.H{"api_key": "test-api-key"}})

	require.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "test-api-key")
	assert.Empty(t, writer.body.Bytes())
}
