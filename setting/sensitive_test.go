package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSensitiveRequestLoggingEnabledByDefault(t *testing.T) {
	assert.True(t, LogSensitiveRequestEnabled)
}
