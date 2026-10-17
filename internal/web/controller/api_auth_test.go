package controller

import (
	"crypto/tls"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/dbtest"
	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/util/crypto"
	"github.com/8bit-warrior/Miniray/internal/web/session"
)

// TestCheckAPIAuth_BearerSuccess characterizes the bearer-token path: a valid
// token reaches the handler and sets api_authed (the contract the later
// client-cert branch must match).
func TestCheckAPIAuth_BearerSuccess(t *testing.T) {
	engine, _ := newAPIAuthTestEngine(t)

	const plaintext = "characterization-token-value"
	if err := database.GetDB().Create(&model.ApiToken{
		Name:    "t1",
		Token:   crypto.HashTokenSHA256(plaintext),
		Enabled: true,
		Scope:   model.ApiScopeAdmin,
	}).Error; err != nil {
		t.Fatalf("seed token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/panel/api/ping", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != `{"api_authed":true}` {
		t.Fatalf("body = %s, want api_authed true", got)
	}
}

// TestCheckAPIAuth_EmptyVerifiedChainsFallsThrough asserts a TLS request with no
// verified client chain is NOT treated as authenticated (it falls through to the
// bearer/session paths) — so the cert branch can't accidentally authorize plain
// browser HTTPS.
func TestCheckAPIAuth_EmptyVerifiedChainsFallsThrough(t *testing.T) {
	engine, _ := newAPIAuthTestEngine(t)

	req := httptest.NewRequest(http.MethodGet, "/panel/api/ping", nil)
	req.TLS = &tls.ConnectionState{} // handshake done, but no client cert verified
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (unauthenticated, no verified chain)", w.Code)
	}
}

// TestCheckAPIAuth_RejectsUnauthenticated characterizes the reject paths:
// no credential → 404 (masking); XHR or a presented (but invalid) Bearer → 401
// so script authors can tell auth failure from a wrong base path.
func TestCheckAPIAuth_RejectsUnauthenticated(t *testing.T) {
	engine, _ := newAPIAuthTestEngine(t)

	cases := []struct {
		name   string
		xhr    bool
		bearer string // empty = omit Authorization header
		want   int
	}{
		{"xhr gets 401", true, "", http.StatusUnauthorized},
		{"non-xhr gets 404", false, "", http.StatusNotFound},
		{"invalid bearer gets 401", false, "definitely-not-a-token", http.StatusUnauthorized},
		{"invalid bearer xhr gets 401", true, "definitely-not-a-token", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/panel/api/ping", nil)
			if c.xhr {
				req.Header.Set("X-Requested-With", "XMLHttpRequest")
			}
			if c.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+c.bearer)
			}
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			if w.Code != c.want {
				t.Fatalf("status = %d, want %d", w.Code, c.want)
			}
		})
	}
}

// TestCheckAPIAuth_SessionLoginPasses characterizes the session path: a
// logged-in browser session (no bearer token) reaches the handler.
func TestCheckAPIAuth_SessionLoginPasses(t *testing.T) {
	engine, _ := newAPIAuthTestEngine(t)

	db := database.GetDB()
	var n int64
	if err := db.Model(&model.User{}).Count(&n).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n == 0 {
		if err := db.Create(&model.User{Username: "sess", Password: "x"}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	ts := httptest.NewServer(engine)
	defer ts.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	loginResp, err := client.Get(ts.URL + "/test-login")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", loginResp.StatusCode)
	}

	pingResp, err := client.Get(ts.URL + "/panel/api/ping")
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	pingResp.Body.Close()
	if pingResp.StatusCode != http.StatusOK {
		t.Fatalf("session ping status = %d, want 200", pingResp.StatusCode)
	}
}

func newAPIAuthTestEngine(t *testing.T) (*gin.Engine, *APIController) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("MINIRAY_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "miniray.db"))
	engine := gin.New()
	store := cookie.NewStore([]byte("api-auth-test-secret"))
	engine.Use(sessions.Sessions("miniray", store))

	a := &APIController{}

	// Logs in as the first user so the session path can be exercised over a
	// cookie round-trip without reaching into checkAPIAuth's internals.
	engine.GET("/test-login", func(c *gin.Context) {
		u, err := a.userService.GetFirstUser()
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		if err := session.SetLoginUser(c, u); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
	})

	api := engine.Group("/panel/api")
	api.Use(a.checkAPIAuth)
	api.Use(a.enforceTokenScope)
	api.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"api_authed": c.GetBool("api_authed")})
	})
	api.GET("/server/status", func(c *gin.Context) {
		scope, _ := c.Get("api_token_scope")
		c.JSON(http.StatusOK, gin.H{"api_authed": c.GetBool("api_authed"), "scope": scope})
	})
	api.POST("/server/updatePanel", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"reached": true})
	})
	api.POST("/clients/:email/detach", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"reached": true})
	})
	api.POST("/inbounds/:id/resetTraffic", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"reached": true})
	})
	api.POST("/clients/clientIps", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"reached": true})
	})
	return engine, a
}
