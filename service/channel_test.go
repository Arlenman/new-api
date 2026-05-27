package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestShouldDisableChannelUsesAutomaticDisableStatusCodes(t *testing.T) {
	origEnabled := common.AutomaticDisableChannelEnabled
	origRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = origEnabled
		operation_setting.AutomaticDisableStatusCodeRanges = origRanges
	})

	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{
		{Start: http.StatusInternalServerError, End: http.StatusInternalServerError},
	}

	err := types.NewErrorWithStatusCode(errors.New("upstream error"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)

	require.True(t, ShouldDisableChannel(err))
}

func TestShouldDisableChannelRequiresAutomaticDisableEnabled(t *testing.T) {
	origEnabled := common.AutomaticDisableChannelEnabled
	origRanges := operation_setting.AutomaticDisableStatusCodeRanges
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = origEnabled
		operation_setting.AutomaticDisableStatusCodeRanges = origRanges
	})

	common.AutomaticDisableChannelEnabled = false
	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{
		{Start: http.StatusInternalServerError, End: http.StatusInternalServerError},
	}

	err := types.NewErrorWithStatusCode(errors.New("upstream error"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)

	require.False(t, ShouldDisableChannel(err))
}
