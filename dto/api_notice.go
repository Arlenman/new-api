package dto

type ApiNoticeField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ApiNoticeSection struct {
	Title string `json:"title,omitempty"`
	Text  string `json:"text"`
}

type ApiNoticeAction struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type ApiNoticeColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type ApiNoticeTable struct {
	Columns []ApiNoticeColumn   `json:"columns"`
	Rows    []map[string]string `json:"rows"`
}

type ApiNoticeMessage struct {
	Format   string             `json:"format"`
	Title    string             `json:"title,omitempty"`
	Level    string             `json:"level,omitempty"`
	Text     string             `json:"text,omitempty"`
	Fields   []ApiNoticeField   `json:"fields,omitempty"`
	Sections []ApiNoticeSection `json:"sections,omitempty"`
	Actions  []ApiNoticeAction  `json:"actions,omitempty"`
	Table    *ApiNoticeTable    `json:"table,omitempty"`
}

type ApiNoticeRequest struct {
	IdempotencyKey string           `json:"idempotency_key"`
	Providers      []string         `json:"providers,omitempty"`
	Message        ApiNoticeMessage `json:"message"`
}

type ApiNoticeReceipt struct {
	Provider  string `json:"provider"`
	MessageID string `json:"message_id,omitempty"`
	Attempts  int    `json:"attempts"`
	Accepted  bool   `json:"accepted"`
}

type ApiNoticeProviderResult struct {
	Provider string           `json:"provider"`
	Receipt  ApiNoticeReceipt `json:"receipt"`
	Error    string           `json:"error,omitempty"`
}

type ApiNoticeResult struct {
	RequestID      string                    `json:"request_id"`
	IdempotencyKey string                    `json:"idempotency_key"`
	Results        []ApiNoticeProviderResult `json:"results"`
}

type ApiNoticeResponse struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Result  ApiNoticeResult `json:"result"`
}

type ApiNoticeDeliveryResult struct {
	HTTPStatus int               `json:"http_status"`
	Response   ApiNoticeResponse `json:"response"`
}

type ApiNoticeProvider struct {
	Name         string   `json:"name"`
	Default      bool     `json:"default"`
	Capabilities []string `json:"capabilities"`
	Ready        bool     `json:"ready"`
	Reason       string   `json:"reason,omitempty"`
}

type ApiNoticeEndpointStatus struct {
	Status     string `json:"status"`
	HTTPStatus int    `json:"http_status"`
}

type ApiNoticeConnectionStatus struct {
	Health           ApiNoticeEndpointStatus `json:"health"`
	Ready            ApiNoticeEndpointStatus `json:"ready"`
	Providers        []ApiNoticeProvider     `json:"providers"`
	APIKeyConfigured bool                    `json:"api_key_configured"`
}

type ApiNoticeConfig struct {
	BaseURL           string `json:"base_url"`
	APIKeyConfigured  bool   `json:"api_key_configured"`
	APIKeyMasked      string `json:"api_key_masked"`
	APIKeySource      string `json:"api_key_source"`
	PersistentStorage bool   `json:"persistent_storage_available"`
}

type ApiNoticeConfigUpdate struct {
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	ClearAPIKey bool   `json:"clear_api_key"`
}

type ApiNoticeAPIKeyReveal struct {
	APIKey       string `json:"api_key"`
	APIKeySource string `json:"api_key_source"`
}

type ApiNoticeTestProviderResult struct {
	Provider string `json:"provider"`
	Accepted bool   `json:"accepted"`
	Attempts int    `json:"attempts"`
	Error    string `json:"error,omitempty"`
}

type ApiNoticeTestSendResult struct {
	HTTPStatus int                           `json:"http_status"`
	Results    []ApiNoticeTestProviderResult `json:"results"`
	Error      string                        `json:"error,omitempty"`
}
