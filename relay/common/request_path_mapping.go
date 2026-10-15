package common

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type requestPathMappingConfig struct {
	Channel map[string]string            `json:"channel"`
	Models  map[string]map[string]string `json:"models"`
}

func ResolveRequestPath(modelName, requestPath, rawMapping string) string {
	return resolveRequestPathForModels([]string{modelName}, requestPath, rawMapping)
}

func ResolveRequestPathForModels(modelNames []string, requestPath, rawMapping string) string {
	return resolveRequestPathForModels(modelNames, requestPath, rawMapping)
}

func resolveRequestPathForModels(modelNames []string, requestPath, rawMapping string) string {
	requestURL, err := url.Parse(requestPath)
	if err != nil {
		return requestPath
	}
	originalPath := requestURL.Path
	if originalPath == "" {
		return requestPath
	}

	var mapping requestPathMappingConfig
	if strings.TrimSpace(rawMapping) == "" || common.Unmarshal([]byte(rawMapping), &mapping) != nil {
		return requestPath
	}

	for _, modelName := range modelNames {
		if modelName == "" {
			continue
		}
		if paths, ok := mapping.Models[modelName]; ok {
			if mappedPath := strings.TrimSpace(paths[originalPath]); mappedPath != "" {
				return replaceRequestPath(requestURL, mappedPath)
			}
		}
	}

	configuredModels := make([]string, 0, len(mapping.Models))
	for configuredModel := range mapping.Models {
		if strings.HasPrefix(configuredModel, "re:") {
			configuredModels = append(configuredModels, configuredModel)
		}
	}
	sort.SliceStable(configuredModels, func(i, j int) bool {
		if len(configuredModels[i]) == len(configuredModels[j]) {
			return configuredModels[i] < configuredModels[j]
		}
		return len(configuredModels[i]) > len(configuredModels[j])
	})
	for _, modelName := range modelNames {
		for _, configuredModel := range configuredModels {
			paths := mapping.Models[configuredModel]
			if !matchRequestPathModel(configuredModel, modelName) {
				continue
			}
			if mappedPath := strings.TrimSpace(paths[originalPath]); mappedPath != "" {
				return replaceRequestPath(requestURL, mappedPath)
			}
		}
	}
	if mappedPath := strings.TrimSpace(mapping.Channel[originalPath]); mappedPath != "" {
		return replaceRequestPath(requestURL, mappedPath)
	}
	return requestPath
}

func matchRequestPathModel(configuredModel, modelName string) bool {
	configuredModel = strings.TrimSpace(configuredModel)
	if strings.HasPrefix(configuredModel, "re:") {
		pattern := strings.TrimPrefix(configuredModel, "re:")
		re, err := regexp.Compile(pattern)
		return err == nil && re.MatchString(modelName)
	}
	return configuredModel == modelName
}

func replaceRequestPath(requestURL *url.URL, mappedPath string) string {
	mappedURL, err := url.Parse(mappedPath)
	if err != nil || mappedURL.Path == "" || !strings.HasPrefix(mappedURL.Path, "/") || mappedURL.Fragment != "" || mappedURL.Host != "" || mappedURL.User != nil {
		return requestURL.String()
	}
	requestURL.Path = mappedURL.Path
	requestURL.RawPath = mappedURL.RawPath
	if mappedURL.RawQuery != "" {
		requestURL.RawQuery = mappedURL.RawQuery
	}
	return requestURL.String()
}
