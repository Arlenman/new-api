package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const defaultIPLocationAPIURL = "https://ipwho.is/{ip}"

type IPLocation struct {
	CountryCode string `json:"country_code,omitempty"`
	Region      string `json:"region,omitempty"`
	City        string `json:"city,omitempty"`
	Private     bool   `json:"private,omitempty"`
}

type ipLocationAPIResponse struct {
	Success     bool   `json:"success"`
	CountryCode string `json:"country_code"`
	Region      string `json:"region"`
	City        string `json:"city"`
	Message     string `json:"message"`
}

func LookupIPLocation(ctx context.Context, normalizedIP string) (IPLocation, error) {
	ip := net.ParseIP(normalizedIP)
	if ip == nil {
		return IPLocation{}, errors.New("IP 地址格式无效")
	}
	if common.IsPrivateIP(ip) || ip.IsPrivate() || !ip.IsGlobalUnicast() {
		return IPLocation{Private: true}, nil
	}

	endpoint := common.GetEnvOrDefaultString("IP_LOCATION_API_URL", defaultIPLocationAPIURL)
	escapedIP := url.PathEscape(ip.String())
	if strings.Contains(endpoint, "{ip}") {
		endpoint = strings.ReplaceAll(endpoint, "{ip}", escapedIP)
	} else {
		endpoint = strings.TrimRight(endpoint, "/") + "/" + escapedIP
	}
	parsedURL, err := url.Parse(endpoint)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return IPLocation{}, errors.New("IP 地区查询服务地址无效")
	}

	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return IPLocation{}, err
	}
	client := GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return IPLocation{}, fmt.Errorf("IP 地区查询失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return IPLocation{}, fmt.Errorf("IP 地区查询服务返回状态码 %d", resp.StatusCode)
	}

	var payload ipLocationAPIResponse
	if err := common.DecodeJson(io.LimitReader(resp.Body, 64*1024), &payload); err != nil {
		return IPLocation{}, fmt.Errorf("解析 IP 地区查询结果失败: %w", err)
	}
	if !payload.Success {
		if strings.TrimSpace(payload.Message) == "" {
			payload.Message = "IP 地区查询服务未返回结果"
		}
		return IPLocation{}, errors.New(payload.Message)
	}

	location := IPLocation{
		CountryCode: normalizeIPLocationField(payload.CountryCode, 8),
		Region:      normalizeIPLocationField(payload.Region, 128),
		City:        normalizeIPLocationField(payload.City, 128),
	}
	if location.CountryCode == "" && location.Region == "" && location.City == "" {
		return IPLocation{}, errors.New("IP 地区查询服务未返回地区信息")
	}
	return location, nil
}

func normalizeIPLocationField(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = string(runes[:maxRunes])
	}
	return value
}
