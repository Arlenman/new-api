package router

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const imagePlaygroundRoute = "/_tools/gpt-image-playground"

var hashedImagePlaygroundAssetPattern = regexp.MustCompile(`-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`)

type imagePlaygroundBuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
}

type imagePlayground struct {
	root      string
	buildInfo imagePlaygroundBuildInfo
}

func loadImagePlayground(dist string) (*imagePlayground, error) {
	root, err := filepath.Abs(strings.TrimSpace(dist))
	if err != nil {
		return nil, fmt.Errorf("resolve image playground distribution: %w", err)
	}
	indexInfo, err := os.Stat(filepath.Join(root, "index.html"))
	if err != nil {
		return nil, fmt.Errorf("read image playground index.html: %w", err)
	}
	if !indexInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("image playground index.html is not a regular file")
	}

	buildInfoData, err := os.ReadFile(filepath.Join(root, "build-info.json"))
	if err != nil {
		return nil, fmt.Errorf("read image playground build-info.json: %w", err)
	}
	var buildInfo imagePlaygroundBuildInfo
	if err = common.Unmarshal(buildInfoData, &buildInfo); err != nil {
		return nil, fmt.Errorf("decode image playground build-info.json: %w", err)
	}
	if strings.TrimSpace(buildInfo.Version) == "" || strings.TrimSpace(buildInfo.Commit) == "" {
		return nil, fmt.Errorf("image playground build-info.json is missing version or commit")
	}

	return &imagePlayground{root: root, buildInfo: buildInfo}, nil
}

func (tool *imagePlayground) serve(c *gin.Context) {
	requestedPath := strings.TrimPrefix(c.Param("filepath"), "/")
	if requestedPath == "" {
		requestedPath = "index.html"
	}
	cleanPath := strings.TrimPrefix(path.Clean("/"+requestedPath), "/")
	if cleanPath == "." || cleanPath != requestedPath {
		c.Status(http.StatusNotFound)
		return
	}

	fullPath := filepath.Join(tool.root, filepath.FromSlash(cleanPath))
	relativePath, err := filepath.Rel(tool.root, fullPath)
	if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		c.Status(http.StatusNotFound)
		return
	}
	file, err := os.Open(fullPath)
	if err != nil {
		if !os.IsNotExist(err) && !os.IsPermission(err) {
			common.SysError("open image playground asset: " + err.Error())
		}
		c.Status(http.StatusNotFound)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		c.Status(http.StatusNotFound)
		return
	}

	c.Header("Content-Security-Policy", "frame-ancestors 'self'")
	c.Header("Referrer-Policy", "same-origin")
	c.Header("X-Content-Type-Options", "nosniff")
	if cleanPath == "sw.js" {
		c.Header("Service-Worker-Allowed", imagePlaygroundRoute+"/")
	}
	if strings.HasPrefix(cleanPath, "assets/") && hashedImagePlaygroundAssetPattern.MatchString(path.Base(cleanPath)) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		c.Header("Cache-Control", "no-cache")
	}
	if contentType := mime.TypeByExtension(filepath.Ext(cleanPath)); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	if cleanPath == "index.html" {
		index, readErr := os.ReadFile(filepath.Join(tool.root, cleanPath))
		if readErr != nil {
			common.SysError("read image playground index: " + readErr.Error())
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", injectImagePlaygroundUserIdentity(index, c.GetInt("id")))
		return
	}
	http.ServeContent(c.Writer, c.Request, path.Base(cleanPath), info.ModTime(), file)
}

func injectImagePlaygroundUserIdentity(index []byte, userID int) []byte {
	if userID <= 0 {
		return index
	}

	script := fmt.Sprintf(`<script>window.__NEW_API_USER_ID__=%d;</script>`, userID)
	html := string(index)
	headEnd := strings.Index(strings.ToLower(html), "</head>")
	if headEnd < 0 {
		return append([]byte(script), index...)
	}
	return []byte(html[:headEnd] + script + html[headEnd:])
}
