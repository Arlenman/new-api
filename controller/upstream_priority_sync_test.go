package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssignUpstreamPriorityRanksUsesDistinctEffectiveRatios(t *testing.T) {
	candidates := []upstreamPriorityCandidate{
		{Channel: &model.UpstreamChannel{Id: 1}, EffectiveRatio: decimal.RequireFromString("0.03")},
		{Channel: &model.UpstreamChannel{Id: 2}, EffectiveRatio: decimal.RequireFromString("0.20")},
		{Channel: &model.UpstreamChannel{Id: 3}, EffectiveRatio: decimal.RequireFromString("0.05")},
		{Channel: &model.UpstreamChannel{Id: 4}, EffectiveRatio: decimal.RequireFromString("0.03")},
		{Channel: &model.UpstreamChannel{Id: 5}, EffectiveRatio: decimal.RequireFromString("0.12")},
		{Channel: &model.UpstreamChannel{Id: 6}, EffectiveRatio: decimal.RequireFromString("0.06")},
		{Channel: &model.UpstreamChannel{Id: 7}, EffectiveRatio: decimal.RequireFromString("0.035")},
	}

	assignUpstreamPriorityRanks(candidates)

	assert.Equal(t, []int{2, 5, 6, 3, 7, 1, 4}, []int{
		candidates[0].Channel.Id,
		candidates[1].Channel.Id,
		candidates[2].Channel.Id,
		candidates[3].Channel.Id,
		candidates[4].Channel.Id,
		candidates[5].Channel.Id,
		candidates[6].Channel.Id,
	})
	assert.Equal(t, []int64{10, 11, 12, 13, 14, 15, 15}, []int64{
		candidates[0].Priority,
		candidates[1].Priority,
		candidates[2].Priority,
		candidates[3].Priority,
		candidates[4].Priority,
		candidates[5].Priority,
		candidates[6].Priority,
	})
}

func TestBuildUpstreamPriorityCandidateUsesSelectedGroupRatioAndMultiplier(t *testing.T) {
	groupID := int64(7)
	channel := &model.UpstreamChannel{
		Id:               1,
		SelectedGroup:    " premium ",
		DefaultTestModel: " gpt-4o-mini ",
		Multiplier:       1.5,
	}
	snapshot := service.UpstreamSnapshot{
		Groups: []service.UpstreamGroup{{ID: groupID, Name: "premium", Ratio: 0.9}},
		Ratios: map[string]float64{
			"7":       0.04,
			"premium": 0.05,
		},
		Models: []service.UpstreamModel{{ID: "gpt-4o-mini"}},
		Keys: []service.UpstreamKey{
			{ID: 11, Group: "other", Status: "active", KeyFingerprint: "other-key"},
			{ID: 12, GroupID: &groupID, Status: "active", KeyFingerprint: "selected-key"},
		},
	}

	candidate, err := buildUpstreamPriorityCandidate(channel, snapshot)
	require.NoError(t, err)

	assert.True(t, candidate.EffectiveRatio.Equal(decimal.RequireFromString("0.06")))
	require.Len(t, candidate.SelectedKeys, 1)
	assert.Equal(t, int64(12), candidate.SelectedKeys[0].ID)
}

func TestBuildUpstreamPriorityCandidateFallsBackToGroupNameThenGroupRatio(t *testing.T) {
	groupID := int64(7)
	tests := []struct {
		name      string
		ratios    map[string]float64
		group     service.UpstreamGroup
		wantRatio string
	}{
		{
			name:      "group name ratio",
			ratios:    map[string]float64{"premium": 0.05},
			group:     service.UpstreamGroup{ID: groupID, Name: "premium", Ratio: 0.9},
			wantRatio: "0.05",
		},
		{
			name:      "group ratio",
			ratios:    map[string]float64{},
			group:     service.UpstreamGroup{ID: groupID, Name: "premium", Ratio: 0.07},
			wantRatio: "0.07",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &model.UpstreamChannel{
				Id:               1,
				SelectedGroup:    "premium",
				DefaultTestModel: "gpt-4o-mini",
				Multiplier:       1,
			}
			snapshot := service.UpstreamSnapshot{
				Groups: []service.UpstreamGroup{tt.group},
				Ratios: tt.ratios,
				Models: []service.UpstreamModel{{ID: "gpt-4o-mini"}},
				Keys: []service.UpstreamKey{{
					ID:             12,
					Group:          "premium",
					Status:         "active",
					KeyFingerprint: "selected-key",
				}},
			}

			candidate, err := buildUpstreamPriorityCandidate(channel, snapshot)
			require.NoError(t, err)
			assert.True(t, candidate.EffectiveRatio.Equal(decimal.RequireFromString(tt.wantRatio)))
		})
	}
}

func TestMatchChannelsForUpstreamCandidateRequiresBaseURLAndSelectedKey(t *testing.T) {
	baseURL := "https://upstream.example/v1"
	sameNormalizedBaseURL := "https://UPSTREAM.example/v1/"
	otherBaseURL := "https://other.example"
	selectedKey := "sk-selected"
	otherKey := "sk-other"
	candidate := upstreamPriorityCandidate{
		Channel: &model.UpstreamChannel{BaseURL: baseURL},
		SelectedKeys: []service.UpstreamKey{{
			KeyFingerprint: model.UpstreamKeyFingerprint(selectedKey),
		}},
	}
	channels := []*model.Channel{
		{Id: 1, BaseURL: &sameNormalizedBaseURL, Key: selectedKey},
		{Id: 2, BaseURL: &sameNormalizedBaseURL, Key: otherKey + "\n" + selectedKey, ChannelInfo: model.ChannelInfo{IsMultiKey: true}},
		{Id: 3, BaseURL: &sameNormalizedBaseURL, Key: otherKey},
		{Id: 4, BaseURL: &otherBaseURL, Key: selectedKey},
	}

	matched := matchChannelsForUpstreamCandidate(candidate, indexChannelsByNormalizedBaseURL(channels))

	require.Len(t, matched, 2)
	assert.Equal(t, []int{1, 2}, []int{matched[0].Id, matched[1].Id})
}

func TestSelectUpstreamPriorityTestChannelPrefersDefaultModel(t *testing.T) {
	first := &model.Channel{Id: 1, Models: "claude-3-5-sonnet"}
	matching := &model.Channel{Id: 2, Models: "gpt-4o-mini,claude-3-5-sonnet"}

	assert.Same(t, matching, selectUpstreamPriorityTestChannel([]*model.Channel{first, matching}, "gpt-4o-mini"))
	assert.Same(t, first, selectUpstreamPriorityTestChannel([]*model.Channel{first, matching}, "missing-model"))
	assert.Nil(t, selectUpstreamPriorityTestChannel(nil, "gpt-4o-mini"))
}
