package constant

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPath2RelayModeRecognizesPlaygroundImageCompatibilityPaths(t *testing.T) {
	tests := []struct {
		path string
		mode int
	}{
		{path: "/pg/images/generations", mode: RelayModeImagesGenerations},
		{path: "/pg/v1/images/generations", mode: RelayModeImagesGenerations},
		{path: "/pg/images/edits", mode: RelayModeImagesEdits},
		{path: "/pg/v1/images/edits", mode: RelayModeImagesEdits},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			assert.Equal(t, test.mode, Path2RelayMode(test.path))
			assert.True(t, IsPlaygroundImagePath(test.path))
			assert.True(t, IsPlaygroundRelayPath(test.path))
		})
	}
}

func TestPath2RelayModeRecognizesPlaygroundResponsesCompatibilityPaths(t *testing.T) {
	for _, path := range []string{"/pg/responses", "/pg/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			assert.Equal(t, RelayModeResponses, Path2RelayMode(path))
			assert.True(t, IsPlaygroundResponsesPath(path))
			assert.True(t, IsPlaygroundRelayPath(path))
		})
	}
}
