package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/domain/credential"
)

type mockFetcher struct {
	creds map[string]*credential.Credential
}

func (f *mockFetcher) GetByKey(key string) (*credential.Credential, error) {
	return f.creds[key], nil
}

func newKeyAuthContext(method, path string, headers map[string]string) (*pluginPkg.PluginContext, *httptest.ResponseRecorder) {
	r := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	ctx := pluginPkg.NewPluginContext(w, r, "test-trace")
	return ctx, w
}

func setupKeyAuthPlugin(t *testing.T, config map[string]interface{}, fetcher CredentialFetcher) *KeyAuthPlugin {
	t.Helper()
	factory := NewKeyAuthFactory()
	p, err := factory.Create(config)
	if err != nil {
		t.Fatalf("factory.Create error: %v", err)
	}
	kap := p.(*KeyAuthPlugin)
	if fetcher != nil {
		kap.SetCredentialFetcher(fetcher)
	}
	return kap
}

// --- Success path ---

func TestKeyAuth_Success_FromHeader(t *testing.T) {
	consumerID := uuid.New()
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			"valid-key": {
				ID:         uuid.New(),
				ConsumerID: consumerID,
				Type:       credential.TypeKeyAuth,
				Key:        "valid-key",
				Enabled:    true,
			},
		},
	}

	p := setupKeyAuthPlugin(t, map[string]interface{}{}, fetcher)
	ctx, w := newKeyAuthContext("GET", "/test", map[string]string{"apikey": "valid-key"})
	ctx.SetAttribute("credential_fetcher", fetcher)

	err := p.OnRequest(ctx)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ctx.GetAttribute("auth_consumer_id") != consumerID {
		t.Errorf("auth_consumer_id not set correctly")
	}
	if ctx.GetAttribute("auth_credential_key") != "valid-key" {
		t.Errorf("auth_credential_key not set correctly")
	}
}

func TestKeyAuth_Success_FromXHeader(t *testing.T) {
	consumerID := uuid.New()
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			"my-key": {
				ID: uuid.New(), ConsumerID: consumerID,
				Type: credential.TypeKeyAuth, Key: "my-key", Enabled: true,
			},
		},
	}

	p := setupKeyAuthPlugin(t, map[string]interface{}{}, fetcher)
	ctx, _ := newKeyAuthContext("GET", "/test", map[string]string{"X-apikey": "my-key"})
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}

func TestKeyAuth_Success_FromQuery(t *testing.T) {
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			"qkey": {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeKeyAuth, Key: "qkey", Enabled: true,
			},
		},
	}

	p := setupKeyAuthPlugin(t, map[string]interface{}{"key_in_header": false}, fetcher)
	r := httptest.NewRequest("GET", "/test?apikey=qkey", nil)
	w := httptest.NewRecorder()
	ctx := pluginPkg.NewPluginContext(w, r, "trace")
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}

func TestKeyAuth_Success_CustomKeyNames(t *testing.T) {
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			"token-abc": {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeKeyAuth, Key: "token-abc", Enabled: true,
			},
		},
	}

	p := setupKeyAuthPlugin(t, map[string]interface{}{
		"key_names": []interface{}{"X-Custom-Key"},
	}, fetcher)
	ctx, _ := newKeyAuthContext("GET", "/test", map[string]string{"X-Custom-Key": "token-abc"})
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}

// --- Failure paths ---

func TestKeyAuth_MissingKey_401(t *testing.T) {
	p := setupKeyAuthPlugin(t, map[string]interface{}{}, nil)
	ctx, w := newKeyAuthContext("GET", "/test", nil)

	err := p.OnRequest(ctx)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	if !ctx.IsShortCircuited() {
		t.Error("expected short-circuit")
	}
}

func TestKeyAuth_InvalidKey_401(t *testing.T) {
	fetcher := &mockFetcher{creds: map[string]*credential.Credential{}}
	p := setupKeyAuthPlugin(t, map[string]interface{}{}, fetcher)
	ctx, w := newKeyAuthContext("GET", "/test", map[string]string{"apikey": "wrong-key"})
	ctx.SetAttribute("credential_fetcher", fetcher)

	err := p.OnRequest(ctx)
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestKeyAuth_DisabledCredential_401(t *testing.T) {
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			"disabled-key": {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeKeyAuth, Key: "disabled-key", Enabled: false,
			},
		},
	}
	p := setupKeyAuthPlugin(t, map[string]interface{}{}, fetcher)
	ctx, w := newKeyAuthContext("GET", "/test", map[string]string{"apikey": "disabled-key"})
	ctx.SetAttribute("credential_fetcher", fetcher)

	err := p.OnRequest(ctx)
	if err == nil {
		t.Fatal("expected error for disabled credential")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestKeyAuth_WrongCredentialType_401(t *testing.T) {
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			"jwt-key": {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: "jwt-key", Enabled: true,
			},
		},
	}
	p := setupKeyAuthPlugin(t, map[string]interface{}{}, fetcher)
	ctx, w := newKeyAuthContext("GET", "/test", map[string]string{"apikey": "jwt-key"})
	ctx.SetAttribute("credential_fetcher", fetcher)

	err := p.OnRequest(ctx)
	if err == nil {
		t.Fatal("expected error for wrong credential type")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestKeyAuth_NoFetcher_401(t *testing.T) {
	p := setupKeyAuthPlugin(t, map[string]interface{}{}, nil)
	ctx, w := newKeyAuthContext("GET", "/test", map[string]string{"apikey": "some-key"})
	// Don't set credential_fetcher attribute

	err := p.OnRequest(ctx)
	if err == nil {
		t.Fatal("expected error when no fetcher configured")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// --- Anonymous ---

func TestKeyAuth_MissingKey_Anonymous(t *testing.T) {
	p := setupKeyAuthPlugin(t, map[string]interface{}{"anonymous": "anon-consumer"}, nil)
	ctx, w := newKeyAuthContext("GET", "/test", nil)

	err := p.OnRequest(ctx)
	if err != nil {
		t.Fatalf("expected no error with anonymous, got: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with anonymous, got %d", w.Code)
	}
	if ctx.GetAttribute("auth_anonymous") != "anon-consumer" {
		t.Errorf("expected auth_anonymous set")
	}
}

func TestKeyAuth_InvalidKey_Anonymous(t *testing.T) {
	fetcher := &mockFetcher{creds: map[string]*credential.Credential{}}
	p := setupKeyAuthPlugin(t, map[string]interface{}{"anonymous": "anon-consumer"}, fetcher)
	ctx, _ := newKeyAuthContext("GET", "/test", map[string]string{"apikey": "bad"})
	ctx.SetAttribute("credential_fetcher", fetcher)

	err := p.OnRequest(ctx)
	if err != nil {
		t.Fatalf("expected anonymous fallback, got: %v", err)
	}
	if ctx.GetAttribute("auth_anonymous") != "anon-consumer" {
		t.Errorf("expected auth_anonymous set")
	}
}

// --- HideCredentials ---

func TestKeyAuth_HideCredentials(t *testing.T) {
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			"secret-key": {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeKeyAuth, Key: "secret-key", Enabled: true,
			},
		},
	}
	p := setupKeyAuthPlugin(t, map[string]interface{}{"hide_credentials": true}, fetcher)
	ctx, _ := newKeyAuthContext("GET", "/test", map[string]string{"apikey": "secret-key"})
	ctx.SetAttribute("credential_fetcher", fetcher)

	_ = p.OnRequest(ctx)

	if ctx.Request.Header.Get("apikey") != "" {
		t.Error("expected apikey header to be removed")
	}
}

func TestKeyAuth_HideCredentials_QueryParam(t *testing.T) {
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			"qkey": {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeKeyAuth, Key: "qkey", Enabled: true,
			},
		},
	}
	p := setupKeyAuthPlugin(t, map[string]interface{}{
		"key_in_header": false,
		"key_names":     []interface{}{"apikey"},
		"hide_credentials": true,
	}, fetcher)
	r := httptest.NewRequest("GET", "/test?apikey=qkey&other=1", nil)
	w := httptest.NewRecorder()
	ctx := pluginPkg.NewPluginContext(w, r, "trace")
	ctx.SetAttribute("credential_fetcher", fetcher)

	_ = p.OnRequest(ctx)

	raw := ctx.Request.URL.RawQuery
	if strings.Contains(raw, "apikey=") {
		t.Errorf("expected apikey query param to be removed, got raw: %s", raw)
	}
	if !strings.Contains(raw, "other=1") {
		t.Error("expected other query param to be preserved")
	}
}

// --- Factory ---

func TestKeyAuth_FactoryName(t *testing.T) {
	factory := NewKeyAuthFactory()
	if factory.Name() != "key-auth" {
		t.Errorf("expected 'key-auth', got '%s'", factory.Name())
	}
}

func TestKeyAuth_DefaultConfig(t *testing.T) {
	p := setupKeyAuthPlugin(t, map[string]interface{}{}, nil)
	if len(p.config.KeyNames) != 1 || p.config.KeyNames[0] != "apikey" {
		t.Errorf("default KeyNames should be [apikey]")
	}
	if !p.config.KeyInHeader {
		t.Error("default KeyInHeader should be true")
	}
	if !p.config.KeyInQuery {
		t.Error("default KeyInQuery should be true")
	}
}
