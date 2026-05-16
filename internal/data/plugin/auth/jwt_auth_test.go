package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/domain/credential"
)

func makeJWT(header map[string]interface{}, claims map[string]interface{}, secret string) string {
	hBytes, _ := json.Marshal(header)
	pBytes, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(hBytes)
	payloadB64 := base64.RawURLEncoding.EncodeToString(pBytes)

	signingInput := headerB64 + "." + payloadB64
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return headerB64 + "." + payloadB64 + "." + signature
}

func validJWT(secret, issuer string) string {
	return makeJWT(
		map[string]interface{}{"alg": "HS256", "typ": "JWT"},
		map[string]interface{}{
			"iss": issuer,
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		secret,
	)
}

func expiredJWT(secret, issuer string) string {
	return makeJWT(
		map[string]interface{}{"alg": "HS256", "typ": "JWT"},
		map[string]interface{}{
			"iss": issuer,
			"exp": time.Now().Add(-1 * time.Hour).Unix(),
		},
		secret,
	)
}

func jwtContext(method, path, authHeader string) (*pluginPkg.PluginContext, *httptest.ResponseRecorder) {
	r := httptest.NewRequest(method, path, nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	ctx := pluginPkg.NewPluginContext(w, r, "test-trace")
	return ctx, w
}

func setupJWTPlugin(t *testing.T, config map[string]interface{}, fetcher CredentialFetcher) *JWTAuthPlugin {
	t.Helper()
	factory := NewJWTAuthFactory()
	p, err := factory.Create(config)
	if err != nil {
		t.Fatalf("factory.Create error: %v", err)
	}
	jp := p.(*JWTAuthPlugin)
	if fetcher != nil {
		jp.SetCredentialFetcher(fetcher)
	}
	return jp
}

// --- Success path ---

func TestJWTAuth_Success_ValidToken(t *testing.T) {
	secret := "test-secret"
	issuer := "test-issuer"
	consumerID := uuid.New()

	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: consumerID,
				Type: credential.TypeJWTAuth, Key: issuer, Secret: secret, Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{}, fetcher)
	token := validJWT(secret, issuer)
	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	err := p.OnRequest(ctx)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ctx.GetAttribute("auth_consumer_id") != consumerID {
		t.Errorf("auth_consumer_id not set")
	}
	if ctx.GetAttribute("auth_jwt_token") != token {
		t.Errorf("auth_jwt_token not set")
	}
}

func TestJWTAuth_Success_FromCookie(t *testing.T) {
	secret := "s"
	issuer := "iss-cookie"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: issuer, Secret: secret, Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{}, fetcher)
	token := validJWT(secret, issuer)

	r := httptest.NewRequest("GET", "/test", nil)
	r.AddCookie(&http.Cookie{Name: "token", Value: token})
	w := httptest.NewRecorder()
	ctx := pluginPkg.NewPluginContext(w, r, "trace")
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err != nil {
		t.Fatalf("expected success from cookie, got: %v", err)
	}
}

func TestJWTAuth_Success_FromQueryParam(t *testing.T) {
	secret := "s"
	issuer := "iss-query"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: issuer, Secret: secret, Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{}, fetcher)
	token := validJWT(secret, issuer)

	r := httptest.NewRequest("GET", "/test?token="+token, nil)
	w := httptest.NewRecorder()
	ctx := pluginPkg.NewPluginContext(w, r, "trace")
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err != nil {
		t.Fatalf("expected success from query, got: %v", err)
	}
}

// --- Failure paths ---

func TestJWTAuth_MissingToken_401(t *testing.T) {
	p := setupJWTPlugin(t, map[string]interface{}{}, nil)
	ctx, w := jwtContext("GET", "/test", "")

	err := p.OnRequest(ctx)
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	if !ctx.IsShortCircuited() {
		t.Error("expected short-circuit")
	}
}

func TestJWTAuth_InvalidFormat_401(t *testing.T) {
	p := setupJWTPlugin(t, map[string]interface{}{}, nil)
	ctx, w := jwtContext("GET", "/test", "Bearer not.a.jwt.token")

	err := p.OnRequest(ctx)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_MalformedBase64_401(t *testing.T) {
	p := setupJWTPlugin(t, map[string]interface{}{}, nil)
	ctx, w := jwtContext("GET", "/test", "Bearer !!!.eyJpc3MiOiJ0ZXN0In0.sig")

	err := p.OnRequest(ctx)
	if err == nil {
		t.Fatal("expected error for malformed base64")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_ExpiredToken_401(t *testing.T) {
	secret := "test-secret"
	issuer := "test-issuer"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: issuer, Secret: secret, Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{}, fetcher)
	token := expiredJWT(secret, issuer)
	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	err := p.OnRequest(ctx)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_InvalidSignature_401(t *testing.T) {
	issuer := "test-issuer"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: issuer, Secret: "correct-secret", Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{}, fetcher)
	token := validJWT("wrong-secret", issuer)
	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	err := p.OnRequest(ctx)
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_DisabledCredential_401(t *testing.T) {
	issuer := "test-issuer"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: issuer, Secret: "s", Enabled: false,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{}, fetcher)
	token := validJWT("s", issuer)
	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err == nil {
		t.Fatal("expected error for disabled credential")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_CredentialNotFound_401(t *testing.T) {
	fetcher := &mockFetcher{creds: map[string]*credential.Credential{}}
	p := setupJWTPlugin(t, map[string]interface{}{}, fetcher)
	token := validJWT("s", "unknown-issuer")
	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err == nil {
		t.Fatal("expected error for missing credential")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_WrongCredentialType_401(t *testing.T) {
	issuer := "test-issuer"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeKeyAuth, Key: issuer, Secret: "s", Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{}, fetcher)
	token := validJWT("s", issuer)
	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err == nil {
		t.Fatal("expected error for wrong credential type")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// --- Claims verification ---

func TestJWTAuth_MissingExpClaim_WhenRequired_401(t *testing.T) {
	issuer := "iss"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: issuer, Secret: "s", Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{"claims_to_verify": []interface{}{"exp"}}, fetcher)
	// Token without exp claim
	token := makeJWT(
		map[string]interface{}{"alg": "HS256", "typ": "JWT"},
		map[string]interface{}{"iss": issuer},
		"s",
	)
	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err == nil {
		t.Fatal("expected error for missing exp claim")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_NbfClaim_FutureToken_401(t *testing.T) {
	issuer := "iss"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: issuer, Secret: "s", Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{}, fetcher)
	token := makeJWT(
		map[string]interface{}{"alg": "HS256", "typ": "JWT"},
		map[string]interface{}{
			"iss": issuer,
			"exp": time.Now().Add(1 * time.Hour).Unix(),
			"nbf": time.Now().Add(1 * time.Hour).Unix(),
		},
		"s",
	)
	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err == nil {
		t.Fatal("expected error for nbf in future")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// --- MaximumExpiration ---

func TestJWTAuth_MaximumExpiration_Exceeded_401(t *testing.T) {
	issuer := "iss"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: issuer, Secret: "s", Enabled: true,
			},
		},
	}

	maxExp := int64(60) // 60 seconds max
	p := setupJWTPlugin(t, map[string]interface{}{"maximum_expiration": float64(maxExp)}, fetcher)
	token := makeJWT(
		map[string]interface{}{"alg": "HS256", "typ": "JWT"},
		map[string]interface{}{
			"iss": issuer,
			"exp": time.Now().Add(1 * time.Hour).Unix(), // 3600s > 60s
		},
		"s",
	)
	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err == nil {
		t.Fatal("expected error for exceeding max expiration")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// --- Anonymous ---

func TestJWTAuth_MissingToken_Anonymous(t *testing.T) {
	p := setupJWTPlugin(t, map[string]interface{}{"anonymous": "anon-id"}, nil)
	ctx, w := jwtContext("GET", "/test", "")

	err := p.OnRequest(ctx)
	if err != nil {
		t.Fatalf("expected anonymous fallback, got: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with anonymous, got %d", w.Code)
	}
	if ctx.GetAttribute("auth_anonymous") != "anon-id" {
		t.Errorf("expected auth_anonymous set")
	}
}

// --- RunOnPreflight ---

func TestJWTAuth_SkipPreflight_ByDefault(t *testing.T) {
	p := setupJWTPlugin(t, map[string]interface{}{}, nil)
	r := httptest.NewRequest("OPTIONS", "/test", nil)
	w := httptest.NewRecorder()
	ctx := pluginPkg.NewPluginContext(w, r, "trace")

	err := p.OnRequest(ctx)
	if err != nil {
		t.Fatalf("expected preflight to be skipped, got: %v", err)
	}
	if w.Code == http.StatusUnauthorized {
		t.Errorf("preflight should pass without auth, got 401")
	}
}

func TestJWTAuth_RunOnPreflight_RequiresAuth(t *testing.T) {
	p := setupJWTPlugin(t, map[string]interface{}{"run_on_preflight": true}, nil)
	r := httptest.NewRequest("OPTIONS", "/test", nil)
	w := httptest.NewRecorder()
	ctx := pluginPkg.NewPluginContext(w, r, "trace")

	err := p.OnRequest(ctx)
	if err == nil {
		t.Fatal("expected auth required on preflight")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// --- HideCredentials ---

func TestJWTAuth_HideCredentials(t *testing.T) {
	secret := "s"
	issuer := "iss"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: issuer, Secret: secret, Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{"hide_credentials": true}, fetcher)
	token := validJWT(secret, issuer)
	ctx, _ := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	_ = p.OnRequest(ctx)

	if ctx.Request.Header.Get("Authorization") != "" {
		t.Error("expected Authorization header to be removed")
	}
}

// --- Factory ---

func TestJWTAuth_FactoryName(t *testing.T) {
	factory := NewJWTAuthFactory()
	if factory.Name() != "jwt-auth" {
		t.Errorf("expected 'jwt-auth', got '%s'", factory.Name())
	}
}

func TestJWTAuth_DefaultConfig(t *testing.T) {
	p := setupJWTPlugin(t, map[string]interface{}{}, nil)
	if p.config.KeyClaimName != "iss" {
		t.Errorf("default KeyClaimName should be 'iss'")
	}
	if p.config.Algorithm != "HS256" {
		t.Errorf("default Algorithm should be 'HS256'")
	}
	if len(p.config.ClaimsToVerify) != 1 || p.config.ClaimsToVerify[0] != "exp" {
		t.Errorf("default ClaimsToVerify should be [exp]")
	}
}

// --- key claim extraction ---

func TestJWTAuth_KeyClaim_Sub(t *testing.T) {
	secret := "s"
	subValue := "user-123"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			subValue: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: subValue, Secret: secret, Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{"key_claim_name": "sub"}, fetcher)
	token := makeJWT(
		map[string]interface{}{"alg": "HS256", "typ": "JWT"},
		map[string]interface{}{
			"iss": "some-issuer",
			"sub": subValue,
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		secret,
	)
	ctx, _ := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err != nil {
		t.Fatalf("expected success with sub claim, got: %v", err)
	}
}

func TestJWTAuth_KeyClaim_Missing_401(t *testing.T) {
	p := setupJWTPlugin(t, map[string]interface{}{"key_claim_name": "sub"}, nil)
	token := makeJWT(
		map[string]interface{}{"alg": "HS256", "typ": "JWT"},
		map[string]interface{}{
			"iss": "some-issuer",
			"exp": time.Now().Add(1 * time.Hour).Unix(),
			// no "sub"
		},
		"s",
	)
	ctx, w := jwtContext("GET", "/test", "Bearer "+token)

	// No fetcher needed - fails at key claim extraction
	err := p.OnRequest(ctx)
	if err == nil {
		t.Fatal("expected error for missing key claim")
	}
	if !strings.Contains(fmt.Sprint(err), "unauthorized") {
		t.Errorf("expected unauthorized error")
	}
	_ = w
}

func makeJWTRSA(header map[string]interface{}, claims map[string]interface{}, privKey *rsa.PrivateKey, hash crypto.Hash) string {
	hBytes, _ := json.Marshal(header)
	pBytes, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(hBytes)
	payloadB64 := base64.RawURLEncoding.EncodeToString(pBytes)

	signingInput := headerB64 + "." + payloadB64
	hasher := hash.New()
	hasher.Write([]byte(signingInput))
	hashed := hasher.Sum(nil)

	signature, _ := rsa.SignPKCS1v15(rand.Reader, privKey, hash, hashed)
	sigB64 := base64.RawURLEncoding.EncodeToString(signature)

	return headerB64 + "." + payloadB64 + "." + sigB64
}

func makeJWTECDSA(header map[string]interface{}, claims map[string]interface{}, privKey *ecdsa.PrivateKey, hash crypto.Hash, keySize int) string {
	hBytes, _ := json.Marshal(header)
	pBytes, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(hBytes)
	payloadB64 := base64.RawURLEncoding.EncodeToString(pBytes)

	signingInput := headerB64 + "." + payloadB64
	hasher := hash.New()
	hasher.Write([]byte(signingInput))
	hashed := hasher.Sum(nil)

	r, s, _ := ecdsa.Sign(rand.Reader, privKey, hashed)

	signature := make([]byte, 2*keySize)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(signature[keySize-len(rBytes):keySize], rBytes)
	copy(signature[2*keySize-len(sBytes):], sBytes)

	sigB64 := base64.RawURLEncoding.EncodeToString(signature)

	return headerB64 + "." + payloadB64 + "." + sigB64
}

func generateRSAKeyPair(bits int) (*rsa.PrivateKey, string, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, "", err
	}

	pubKeyBytes, _ := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	return privKey, string(pubKeyPEM), nil
}

func generateECDSAKeyPair(curve elliptic.Curve) (*ecdsa.PrivateKey, string, error) {
	privKey, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return nil, "", err
	}

	pubKeyBytes, _ := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	return privKey, string(pubKeyPEM), nil
}

func TestJWTAuth_RS256_Success(t *testing.T) {
	privKey, pubKeyPEM, err := generateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	issuer := "rsa-issuer"
	consumerID := uuid.New()

	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: consumerID,
				Type: credential.TypeJWTAuth, Key: issuer, Secret: pubKeyPEM, Algorithm: "RS256", Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{"algorithm": "RS256"}, fetcher)

	token := makeJWTRSA(
		map[string]interface{}{"alg": "RS256", "typ": "JWT"},
		map[string]interface{}{
			"iss": issuer,
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		privKey,
		crypto.SHA256,
	)

	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err != nil {
		t.Fatalf("expected RS256 success, got: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestJWTAuth_RS384_Success(t *testing.T) {
	privKey, pubKeyPEM, err := generateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	issuer := "rsa384-issuer"
	consumerID := uuid.New()

	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: consumerID,
				Type: credential.TypeJWTAuth, Key: issuer, Secret: pubKeyPEM, Algorithm: "RS384", Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{"algorithm": "RS384"}, fetcher)

	token := makeJWTRSA(
		map[string]interface{}{"alg": "RS384", "typ": "JWT"},
		map[string]interface{}{
			"iss": issuer,
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		privKey,
		crypto.SHA384,
	)

	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err != nil {
		t.Fatalf("expected RS384 success, got: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestJWTAuth_RS512_Success(t *testing.T) {
	privKey, pubKeyPEM, err := generateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	issuer := "rsa512-issuer"
	consumerID := uuid.New()

	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: consumerID,
				Type: credential.TypeJWTAuth, Key: issuer, Secret: pubKeyPEM, Algorithm: "RS512", Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{"algorithm": "RS512"}, fetcher)

	token := makeJWTRSA(
		map[string]interface{}{"alg": "RS512", "typ": "JWT"},
		map[string]interface{}{
			"iss": issuer,
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		privKey,
		crypto.SHA512,
	)

	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err != nil {
		t.Fatalf("expected RS512 success, got: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestJWTAuth_RSA_InvalidSignature_401(t *testing.T) {
	_, pubKeyPEM, err := generateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	wrongPrivKey, _, err := generateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("failed to generate wrong RSA key: %v", err)
	}

	issuer := "rsa-issuer"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: issuer, Secret: pubKeyPEM, Algorithm: "RS256", Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{"algorithm": "RS256"}, fetcher)

	token := makeJWTRSA(
		map[string]interface{}{"alg": "RS256", "typ": "JWT"},
		map[string]interface{}{
			"iss": issuer,
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		wrongPrivKey,
		crypto.SHA256,
	)

	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err == nil {
		t.Fatal("expected error for invalid RSA signature")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_ES256_Success(t *testing.T) {
	privKey, pubKeyPEM, err := generateECDSAKeyPair(elliptic.P256())
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}

	issuer := "es256-issuer"
	consumerID := uuid.New()

	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: consumerID,
				Type: credential.TypeJWTAuth, Key: issuer, Secret: pubKeyPEM, Algorithm: "ES256", Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{"algorithm": "ES256"}, fetcher)

	token := makeJWTECDSA(
		map[string]interface{}{"alg": "ES256", "typ": "JWT"},
		map[string]interface{}{
			"iss": issuer,
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		privKey,
		crypto.SHA256,
		32,
	)

	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err != nil {
		t.Fatalf("expected ES256 success, got: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestJWTAuth_ES384_Success(t *testing.T) {
	privKey, pubKeyPEM, err := generateECDSAKeyPair(elliptic.P384())
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}

	issuer := "es384-issuer"
	consumerID := uuid.New()

	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: consumerID,
				Type: credential.TypeJWTAuth, Key: issuer, Secret: pubKeyPEM, Algorithm: "ES384", Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{"algorithm": "ES384"}, fetcher)

	token := makeJWTECDSA(
		map[string]interface{}{"alg": "ES384", "typ": "JWT"},
		map[string]interface{}{
			"iss": issuer,
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		privKey,
		crypto.SHA384,
		48,
	)

	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err != nil {
		t.Fatalf("expected ES384 success, got: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestJWTAuth_ES512_Success(t *testing.T) {
	privKey, pubKeyPEM, err := generateECDSAKeyPair(elliptic.P521())
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}

	issuer := "es512-issuer"
	consumerID := uuid.New()

	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: consumerID,
				Type: credential.TypeJWTAuth, Key: issuer, Secret: pubKeyPEM, Algorithm: "ES512", Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{"algorithm": "ES512"}, fetcher)

	token := makeJWTECDSA(
		map[string]interface{}{"alg": "ES512", "typ": "JWT"},
		map[string]interface{}{
			"iss": issuer,
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		privKey,
		crypto.SHA512,
		66,
	)

	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err != nil {
		t.Fatalf("expected ES512 success, got: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestJWTAuth_ECDSA_InvalidSignature_401(t *testing.T) {
	_, pubKeyPEM, err := generateECDSAKeyPair(elliptic.P256())
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}

	wrongPrivKey, _, err := generateECDSAKeyPair(elliptic.P256())
	if err != nil {
		t.Fatalf("failed to generate wrong ECDSA key: %v", err)
	}

	issuer := "es256-issuer"
	fetcher := &mockFetcher{
		creds: map[string]*credential.Credential{
			issuer: {
				ID: uuid.New(), ConsumerID: uuid.New(),
				Type: credential.TypeJWTAuth, Key: issuer, Secret: pubKeyPEM, Algorithm: "ES256", Enabled: true,
			},
		},
	}

	p := setupJWTPlugin(t, map[string]interface{}{"algorithm": "ES256"}, fetcher)

	token := makeJWTECDSA(
		map[string]interface{}{"alg": "ES256", "typ": "JWT"},
		map[string]interface{}{
			"iss": issuer,
			"exp": time.Now().Add(1 * time.Hour).Unix(),
		},
		wrongPrivKey,
		crypto.SHA256,
		32,
	)

	ctx, w := jwtContext("GET", "/test", "Bearer "+token)
	ctx.SetAttribute("credential_fetcher", fetcher)

	if err := p.OnRequest(ctx); err == nil {
		t.Fatal("expected error for invalid ECDSA signature")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
