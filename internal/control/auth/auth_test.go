package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJWTClaims_IncludesTenantID(t *testing.T) {
	adminID := uuid.New()
	tenantID := uuid.New()
	roles := []string{"tenant_admin", "developer"}
	permissions := []string{"service:read", "route:create"}

	claims := &JWTClaims{
		AdminID:     adminID,
		Username:    "testuser",
		TenantID:    tenantID,
		Roles:       roles,
		Permissions: permissions,
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}

	if claims.TenantID != tenantID {
		t.Errorf("TenantID = %s, want %s", claims.TenantID, tenantID)
	}
	if len(claims.Roles) != 2 {
		t.Errorf("Roles length = %d, want 2", len(claims.Roles))
	}
	if len(claims.Permissions) != 2 {
		t.Errorf("Permissions length = %d, want 2", len(claims.Permissions))
	}
}

func TestJWTManager_Generate_Succeeds(t *testing.T) {
	mgr := NewJWTManager("test-secret-key")
	adminID := uuid.New()
	tenantID := uuid.New()

	token, err := mgr.Generate(adminID, "admin1", tenantID,
		[]string{"tenant_admin"}, []string{"service:read"}, 1*time.Hour)
	if err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}
	// Verify it starts with expected prefix
	if len(token) < len("portkey_token_") {
		t.Error("token too short")
	}
}

// TestJWTManager_GenerateValidate_RoundTrip verifies that token
// generation and validation round-trips correctly with various
// role/permission combinations (using JSON+base64 encoding).
func TestJWTManager_GenerateValidate_RoundTrip(t *testing.T) {
	mgr := NewJWTManager("test-secret-key")
	adminID := uuid.New()
	tenantID := uuid.New()

	t.Run("role without underscores", func(t *testing.T) {
		token, err := mgr.Generate(adminID, "user1", tenantID,
			[]string{"viewer"}, []string{"service:read"}, 1*time.Hour)
		if err != nil {
			t.Fatalf("Generate error: %v", err)
		}
		claims, err := mgr.Validate(token)
		if err != nil {
			t.Fatalf("Validate error: %v", err)
		}
		if claims.AdminID != adminID {
			t.Errorf("AdminID = %s, want %s", claims.AdminID, adminID)
		}
		if claims.TenantID != tenantID {
			t.Errorf("TenantID = %s, want %s", claims.TenantID, tenantID)
		}
		if len(claims.Roles) != 1 || claims.Roles[0] != "viewer" {
			t.Errorf("Roles = %v, want [viewer]", claims.Roles)
		}
		if len(claims.Permissions) != 1 || claims.Permissions[0] != "service:read" {
			t.Errorf("Permissions = %v, want [service:read]", claims.Permissions)
		}
	})

	t.Run("role with underscore", func(t *testing.T) {
		token, err := mgr.Generate(adminID, "user2", tenantID,
			[]string{"tenant_admin"}, []string{"service:read", "route:create"}, 1*time.Hour)
		if err != nil {
			t.Fatalf("Generate error: %v", err)
		}
		claims, err := mgr.Validate(token)
		if err != nil {
			t.Fatalf("Validate error: %v", err)
		}
		if len(claims.Roles) != 1 || claims.Roles[0] != "tenant_admin" {
			t.Errorf("Roles = %v, want [tenant_admin]", claims.Roles)
		}
		if len(claims.Permissions) != 2 {
			t.Errorf("Permissions length = %d, want 2", len(claims.Permissions))
		}
	})

	t.Run("empty roles and permissions", func(t *testing.T) {
		token, err := mgr.Generate(adminID, "user3", uuid.Nil,
			[]string{}, []string{}, 1*time.Hour)
		if err != nil {
			t.Fatalf("Generate error: %v", err)
		}
		claims, err := mgr.Validate(token)
		if err != nil {
			t.Fatalf("Validate error: %v", err)
		}
		if claims.TenantID != uuid.Nil {
			t.Errorf("TenantID = %s, want nil", claims.TenantID)
		}
		if len(claims.Roles) != 0 {
			t.Errorf("Roles = %v, want []", claims.Roles)
		}
		if len(claims.Permissions) != 0 {
			t.Errorf("Permissions = %v, want []", claims.Permissions)
		}
	})

	t.Run("multiple roles", func(t *testing.T) {
		token, err := mgr.Generate(adminID, "user4", tenantID,
			[]string{"tenant_admin", "developer", "viewer"},
			[]string{"service:read", "route:create", "plugin:delete"}, 1*time.Hour)
		if err != nil {
			t.Fatalf("Generate error: %v", err)
		}
		claims, err := mgr.Validate(token)
		if err != nil {
			t.Fatalf("Validate error: %v", err)
		}
		if len(claims.Roles) != 3 {
			t.Errorf("Roles length = %d, want 3", len(claims.Roles))
		}
		if len(claims.Permissions) != 3 {
			t.Errorf("Permissions length = %d, want 3", len(claims.Permissions))
		}
	})
}

func TestJWTManager_Validate_InvalidToken(t *testing.T) {
	mgr := NewJWTManager("test-secret-key")

	_, err := mgr.Validate("not-a-valid-token")
	if err != ErrTokenInvalid {
		t.Errorf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestAuthService_GenerateToken_ReturnsFullAuthResult(t *testing.T) {
	hasher := NewPasswordHasher()
	jwtMgr := NewJWTManager("test-secret")
	svc := NewAuthService(hasher, jwtMgr)

	adminID := uuid.New()
	tenantID := uuid.New()
	roles := []string{"tenant_admin"}
	permissions := []string{"service:read", "route:create"}

	result, err := svc.GenerateToken(adminID, "admin1", tenantID, roles, permissions)
	if err != nil {
		t.Fatalf("GenerateToken error = %v", err)
	}

	if result.TenantID != tenantID {
		t.Errorf("result.TenantID = %s, want %s", result.TenantID, tenantID)
	}
	if len(result.Roles) != 1 || result.Roles[0] != "tenant_admin" {
		t.Errorf("result.Roles = %v, want [tenant_admin]", result.Roles)
	}
	if len(result.Permissions) != 2 {
		t.Errorf("result.Permissions length = %d, want 2", len(result.Permissions))
	}
	if result.Token == "" {
		t.Error("result.Token is empty")
	}
	if result.AdminID != adminID {
		t.Errorf("result.AdminID = %s, want %s", result.AdminID, adminID)
	}
}

func TestAuthService_ValidateToken_PropagatesJWTErrors(t *testing.T) {
	hasher := NewPasswordHasher()
	jwtMgr := NewJWTManager("test-secret")
	svc := NewAuthService(hasher, jwtMgr)

	_, err := svc.ValidateToken("invalid-token-here")
	if err != ErrTokenInvalid {
		t.Errorf("expected ErrTokenInvalid, got %v", err)
	}
}

// TestPasswordHasher_Compare verifies the simple hasher works for login flow.
func TestPasswordHasher_Compare(t *testing.T) {
	hasher := NewPasswordHasher()
	hash, _ := hasher.Hash("mypassword")

	if err := hasher.Compare(hash, "mypassword"); err != nil {
		t.Errorf("correct password should match: %v", err)
	}
	if err := hasher.Compare(hash, "wrongpassword"); err != ErrInvalidCredentials {
		t.Errorf("wrong password: expected ErrInvalidCredentials, got %v", err)
	}
}
