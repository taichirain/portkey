package credential

import (
	"testing"

	"github.com/google/uuid"
)

func TestNew_ValidKeyAuth(t *testing.T) {
	cid := uuid.New()
	c, err := New(cid, TypeKeyAuth, "my-api-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ConsumerID != cid {
		t.Errorf("ConsumerID mismatch")
	}
	if c.Type != TypeKeyAuth {
		t.Errorf("expected type 'key-auth', got '%s'", c.Type)
	}
	if !c.Enabled {
		t.Error("expected enabled by default")
	}
}

func TestNew_ValidJWTAuth(t *testing.T) {
	cid := uuid.New()
	c, err := New(cid, TypeJWTAuth, "iss-value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.IsJWTAuth() {
		t.Error("expected IsJWTAuth=true")
	}
	if c.IsKeyAuth() {
		t.Error("expected IsKeyAuth=false")
	}
}

func TestNew_NilConsumerID(t *testing.T) {
	_, err := New(uuid.Nil, TypeKeyAuth, "key")
	if err != ErrCredentialConsumerIDRequired {
		t.Errorf("expected ErrCredentialConsumerIDRequired, got %v", err)
	}
}

func TestNew_EmptyType(t *testing.T) {
	_, err := New(uuid.New(), "", "key")
	if err != ErrCredentialTypeRequired {
		t.Errorf("expected ErrCredentialTypeRequired, got %v", err)
	}
}

func TestNew_EmptyKey(t *testing.T) {
	_, err := New(uuid.New(), TypeKeyAuth, "")
	if err != ErrCredentialKeyRequired {
		t.Errorf("expected ErrCredentialKeyRequired, got %v", err)
	}
}

func TestNew_WhitespaceKey(t *testing.T) {
	_, err := New(uuid.New(), TypeKeyAuth, "   ")
	if err != ErrCredentialKeyRequired {
		t.Errorf("expected ErrCredentialKeyRequired, got %v", err)
	}
}

func TestNew_InvalidType(t *testing.T) {
	_, err := New(uuid.New(), Type("oauth"), "key")
	if err != ErrCredentialInvalidType {
		t.Errorf("expected ErrCredentialInvalidType, got %v", err)
	}
}

func TestValidate_Valid(t *testing.T) {
	c, _ := New(uuid.New(), TypeKeyAuth, "key")
	if err := c.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestEnableDisable(t *testing.T) {
	c, _ := New(uuid.New(), TypeKeyAuth, "key")
	if !c.Enabled {
		t.Fatal("expected initially enabled")
	}
	c.Disable()
	if c.Enabled {
		t.Error("expected disabled")
	}
	c.Enable()
	if !c.Enabled {
		t.Error("expected enabled")
	}
}

func TestAddTag_NoDuplicate(t *testing.T) {
	c, _ := New(uuid.New(), TypeKeyAuth, "key")
	c.AddTag("prod")
	c.AddTag("v1")
	c.AddTag("prod")
	if len(c.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(c.Tags))
	}
}

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		name     string
		secret   string
		expected string
	}{
		{"empty", "", ""},
		{"short 1 char", "a", "****"},
		{"short 4 chars", "abcd", "****"},
		{"5 chars", "abcde", "ab****de"},
		{"long", "supersecretvalue", "su****ue"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := New(uuid.New(), TypeKeyAuth, "key")
			c.Secret = tt.secret
			if got := c.MaskSecret(); got != tt.expected {
				t.Errorf("MaskSecret() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{"short", "ab", "****"},
		{"normal", "apikey12345", "ap****45"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := New(uuid.New(), TypeKeyAuth, tt.key)
			if got := c.MaskKey(); got != tt.expected {
				t.Errorf("MaskKey() = %q, want %q", got, tt.expected)
			}
		})
	}

	t.Run("empty", func(t *testing.T) {
		c, _ := New(uuid.New(), TypeKeyAuth, "placeholder")
		c.Key = ""
		if got := c.MaskKey(); got != "" {
			t.Errorf("MaskKey() = %q, want empty", got)
		}
	})
}
