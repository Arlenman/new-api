package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

const (
	UpstreamPriorityMinIntervalSeconds = 15
	UpstreamPriorityMaxIntervalSeconds = 86400
	UpstreamPriorityMinLatencySeconds  = 1
	UpstreamPriorityMaxLatencySeconds  = 300
)

type UpstreamPrioritySetting struct {
	Enabled               bool `json:"enabled"`
	IntervalSeconds       int  `json:"interval_seconds"`
	MaxTestLatencySeconds int  `json:"max_test_latency_seconds"`
}

var upstreamPrioritySetting = UpstreamPrioritySetting{
	Enabled:               false,
	IntervalSeconds:       300,
	MaxTestLatencySeconds: 5,
}

func init() {
	config.GlobalConfig.Register("upstream_priority_setting", &upstreamPrioritySetting)
}

func GetUpstreamPrioritySetting() *UpstreamPrioritySetting {
	return &upstreamPrioritySetting
}
