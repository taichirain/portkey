package auth

import (
	"net/http"
	"strings"

	"github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/domain/credential"
)

const (
	KeyAuthPluginName = "key-auth"
)

type KeyAuthConfig struct {
	KeyNames    []string `json:"key_names"`
	KeyInHeader bool     `json:"key_in_header"`
	KeyInQuery  bool     `json:"key_in_query"`
	HideCredentials bool  `json:"hide_credentials"`
	Anonymous   *string  `json:"anonymous"`
}

type KeyAuthPlugin struct {
	config        *KeyAuthConfig
	credentialFetcher CredentialFetcher
}

type CredentialFetcher interface {
	GetByKey(key string) (*credential.Credential, error)
}

func NewKeyAuthFactory() plugin.PluginFactory {
	return &keyAuthFactory{}
}

type keyAuthFactory struct{}

func (f *keyAuthFactory) Name() string {
	return KeyAuthPluginName
}

func (f *keyAuthFactory) Create(config map[string]interface{}) (plugin.Plugin, error) {
	cfg := parseKeyAuthConfig(config)
	return &KeyAuthPlugin{
		config: cfg,
	}, nil
}

func parseKeyAuthConfig(config map[string]interface{}) *KeyAuthConfig {
	cfg := &KeyAuthConfig{
		KeyNames:    []string{"apikey"},
		KeyInHeader: true,
		KeyInQuery:  true,
		HideCredentials: true,
	}

	if keyNames, ok := config["key_names"].([]interface{}); ok {
		names := make([]string, 0, len(keyNames))
		for _, n := range keyNames {
			if name, ok := n.(string); ok {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			cfg.KeyNames = names
		}
	}

	if keyInHeader, ok := config["key_in_header"].(bool); ok {
		cfg.KeyInHeader = keyInHeader
	}
	if keyInQuery, ok := config["key_in_query"].(bool); ok {
		cfg.KeyInQuery = keyInQuery
	}
	if hideCredentials, ok := config["hide_credentials"].(bool); ok {
		cfg.HideCredentials = hideCredentials
	}
	if anonymous, ok := config["anonymous"].(string); ok {
		cfg.Anonymous = &anonymous
	}

	return cfg
}

func (p *KeyAuthPlugin) Name() string {
	return KeyAuthPluginName
}

func (p *KeyAuthPlugin) SetCredentialFetcher(fetcher CredentialFetcher) {
	p.credentialFetcher = fetcher
}

func (p *KeyAuthPlugin) OnRequest(ctx *plugin.PluginContext) error {
	key := p.extractKey(ctx.Request)
	if key == "" {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "No API key found in request")
	}

	credFetcher, ok := ctx.GetAttribute("credential_fetcher").(CredentialFetcher)
	if !ok {
		return p.unauthorized(ctx, "Credential fetcher not configured")
	}

	cred, err := credFetcher.GetByKey(key)
	if err != nil || cred == nil {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "Invalid API key")
	}

	if !cred.Enabled {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "API key is disabled")
	}

	if !cred.IsKeyAuth() {
		if p.config.Anonymous != nil {
			ctx.SetAttribute("auth_anonymous", *p.config.Anonymous)
			return nil
		}
		return p.unauthorized(ctx, "Invalid credential type")
	}

	ctx.SetConsumerID(cred.ConsumerID)
	ctx.SetAttribute("auth_consumer_id", cred.ConsumerID)
	ctx.SetAttribute("auth_credential_id", cred.ID)
	ctx.SetAttribute("auth_credential_type", string(cred.Type))
	ctx.SetAttribute("auth_credential_key", cred.Key)

	if p.config.HideCredentials {
		p.hideCredentials(ctx)
	}

	return nil
}

func (p *KeyAuthPlugin) extractKey(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}

	if p.config.KeyInHeader {
		for _, keyName := range p.config.KeyNames {
			if key := r.Header.Get(keyName); key != "" {
				return key
			}
			if key := r.Header.Get("X-" + keyName); key != "" {
				return key
			}
		}
	}

	if p.config.KeyInQuery {
		for _, keyName := range p.config.KeyNames {
			if key := r.URL.Query().Get(keyName); key != "" {
				return key
			}
		}
	}

	return ""
}

func (p *KeyAuthPlugin) hideCredentials(ctx *plugin.PluginContext) {
	for _, keyName := range p.config.KeyNames {
		ctx.Request.Header.Del(keyName)
		ctx.Request.Header.Del("X-" + keyName)
	}

	q := ctx.Request.URL.Query()
	for _, keyName := range p.config.KeyNames {
		q.Del(keyName)
	}
	ctx.Request.URL.RawQuery = q.Encode()
}

func (p *KeyAuthPlugin) unauthorized(ctx *plugin.PluginContext, message string) error {
	ctx.ResponseWriter.Header().Set("Content-Type", "application/json")
	ctx.ResponseWriter.WriteHeader(http.StatusUnauthorized)
	ctx.ResponseWriter.Write([]byte(`{"message":"` + message + `"}`))
	ctx.ShortCircuit()
	return plugin.NewPluginError(p.Name(), "unauthorized", nil)
}

func (p *KeyAuthPlugin) OnResponse(ctx *plugin.PluginContext, resp *http.Response) error {
	return nil
}

func (p *KeyAuthPlugin) OnError(ctx *plugin.PluginContext, err error) error {
	return nil
}
