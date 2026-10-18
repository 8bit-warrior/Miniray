package controller

import (
	"bytes"
	htmlpkg "html"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/8bit-warrior/Miniray/internal/config"
	"github.com/8bit-warrior/Miniray/internal/logger"
	"github.com/8bit-warrior/Miniray/internal/web/session"
)

var distFS fs.FS

func SetDistFS(fsys fs.FS) {
	distFS = fsys
}

var distPageBuildTime = time.Now()

func normalizeWebBasePath(basePath string) string {
	if basePath == "" {
		return "/"
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	if !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}
	return basePath
}

func pwaHeadInjection(basePath, pageName string) []byte {
	if pageName != "index.html" && pageName != "login.html" {
		return nil
	}

	basePath = normalizeWebBasePath(basePath)
	return []byte(`<link rel="manifest" href="` + htmlpkg.EscapeString(basePath+"manifest.webmanifest") + `"><script data-cfasync="false" defer src="` + htmlpkg.EscapeString(basePath+"pwa-register.js") + `"></script>`)
}

func serveDistPage(c *gin.Context, name string) {
	body, err := fs.ReadFile(distFS, "dist/"+name)
	if err != nil {
		c.String(http.StatusInternalServerError, "missing embedded page: %s", name)
		return
	}

	basePath := c.GetString("base_path")
	if basePath == "" {
		basePath = "/"
	}

	if basePath != "/" {
		body = bytes.ReplaceAll(body, []byte(`src="/assets/`), []byte(`src="`+basePath+`assets/`))
		body = bytes.ReplaceAll(body, []byte(`href="/assets/`), []byte(`href="`+basePath+`assets/`))
	}

	jsEscape := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"<", `<`,
		">", `>`,
		"&", `&`,
	)
	escapedBase := jsEscape.Replace(basePath)
	csrfToken, err := session.EnsureCSRFToken(c)
	if err != nil {
		logger.Warning("Unable to mint CSRF token for", name+":", err)
		csrfToken = ""
	}
	csrfMeta := []byte(`<meta name="csrf-token" content="` + htmlpkg.EscapeString(csrfToken) + `">`)
	basePathMeta := []byte(`<meta name="base-path" content="` + htmlpkg.EscapeString(basePath) + `">`)

	nonceAttr := ""
	if nonce := c.GetString("csp_nonce"); nonce != "" {
		nonceAttr = ` nonce="` + htmlpkg.EscapeString(nonce) + `"`
	}
	script := `<script data-cfasync="false"` + nonceAttr + `>window.MINIRAY_BASE_PATH="` + escapedBase + `"`
	if name != "login.html" {
		escapedVer := jsEscape.Replace(config.GetPanelVersion())
		script += `;window.MINIRAY_CUR_VER="` + escapedVer + `"`
		script += `;window.MINIRAY_DB_TYPE="` + config.GetDBKind() + `"`
	}
	script += `;</script>`
	inject := []byte(script)
	inject = append(inject, csrfMeta...)
	inject = append(inject, basePathMeta...)
	inject = append(inject, pwaHeadInjection(basePath, name)...)
	inject = append(inject, []byte(`</head>`)...)
	out := bytes.Replace(body, []byte("</head>"), inject, 1)

	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Header("Last-Modified", distPageBuildTime.UTC().Format(http.TimeFormat))
	c.Data(http.StatusOK, "text/html; charset=utf-8", out)
}
