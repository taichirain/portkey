package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/domain/credential"
)

const (
	JWTAuthPluginName = "jwt-auth"
)

type JWTAuthConfig struct {
	KeyClaimName           string   `json:"key_claim_name"`
	SecretIsBase64         bool     `json:"secret_is_base64"`
	ClaimsToVerify         []string `json:"claims_to_verify"`
	Algorithm              string   `json:"algorithm"`
	HideCredentials        bool     `json:"hide_credentials"`
	Anonymous              *string  `json:"anonymous"`
	RunOnPreflight         bool     `json:"run_on_preflight"`
	MaximumExpiration      *int64   `json:"maximum_expiration"`
}

type JWTAuthPlugin struct {
	config            *JWTAuthConfig
	credentialFetcher CredentialFetcher
}

type JWTClaims struct {
	Issuer    string                 `json:"iss,omitempty"`
	Subject   string                 `json:"sub,omitempty"`
	Audience  interface{}            `json:"aud,omitempty"`
	ExpiresAt int64                  `json:"exp,omitempty"`
	NotBefore int64                  `json:"nbf,omitempty"`
	IssuedAt  int64                  `json:"iat,omitempty"`
	ID        string                 `json:"jti,omitempty"`
	Custom    map[string]interface{} `json:"-"`
}

func (c *JWTClaims) Valid() error {
	now := time.Now().Unix()

	if c.ExpiresAt > 0 && now > c.ExpiresAt {
		return errors.New("token is expired")
	}

	if c.NotBefore > 0 && now < c.NotBefore {
		return errors.New("token is not yet valid")
	}

	return nil
}

func NewJWTAuthFactory() plugin.PluginFactory {
	return &jwtAuthFactory{}
}

type jwtAuthFactory struct{}

func (f *jwtAuthFactory) Name() string {
	return JWTAuthPluginName
}

func (f *jwtAuthFactory) Create(config map[string]interface{}) (plugin.Plugin, error) {
	cfg := parseJWTAuthConfig(config)
	return &JWTAuthPlugin{
		config: cfg,
	}, nil
}

func parseJWTAuthConfig(config map[string]interface{}) *JWTAuthConfig {
	cfg := &JWTAuthConfig{
		KeyClaimName:    "iss",
		SecretIsBase64:  false,
		ClaimsToVerify:  []string{"exp"},
		Algorithm:       "HS256",
		HideCredentials: true,
		RunOnPreflight:  false,
	}

	if keyClaimName, ok := config["key_claim_name"].(string); ok {
		cfg.KeyClaimName = keyClaimName
	}
	if secretIsBase64, ok := config["secret_is_base64"].(bool); ok {
		cfg.SecretIsBase64 = secretIsBase64
	}
	if claimsToVerify, ok := config["claims_to_verify"].([]interface{}); ok {
		claims := make([]string, 0, len(claimsToVerify))
		for _, c := range claimsToVerify {
			if claim, ok := c.(string); ok {
				claims = append(claims, claim)
			}
		}
		if len(claims) > 0 {
			cfg.ClaimsToVerify = claims
		}
	}
	if algorithm, ok := config["algorithm"].(string); ok {
		cfg.Algorithm = algorithm
	}
	if hideCredentials, ok := config["hide_credentials"].(bool); ok {
		cfg.HideCredentials = hideCredentials
	}
	if anonymous, ok := config["anonymous"].(string); ok {
		cfg.Anonymous = &anonymous
	}
	if runOnPreflight, ok := config["run_on_preflight"].(bool); ok {
		cfg.RunOnPreflight = runOnPreflight
	}
	if maxExp, ok := config["maximum_expiration"].(float64); ok {
		exp := int64(maxExp)
		cfg.MaximumExpiration = &exp
	}

	return cfg
}

func (p *JWTAuthPlugin) Name() string {
	return JWTAuthPluginName
}

func (p *JWTAuthPlugin) SetCredentialFetcher(fetcher CredentialFetcher) {
	p.credentialFetcher = fetcher
}

func (p *JWTAuthPlugin) OnRequest(ctx *plugin.PluginContext) error {
	if !p.config.RunOnPreflight && ctx.Request.Method == http.MethodOptions {
		return nil
	}

	token := p.extractToken(ctx.Request)
	if token == "" {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "No JWT found in request")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "Invalid JWT format")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "Invalid JWT header")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "Invalid JWT payload")
	}

	var header map[string]interface{}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "Invalid JWT header format")
	}

	var claims JWTClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "Invalid JWT payload format")
	}

	if err := claims.Valid(); err != nil {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, err.Error())
	}

	if p.config.MaximumExpiration != nil {
		maxExp := *p.config.MaximumExpiration
		if claims.ExpiresAt > 0 {
			now := time.Now().Unix()
			if claims.ExpiresAt-now > maxExp {
				if p.config.Anonymous != nil {
					ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
					return nil
				}
				return p.unauthorized(ctx, "Token expiration exceeds maximum allowed")
			}
		}
	}

	keyValue := p.getKeyFromClaims(&claims)
	if keyValue == "" {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, fmt.Sprintf("Key claim '%s' not found in token", p.config.KeyClaimName))
	}

	credFetcher, ok := ctx.GetAttribute("credential_fetcher").(CredentialFetcher)
	if !ok {
		return p.unauthorized(ctx, "Credential fetcher not configured")
	}

	cred, err := credFetcher.GetByKey(keyValue)
	if err != nil || cred == nil {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "Invalid credential key")
	}

	if !cred.Enabled {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "Credential is disabled")
	}

	if !cred.IsJWTAuth() {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "Invalid credential type")
	}

	if err := p.verifySignature(token, cred); err != nil {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "Signature verification failed: "+err.Error())
	}

	if err := p.verifyClaims(&claims); err != nil {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "Claim verification failed: "+err.Error())
	}

	ctx.SetConsumerID(cred.ConsumerID)
	ctx.SetAttribute("auth_consumer_id", cred.ConsumerID)
	ctx.SetAttribute("auth_credential_id", cred.ID)
	ctx.SetAttribute("auth_credential_type", string(cred.Type))
	ctx.SetAttribute("auth_credential_key", cred.Key)
	ctx.SetAttribute("auth_jwt_claims", claims)
	ctx.SetAttribute("auth_jwt_token", token)

	if p.config.HideCredentials {
		p.hideCredentials(ctx)
	}

	return nil
}

func (p *JWTAuthPlugin) getKeyFromClaims(claims *JWTClaims) string {
	switch p.config.KeyClaimName {
	case "iss":
		return claims.Issuer
	case "sub":
		return claims.Subject
	case "jti":
		return claims.ID
	default:
		return ""
	}
}

func (p *JWTAuthPlugin) extractToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
	}

	if cookie, err := r.Cookie("token"); err == nil {
		return cookie.Value
	}

	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	return ""
}

func (p *JWTAuthPlugin) verifySignature(token string, cred *credential.Credential) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("invalid token format")
	}

	signingInput := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return errors.New("invalid signature encoding")
	}

	algorithm := p.config.Algorithm
	if cred.Algorithm != "" {
		algorithm = cred.Algorithm
	}

	switch algorithm {
	case "HS256", "HS384", "HS512":
		return p.verifyHMAC(signingInput, signature, cred.Secret, algorithm)
	case "RS256", "RS384", "RS512":
		return p.verifyRSA(signingInput, signature, cred.Secret, algorithm)
	case "ES256", "ES384", "ES512":
		return p.verifyECDSA(signingInput, signature, cred.Secret, algorithm)
	default:
		return fmt.Errorf("unsupported algorithm: %s", algorithm)
	}
}

func (p *JWTAuthPlugin) verifyHMAC(signingInput string, signature []byte, secret string, algorithm string) error {
	var key []byte
	var err error

	if p.config.SecretIsBase64 {
		key, err = base64.StdEncoding.DecodeString(secret)
		if err != nil {
			return errors.New("invalid base64 secret")
		}
	} else {
		key = []byte(secret)
	}

	var hash crypto.Hash
	switch algorithm {
	case "HS256":
		hash = crypto.SHA256
	case "HS384":
		hash = crypto.SHA384
	case "HS512":
		hash = crypto.SHA512
	default:
		return fmt.Errorf("unsupported HMAC algorithm: %s", algorithm)
	}

	h := hmac.New(hash.New, key)
	h.Write([]byte(signingInput))
	expectedMAC := h.Sum(nil)

	if len(signature) != len(expectedMAC) {
		return errors.New("signature length mismatch")
	}

	if !hmac.Equal(signature, expectedMAC) {
		return errors.New("signature mismatch")
	}

	return nil
}

func (p *JWTAuthPlugin) verifyRSA(signingInput string, signature []byte, secret string, algorithm string) error {
	block, _ := pem.Decode([]byte(secret))
	if block == nil {
		return errors.New("invalid PEM format")
	}

	var pubKey *rsa.PublicKey
	var err error

	if strings.Contains(secret, "BEGIN PUBLIC KEY") {
		pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return errors.New("invalid public key")
		}
		var ok bool
		pubKey, ok = pubInterface.(*rsa.PublicKey)
		if !ok {
			return errors.New("not an RSA public key")
		}
	} else if strings.Contains(secret, "BEGIN RSA PUBLIC KEY") {
		pubKey, err = x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return errors.New("invalid RSA public key")
		}
	} else {
		return errors.New("unsupported key format")
	}

	var hash crypto.Hash
	var hashed []byte

	switch algorithm {
	case "RS256":
		hash = crypto.SHA256
		h := sha256.Sum256([]byte(signingInput))
		hashed = h[:]
	case "RS384":
		hash = crypto.SHA384
		h := sha512.Sum384([]byte(signingInput))
		hashed = h[:]
	case "RS512":
		hash = crypto.SHA512
		h := sha512.Sum512([]byte(signingInput))
		hashed = h[:]
	default:
		return fmt.Errorf("unsupported RSA algorithm: %s", algorithm)
	}

	if err := rsa.VerifyPKCS1v15(pubKey, hash, hashed, signature); err != nil {
		return errors.New("RSA signature verification failed")
	}

	return nil
}

func (p *JWTAuthPlugin) verifyECDSA(signingInput string, signature []byte, secret string, algorithm string) error {
	block, _ := pem.Decode([]byte(secret))
	if block == nil {
		return errors.New("invalid PEM format")
	}

	var pubKey *ecdsa.PublicKey

	if strings.Contains(secret, "BEGIN PUBLIC KEY") {
		pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return errors.New("invalid public key")
		}
		var ok bool
		pubKey, ok = pubInterface.(*ecdsa.PublicKey)
		if !ok {
			return errors.New("not an ECDSA public key")
		}
	} else {
		return errors.New("unsupported key format")
	}

	var hashed []byte
	var keySize int

	switch algorithm {
	case "ES256":
		h := sha256.Sum256([]byte(signingInput))
		hashed = h[:]
		keySize = 32
	case "ES384":
		h := sha512.Sum384([]byte(signingInput))
		hashed = h[:]
		keySize = 48
	case "ES512":
		h := sha512.Sum512([]byte(signingInput))
		hashed = h[:]
		keySize = 66
	default:
		return fmt.Errorf("unsupported ECDSA algorithm: %s", algorithm)
	}

	if len(signature) != 2*keySize {
		return errors.New("invalid ECDSA signature length")
	}

	r := big.NewInt(0).SetBytes(signature[:keySize])
	s := big.NewInt(0).SetBytes(signature[keySize:])

	if !ecdsa.Verify(pubKey, hashed, r, s) {
		return errors.New("ECDSA signature verification failed")
	}

	return nil
}

func (p *JWTAuthPlugin) verifyClaims(claims *JWTClaims) error {
	for _, claim := range p.config.ClaimsToVerify {
		switch claim {
		case "exp":
			if claims.ExpiresAt == 0 {
				return errors.New("'exp' claim is required but missing")
			}
		case "nbf":
			if claims.NotBefore == 0 {
				return errors.New("'nbf' claim is required but missing")
			}
		case "iat":
			if claims.IssuedAt == 0 {
				return errors.New("'iat' claim is required but missing")
			}
		}
	}
	return nil
}

func (p *JWTAuthPlugin) hideCredentials(ctx *plugin.PluginContext) {
	ctx.Request.Header.Del("Authorization")
	if cookie, err := ctx.Request.Cookie("token"); err == nil {
		cookie.MaxAge = -1
	}

	q := ctx.Request.URL.Query()
	q.Del("token")
	ctx.Request.URL.RawQuery = q.Encode()
}

func (p *JWTAuthPlugin) unauthorized(ctx *plugin.PluginContext, message string) error {
	ctx.ResponseWriter.Header().Set("Content-Type", "application/json")
	ctx.ResponseWriter.WriteHeader(http.StatusUnauthorized)
	ctx.ResponseWriter.Write([]byte(`{"message":"` + message + `"}`))
	ctx.ShortCircuit()
	return plugin.NewPluginError(p.Name(), "unauthorized", nil)
}

func (p *JWTAuthPlugin) OnResponse(ctx *plugin.PluginContext, resp *http.Response) error {
	return nil
}

func (p *JWTAuthPlugin) OnError(ctx *plugin.PluginContext, err error) error {
	return nil
}
