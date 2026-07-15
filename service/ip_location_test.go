package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupIPLocationTreatsPrivateIPAsLocalWithoutRemoteLookup(t *testing.T) {
	location, err := LookupIPLocation(context.Background(), "192.168.1.20")

	require.NoError(t, err)
	assert.True(t, location.Private)
	assert.Empty(t, location.CountryCode)
	assert.Empty(t, location.Region)
	assert.Empty(t, location.City)
}

func TestLookupIPLocationRejectsInvalidIP(t *testing.T) {
	_, err := LookupIPLocation(context.Background(), "not-an-ip")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "IP 地址格式无效")
}
