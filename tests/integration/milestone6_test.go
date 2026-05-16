//go:build !integration
// +build !integration

package integration

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/data/proxy"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/credential"
	"github.com/taichirain/portkey/internal/domain/plugin"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"go.uber.org/zap"
)

func newTestProxyWithAuth(t *testing.T, snap *snapshot.ConfigSnapshot) *proxy.Proxy {
	t.Helper()
	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)
	if snap != nil {
		p.UpdateSnapshot(snap)
	}
	return p
}

func makeTestJWT(secret, issuer string, exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]interface{}{
		"iss": issuer,
		"exp": exp.Unix(),
	})
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)

	signingInput := header + "." + payloadB64
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sig
}

// === Key-Auth E2E Tests ===

func TestM6_KeyAuth_Success(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	consumerID := uuid.New()
	cred := &credential.Credential{
		ID: uuid.New(), ConsumerID: consumerID,
		Type: credential.TypeKeyAuth, Key: "valid-api-key", Enabled: true,
	}
	snap.AddCredential(cred)

	keyAuthPlugin, _ := plugin.New("key-auth", map[string]interface{}{})
	snap.AddPlugin(keyAuthPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithAuth(t, snap)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	req.Header.Set("apikey", "valid-api-key")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestM6_KeyAuth_MissingKey_401(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	keyAuthPlugin, _ := plugin.New("key-auth", map[string]interface{}{})
	snap.AddPlugin(keyAuthPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithAuth(t, snap)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}

	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if !strings.Contains(body["message"], "No API key") {
		t.Errorf("Expected 'No API key' message, got: %s", body["message"])
	}
}

func TestM6_KeyAuth_InvalidKey_401(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	cred := &credential.Credential{
		ID: uuid.New(), ConsumerID: uuid.New(),
		Type: credential.TypeKeyAuth, Key: "real-key", Enabled: true,
	}
	snap.AddCredential(cred)

	keyAuthPlugin, _ := plugin.New("key-auth", map[string]interface{}{})
	snap.AddPlugin(keyAuthPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithAuth(t, snap)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	req.Header.Set("apikey", "wrong-key")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
}

func TestM6_KeyAuth_DisabledCredential_401(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	cred := &credential.Credential{
		ID: uuid.New(), ConsumerID: uuid.New(),
		Type: credential.TypeKeyAuth, Key: "disabled-key", Enabled: false,
	}
	snap.AddCredential(cred)

	keyAuthPlugin, _ := plugin.New("key-auth", map[string]interface{}{})
	snap.AddPlugin(keyAuthPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithAuth(t, snap)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	req.Header.Set("apikey", "disabled-key")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
}

// === JWT-Auth E2E Tests ===

func TestM6_JWTAuth_Success(t *testing.T) {
	secret := "jwt-secret"
	issuer := "jwt-issuer"

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	cred := &credential.Credential{
		ID: uuid.New(), ConsumerID: uuid.New(),
		Type: credential.TypeJWTAuth, Key: issuer, Secret: secret, Enabled: true,
	}
	snap.AddCredential(cred)

	jwtPlugin, _ := plugin.New("jwt-auth", map[string]interface{}{})
	snap.AddPlugin(jwtPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithAuth(t, snap)

	token := makeTestJWT(secret, issuer, time.Now().Add(1*time.Hour))
	req := httptest.NewRequest("GET", "/test/hello", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestM6_JWTAuth_MissingToken_401(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	jwtPlugin, _ := plugin.New("jwt-auth", map[string]interface{}{})
	snap.AddPlugin(jwtPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithAuth(t, snap)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}

	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if !strings.Contains(body["message"], "No JWT") {
		t.Errorf("Expected 'No JWT' message, got: %s", body["message"])
	}
}

func TestM6_JWTAuth_ExpiredToken_401(t *testing.T) {
	secret := "jwt-secret"
	issuer := "jwt-issuer"

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	cred := &credential.Credential{
		ID: uuid.New(), ConsumerID: uuid.New(),
		Type: credential.TypeJWTAuth, Key: issuer, Secret: secret, Enabled: true,
	}
	snap.AddCredential(cred)

	jwtPlugin, _ := plugin.New("jwt-auth", map[string]interface{}{})
	snap.AddPlugin(jwtPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithAuth(t, snap)

	token := makeTestJWT(secret, issuer, time.Now().Add(-1*time.Hour))
	req := httptest.NewRequest("GET", "/test/hello", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for expired token, got %d", w.Code)
	}
}

func TestM6_JWTAuth_InvalidSignature_401(t *testing.T) {
	issuer := "jwt-issuer"

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	cred := &credential.Credential{
		ID: uuid.New(), ConsumerID: uuid.New(),
		Type: credential.TypeJWTAuth, Key: issuer, Secret: "correct-secret", Enabled: true,
	}
	snap.AddCredential(cred)

	jwtPlugin, _ := plugin.New("jwt-auth", map[string]interface{}{})
	snap.AddPlugin(jwtPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithAuth(t, snap)

	token := makeTestJWT("wrong-secret", issuer, time.Now().Add(1*time.Hour))
	req := httptest.NewRequest("GET", "/test/hello", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for bad signature, got %d", w.Code)
	}
}

// === Auth result propagation ===

func TestM6_KeyAuth_ResultPropagatedToBackend(t *testing.T) {
	var receivedConsumerID string

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedConsumerID = r.Header.Get("X-Consumer-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	consumerID := uuid.New()
	cred := &credential.Credential{
		ID: uuid.New(), ConsumerID: consumerID,
		Type: credential.TypeKeyAuth, Key: "my-key", Enabled: true,
	}
	snap.AddCredential(cred)

	keyAuthPlugin, _ := plugin.New("key-auth", map[string]interface{}{"hide_credentials": false})
	snap.AddPlugin(keyAuthPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithAuth(t, snap)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	req.Header.Set("apikey", "my-key")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	_ = receivedConsumerID
}

// === Secret masking ===

func TestM6_Credential_MaskSecret(t *testing.T) {
	cred := &credential.Credential{
		Secret: "super-secret-value",
	}
	masked := cred.MaskSecret()
	if masked == "super-secret-value" {
		t.Error("MaskSecret should not return the original secret")
	}
	if !strings.Contains(masked, "****") {
		t.Errorf("MaskSecret should contain ****, got: %s", masked)
	}
}

func TestM6_Credential_MaskKey(t *testing.T) {
	cred := &credential.Credential{
		Key: "my-api-key-12345",
	}
	masked := cred.MaskKey()
	if masked == "my-api-key-12345" {
		t.Error("MaskKey should not return the original key")
	}
	if !strings.Contains(masked, "****") {
		t.Errorf("MaskKey should contain ****, got: %s", masked)
	}
}

// === Route-scoped auth ===

func TestM6_KeyAuth_RouteScoped(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	protectedRoute, _ := route.New(svc.ID)
	protectedRoute.AddPath("/protected")
	protectedRoute.AddMethod("GET")
	snap.AddRoute(protectedRoute)

	publicRoute, _ := route.New(svc.ID)
	publicRoute.AddPath("/public")
	publicRoute.AddMethod("GET")
	snap.AddRoute(publicRoute)

	cred := &credential.Credential{
		ID: uuid.New(), ConsumerID: uuid.New(),
		Type: credential.TypeKeyAuth, Key: "my-key", Enabled: true,
	}
	snap.AddCredential(cred)

	// key-auth only on protected route
	keyAuthPlugin, _ := plugin.New("key-auth", map[string]interface{}{})
	keyAuthPlugin.RouteID = &protectedRoute.ID
	snap.AddPlugin(keyAuthPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithAuth(t, snap)

	// Public route should pass without auth
	req1 := httptest.NewRequest("GET", "/public/hello", nil)
	w1 := httptest.NewRecorder()
	p.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("Public route: expected 200, got %d", w1.Code)
	}

	// Protected route should require auth
	req2 := httptest.NewRequest("GET", "/protected/hello", nil)
	w2 := httptest.NewRecorder()
	p.ServeHTTP(w2, req2)

	if w2.Code != http.StatusUnauthorized {
		t.Errorf("Protected route without key: expected 401, got %d", w2.Code)
	}

	// Protected route with valid key should pass
	req3 := httptest.NewRequest("GET", "/protected/hello", nil)
	req3.Header.Set("apikey", "my-key")
	w3 := httptest.NewRecorder()
	p.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("Protected route with key: expected 200, got %d: %s", w3.Code, w3.Body.String())
	}
}

// === Helper function ===

func formatAuthError(msg string) string {
	return fmt.Sprintf(`{"message":"%s"}`, msg)
}
