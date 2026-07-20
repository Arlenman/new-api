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

const infiniteCanvasRoute = "/_tools/infinite-canvas"

var hashedInfiniteCanvasAssetPattern = regexp.MustCompile(`-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`)

type infiniteCanvasBuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
}

type infiniteCanvas struct {
	root      string
	buildInfo infiniteCanvasBuildInfo
}

func loadInfiniteCanvas(dist string) (*infiniteCanvas, error) {
	root, err := filepath.Abs(strings.TrimSpace(dist))
	if err != nil {
		return nil, fmt.Errorf("resolve infinite canvas distribution: %w", err)
	}
	indexInfo, err := os.Stat(filepath.Join(root, "index.html"))
	if err != nil {
		return nil, fmt.Errorf("read infinite canvas index.html: %w", err)
	}
	if !indexInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("infinite canvas index.html is not a regular file")
	}

	buildInfoData, err := os.ReadFile(filepath.Join(root, "build-info.json"))
	if err != nil {
		return nil, fmt.Errorf("read infinite canvas build-info.json: %w", err)
	}
	var buildInfo infiniteCanvasBuildInfo
	if err = common.Unmarshal(buildInfoData, &buildInfo); err != nil {
		return nil, fmt.Errorf("decode infinite canvas build-info.json: %w", err)
	}
	if strings.TrimSpace(buildInfo.Version) == "" || strings.TrimSpace(buildInfo.Commit) == "" {
		return nil, fmt.Errorf("infinite canvas build-info.json is missing version or commit")
	}

	return &infiniteCanvas{root: root, buildInfo: buildInfo}, nil
}

func (tool *infiniteCanvas) serve(c *gin.Context) {
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
	if err != nil && os.IsNotExist(err) && isInfiniteCanvasPageRoute(cleanPath) {
		cleanPath = "index.html"
		fullPath = filepath.Join(tool.root, cleanPath)
		file, err = os.Open(fullPath)
	}
	if err != nil {
		if !os.IsNotExist(err) && !os.IsPermission(err) {
			common.SysError("open infinite canvas asset: " + err.Error())
		}
		c.Status(http.StatusNotFound)
		return
	}

	info, err := file.Stat()
	if err == nil && info.IsDir() && isInfiniteCanvasPageRoute(cleanPath) {
		if closeErr := file.Close(); closeErr != nil {
			common.SysError("close infinite canvas directory: " + closeErr.Error())
		}
		cleanPath = "index.html"
		file, err = os.Open(filepath.Join(tool.root, cleanPath))
		if err == nil {
			info, err = file.Stat()
		}
	}
	if err != nil || !info.Mode().IsRegular() {
		if closeErr := file.Close(); closeErr != nil {
			common.SysError("close infinite canvas asset: " + closeErr.Error())
		}
		c.Status(http.StatusNotFound)
		return
	}
	defer file.Close()

	c.Header("Content-Security-Policy", "frame-ancestors 'self'")
	c.Header("Referrer-Policy", "same-origin")
	c.Header("X-Content-Type-Options", "nosniff")
	if cleanPath == "sw.js" {
		c.Header("Service-Worker-Allowed", infiniteCanvasRoute+"/")
	}
	if strings.HasPrefix(cleanPath, "assets/") && hashedInfiniteCanvasAssetPattern.MatchString(path.Base(cleanPath)) {
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
			common.SysError("read infinite canvas index: " + readErr.Error())
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", injectInfiniteCanvasUserIdentity(index, c.GetInt("id")))
		return
	}
	http.ServeContent(c.Writer, c.Request, path.Base(cleanPath), info.ModTime(), file)
}

func injectInfiniteCanvasUserIdentity(index []byte, userID int) []byte {
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

func isInfiniteCanvasPageRoute(cleanPath string) bool {
	switch cleanPath {
	case "image", "video", "assets", "prompts", "canvas", "config":
		return true
	}
	segments := strings.Split(cleanPath, "/")
	return len(segments) == 2 && segments[0] == "canvas" && segments[1] != ""
}
