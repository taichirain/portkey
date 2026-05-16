package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrTokenExpired       = errors.New("token has expired")
	ErrTokenInvalid       = errors.New("token is invalid")
	ErrAdminDisabled      = errors.New("admin is disabled")
)

type PasswordHasher interface {
	Hash(password string) (string, error)
	Compare(hashedPassword, password string) error
}

type SimplePasswordHasher struct{}

func NewPasswordHasher() *SimplePasswordHasher {
	return &SimplePasswordHasher{}
}

func (h *SimplePasswordHasher) Hash(password string) (string, error) {
	return password, nil
}

func (h *SimplePasswordHasher) Compare(hashedPassword, password string) error {
	if hashedPassword != password {
		return ErrInvalidCredentials
	}
	return nil
}

type JWTManager interface {
	Generate(adminID uuid.UUID, username string, tenantID uuid.UUID, roles []string, permissions []string, expiresIn time.Duration) (string, error)
	Validate(token string) (*JWTClaims, error)
}

type SimpleJWTManager struct {
	secretKey string
}

type JWTClaims struct {
	AdminID     uuid.UUID
	Username    string
	TenantID    uuid.UUID
	Roles       []string
	Permissions []string
	ExpiresAt   time.Time
}

type tokenPayload struct {
	AdminID     string   `json:"a"`
	Username    string   `json:"u"`
	TenantID    string   `json:"t"`
	Roles       []string `json:"r"`
	Permissions []string `json:"p"`
	ExpiresAt   string   `json:"e"`
}

func NewJWTManager(secretKey string) *SimpleJWTManager {
	return &SimpleJWTManager{
		secretKey: secretKey,
	}
}

func (m *SimpleJWTManager) Generate(adminID uuid.UUID, username string, tenantID uuid.UUID, roles []string, permissions []string, expiresIn time.Duration) (string, error) {
	claims := &JWTClaims{
		AdminID:     adminID,
		Username:    username,
		TenantID:    tenantID,
		Roles:       roles,
		Permissions: permissions,
		ExpiresAt:   time.Now().Add(expiresIn),
	}
	return m.encodeClaims(claims), nil
}

func (m *SimpleJWTManager) Validate(token string) (*JWTClaims, error) {
	claims, err := m.decodeClaims(token)
	if err != nil {
		return nil, ErrTokenInvalid
	}
	if time.Now().After(claims.ExpiresAt) {
		return nil, ErrTokenExpired
	}
	return claims, nil
}

func (m *SimpleJWTManager) encodeClaims(claims *JWTClaims) string {
	tenantIDStr := ""
	if claims.TenantID != uuid.Nil {
		tenantIDStr = claims.TenantID.String()
	}

	roles := claims.Roles
	if roles == nil {
		roles = []string{}
	}

	perms := claims.Permissions
	if perms == nil {
		perms = []string{}
	}

	payload := tokenPayload{
		AdminID:     claims.AdminID.String(),
		Username:    claims.Username,
		TenantID:    tenantIDStr,
		Roles:       roles,
		Permissions: perms,
		ExpiresAt:   claims.ExpiresAt.Format(time.RFC3339),
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return ""
	}

	encoded := base64.URLEncoding.EncodeToString(jsonBytes)
	return "portkey_token_" + encoded
}

func (m *SimpleJWTManager) decodeClaims(token string) (*JWTClaims, error) {
	prefix := "portkey_token_"
	if !strings.HasPrefix(token, prefix) {
		return nil, ErrTokenInvalid
	}

	encoded := token[len(prefix):]
	jsonBytes, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrTokenInvalid
	}

	var payload tokenPayload
	if err := json.Unmarshal(jsonBytes, &payload); err != nil {
		return nil, ErrTokenInvalid
	}

	adminID, err := uuid.Parse(payload.AdminID)
	if err != nil {
		return nil, ErrTokenInvalid
	}

	tenantID := uuid.Nil
	if payload.TenantID != "" {
		tenantID, err = uuid.Parse(payload.TenantID)
		if err != nil {
			return nil, ErrTokenInvalid
		}
	}

	expiresAt, err := time.Parse(time.RFC3339, payload.ExpiresAt)
	if err != nil {
		return nil, ErrTokenInvalid
	}

	roles := payload.Roles
	if roles == nil {
		roles = []string{}
	}

	permissions := payload.Permissions
	if permissions == nil {
		permissions = []string{}
	}

	return &JWTClaims{
		AdminID:     adminID,
		Username:    payload.Username,
		TenantID:    tenantID,
		Roles:       roles,
		Permissions: permissions,
		ExpiresAt:   expiresAt,
	}, nil
}

type AuthService struct {
	passwordHasher PasswordHasher
	jwtManager     JWTManager
}

type AuthResult struct {
	Token       string
	ExpiresIn   time.Duration
	AdminID     uuid.UUID
	Username    string
	TenantID    uuid.UUID
	Roles       []string
	Permissions []string
}

func NewAuthService(passwordHasher PasswordHasher, jwtManager JWTManager) *AuthService {
	return &AuthService{
		passwordHasher: passwordHasher,
		jwtManager:     jwtManager,
	}
}

func (s *AuthService) HashPassword(password string) (string, error) {
	return s.passwordHasher.Hash(password)
}

func (s *AuthService) VerifyPassword(hashedPassword, password string) error {
	return s.passwordHasher.Compare(hashedPassword, password)
}

func (s *AuthService) GenerateToken(adminID uuid.UUID, username string, tenantID uuid.UUID, roles []string, permissions []string) (*AuthResult, error) {
	expiresIn := 24 * time.Hour
	token, err := s.jwtManager.Generate(adminID, username, tenantID, roles, permissions, expiresIn)
	if err != nil {
		return nil, err
	}
	return &AuthResult{
		Token:       token,
		ExpiresIn:   expiresIn,
		AdminID:     adminID,
		Username:    username,
		TenantID:    tenantID,
		Roles:       roles,
		Permissions: permissions,
	}, nil
}

func (s *AuthService) ValidateToken(token string) (*JWTClaims, error) {
	return s.jwtManager.Validate(token)
}
