package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
)

func TestAuthenticateManagementKey_LocalhostIPBan_BlocksCorrectKeyDuringBan(t *testing.T) {
	h := &Handler{
		cfg:            &config.Config{},
		failedAttempts: make(map[string]*attemptInfo),
		envSecret:      "test-secret",
	}

	for i := 0; i < 5; i++ {
		allowed, statusCode, errMsg := h.AuthenticateManagementKey("127.0.0.1", true, "wrong-secret")
		if allowed {
			t.Fatalf("expected auth to be denied at attempt %d", i+1)
		}
		if statusCode != http.StatusUnauthorized || errMsg != "invalid management key" {
			t.Fatalf("unexpected auth failure at attempt %d: status=%d msg=%q", i+1, statusCode, errMsg)
		}
	}

	allowed, statusCode, errMsg := h.AuthenticateManagementKey("127.0.0.1", true, "test-secret")
	if allowed {
		t.Fatalf("expected correct key to be denied while banned")
	}
	if statusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden status while banned, got %d", statusCode)
	}
	if !strings.HasPrefix(errMsg, "IP banned due to too many failed attempts. Try again in") {
		t.Fatalf("unexpected banned message: %q", errMsg)
	}
}

func TestMiddlewareSetsSupportPluginHeader(t *testing.T) {

	h := &Handler{
		cfg:            &config.Config{},
		failedAttempts: make(map[string]*attemptInfo),
		envSecret:      "test-secret",
	}
	middleware := h.Middleware()

	t.Run("invalid key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
		c.Request.RemoteAddr = "127.0.0.1:12345"
		c.Request.Header.Set("X-Management-Key", "wrong-secret")

		middleware(c)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
		if got := rec.Header().Get("X-CPA-SUPPORT-PLUGIN"); got != pluginhost.SupportPluginHeaderValue() {
			t.Fatalf("X-CPA-SUPPORT-PLUGIN = %q, want %q", got, pluginhost.SupportPluginHeaderValue())
		}
	})

	t.Run("valid key", func(t *testing.T) {
		engine := gin.New()
		engine.GET("/v0/management/config", middleware, func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("X-Management-Key", "test-secret")
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("X-CPA-SUPPORT-PLUGIN"); got != pluginhost.SupportPluginHeaderValue() {
			t.Fatalf("X-CPA-SUPPORT-PLUGIN = %q, want %q", got, pluginhost.SupportPluginHeaderValue())
		}
	})
}

func TestMiddlewareTrustedHeaderAuth(t *testing.T) {
	cfg := &config.Config{}
	cfg.RemoteManagement.TrustedHeaderAuth.Enabled = true
	cfg.RemoteManagement.TrustedHeaderAuth.UserIDHeader = "X-User-UUID"
	cfg.RemoteManagement.TrustedHeaderAuth.TrustedProxies = []string{"10.0.0.0/8", "192.168.1.1"}

	h := &Handler{
		cfg:            cfg,
		failedAttempts: make(map[string]*attemptInfo),
		envSecret:      "test-secret",
	}
	middleware := h.Middleware()

	t.Run("header auth disabled", func(t *testing.T) {
		hDisabled := &Handler{
			cfg: &config.Config{
				RemoteManagement: config.RemoteManagement{
					TrustedHeaderAuth: config.TrustedHeaderAuth{
						Enabled: false,
					},
				},
			},
			failedAttempts: make(map[string]*attemptInfo),
			envSecret:      "test-secret",
		}
		mw := hDisabled.Middleware()

		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
		c.Request.RemoteAddr = "10.0.0.1:12345"
		c.Request.Header.Set("X-User-UUID", "user-123")

		mw(c)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected StatusForbidden, got %d", rec.Code)
		}
	})

	t.Run("untrusted IP with header", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
		c.Request.RemoteAddr = "172.16.0.1:12345"
		c.Request.Header.Set("X-User-UUID", "user-123")

		middleware(c)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected StatusForbidden, got %d", rec.Code)
		}
	})

	t.Run("trusted CIDR proxy IP with header", func(t *testing.T) {
		engine := gin.New()
		engine.GET("/v0/management/whoami", middleware, h.Whoami)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v0/management/whoami", nil)
		req.RemoteAddr = "10.0.0.5:12345"
		req.Header.Set("X-User-UUID", "user-456")
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected StatusOK, got %d", rec.Code)
		}
		expectedBody := `{"auth_method":"header","authenticated":true,"user_id":"user-456"}`
		if strings.TrimSpace(rec.Body.String()) != expectedBody {
			t.Fatalf("expected body %q, got %q", expectedBody, rec.Body.String())
		}
	})

	t.Run("trusted single IP proxy with header", func(t *testing.T) {
		engine := gin.New()
		engine.GET("/v0/management/whoami", middleware, h.Whoami)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v0/management/whoami", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		req.Header.Set("X-User-UUID", "user-789")
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected StatusOK, got %d", rec.Code)
		}
	})

	t.Run("trusted proxy IP with empty header", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
		c.Request.RemoteAddr = "10.0.0.5:12345"
		c.Request.Header.Set("X-User-UUID", "")

		middleware(c)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected StatusForbidden, got %d", rec.Code)
		}
	})

	t.Run("localhost always trusted", func(t *testing.T) {
		engine := gin.New()
		engine.GET("/v0/management/whoami", middleware, h.Whoami)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v0/management/whoami", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("X-User-UUID", "user-local")
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected StatusOK, got %d", rec.Code)
		}
	})
}
