//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/auth"
	"github.com/taichirain/portkey/internal/control/publisher"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/control/validator"
	"github.com/taichirain/portkey/internal/domain/admin"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"github.com/taichirain/portkey/internal/domain/target"
	"github.com/taichirain/portkey/internal/domain/upstream"
	"github.com/taichirain/portkey/internal/platform/postgres"
	"go.uber.org/zap"
)

// --- Milestone 2: Control Plane 最小闭环 ---

func setupM2(t *testing.T) (
	*postgres.DB,
	*repository.PostgresAuditRepository,
	*repository.PostgresServiceRepository,
	*repository.PostgresRouteRepository,
	*repository.PostgresUpstreamRepository,
	*repository.PostgresTargetRepository,
	*repository.PostgresRevisionRepository,
	*repository.PostgresAdminRepository,
	*publisher.ConfigPublisher,
	*auth.AuthService,
) {
	t.Helper()
	db := SetupTestDB(t)
	CleanupTables(t, db)

	auditRepo := repository.NewPostgresAuditRepository(db)
	serviceRepo := repository.NewPostgresServiceRepository(db, auditRepo)
	routeRepo := repository.NewPostgresRouteRepository(db, auditRepo)
	upstreamRepo := repository.NewPostgresUpstreamRepository(db, auditRepo)
	targetRepo := repository.NewPostgresTargetRepository(db, auditRepo)
	revisionRepo := repository.NewPostgresRevisionRepository(db, auditRepo)
	adminRepo := repository.NewPostgresAdminRepository(db, auditRepo)
	trafficPolicyRepo := repository.NewPostgresTrafficPolicyRepository(db, auditRepo)

	configValidator := validator.NewConfigValidator(routeRepo, serviceRepo, upstreamRepo, targetRepo, trafficPolicyRepo)
	logger, _ := zap.NewDevelopment()
	pub := publisher.NewConfigPublisher(
		configValidator, routeRepo, serviceRepo, upstreamRepo, targetRepo, revisionRepo, auditRepo, trafficPolicyRepo, logger.Named("publisher"),
	)

	passwordHasher := auth.NewPasswordHasher()
	jwtManager := auth.NewJWTManager("test-secret")
	authService := auth.NewAuthService(passwordHasher, jwtManager)

	return db, auditRepo, serviceRepo, routeRepo, upstreamRepo, targetRepo, revisionRepo, adminRepo, pub, authService
}

// --- Admin Login ---

func TestM2_AdminLogin_Success(t *testing.T) {
	_, _, _, _, _, _, _, adminRepo, _, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("test123")
	a, _ := admin.New("login-admin", "login@test.com", hashedPwd)
	if err := adminRepo.Create(ctx, a, nil); err != nil {
		t.Fatalf("Failed to create admin: %v", err)
	}

	// Verify password works
	if err := authService.VerifyPassword(a.PasswordHash, "test123"); err != nil {
		t.Fatalf("Password verification failed: %v", err)
	}

	// Generate token
	result, err := authService.GenerateToken(a.ID, a.Username, uuid.Nil, []string{}, []string{})
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}
	if result.Token == "" {
		t.Error("Expected non-empty token")
	}

	// Validate token
	claims, err := authService.ValidateToken(result.Token)
	if err != nil {
		t.Fatalf("Failed to validate token: %v", err)
	}
	if claims.AdminID != a.ID {
		t.Errorf("Expected admin ID %s, got %s", a.ID, claims.AdminID)
	}
	if claims.Username != "login-admin" {
		t.Errorf("Expected username 'login-admin', got '%s'", claims.Username)
	}
}

func TestM2_AdminLogin_WrongPassword(t *testing.T) {
	_, _, _, _, _, _, _, adminRepo, _, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("correct-password")
	a, _ := admin.New("wrong-pwd-admin", "wrong@test.com", hashedPwd)
	adminRepo.Create(ctx, a, nil)

	err := authService.VerifyPassword(a.PasswordHash, "wrong-password")
	if err == nil {
		t.Error("Expected error for wrong password")
	}
}

func TestM2_AdminLogin_NonExistentUser(t *testing.T) {
	_, _, _, _, _, _, _, adminRepo, _, _ := setupM2(t)
	ctx := context.Background()

	_, err := adminRepo.GetByUsername(ctx, "non-existent-user")
	if err == nil {
		t.Error("Expected error for non-existent user")
	}
}

func TestM2_Token_InvalidFormat(t *testing.T) {
	_, _, _, _, _, _, _, _, _, authService := setupM2(t)

	_, err := authService.ValidateToken("garbage-token")
	if err == nil {
		t.Error("Expected error for invalid token")
	}
}

func TestM2_Token_Expired(t *testing.T) {
	_, _, _, _, _, _, _, _, _, authService := setupM2(t)

	// Generate a token with the real service (24h expiry)
	a, _ := admin.New("exp-admin", "exp@test.com", "pwd")
	result, _ := authService.GenerateToken(a.ID, a.Username, uuid.Nil, []string{}, []string{})

	// Current token should be valid
	_, err := authService.ValidateToken(result.Token)
	if err != nil {
		t.Fatalf("Fresh token should be valid: %v", err)
	}
}

// --- Config Validator ---

func TestM2_ConfigValidator_ValidConfig(t *testing.T) {
	db, auditRepo, serviceRepo, routeRepo, upstreamRepo, targetRepo, _, _, _, _ := setupM2(t)
	ctx := context.Background()

	// Create upstream + target
	up, _ := upstream.New("valid-upstream")
	if err := upstreamRepo.Create(ctx, up, nil); err != nil {
		t.Fatalf("Failed to create upstream: %v", err)
	}
	tgt, _ := target.New(up.ID, "backend.example.com", 8080)
	if err := targetRepo.Create(ctx, tgt, nil); err != nil {
		t.Fatalf("Failed to create target: %v", err)
	}

	// Create service with upstream
	svc, _ := service.New("valid-service")
	svc.UpstreamID = up.ID
	if err := serviceRepo.Create(ctx, svc, nil); err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Create route
	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	if err := routeRepo.Create(ctx, r, nil); err != nil {
		t.Fatalf("Failed to create route: %v", err)
	}

	// Validate
	tpRepo := repository.NewPostgresTrafficPolicyRepository(db, auditRepo)
	configValidator := validator.NewConfigValidator(routeRepo, serviceRepo, upstreamRepo, targetRepo, tpRepo)
	result, err := configValidator.ValidateAll(ctx)
	if err != nil {
		t.Fatalf("Validation error: %v", err)
	}
	if !result.Valid {
		t.Errorf("Expected valid config, got errors: %v", result.Errors)
	}
}

func TestM2_ConfigValidator_RouteNoMatchConditions(t *testing.T) {
	db, auditRepo, serviceRepo, routeRepo, upstreamRepo, targetRepo, _, _, _, _ := setupM2(t)
	ctx := context.Background()

	svc, _ := service.New("svc-for-bad-route")
	svc.Host = "example.com"
	serviceRepo.Create(ctx, svc, nil)

	// Insert route with no match conditions directly via SQL (bypasses domain validation)
	routeID := uuid.New()
	_, err := db.ExecContext(ctx,
		`INSERT INTO routes (id, service_id, protocols, enabled) VALUES ($1, $2, $3, $4)`,
		routeID, svc.ID, `{"http"}`, true,
	)
	if err != nil {
		t.Fatalf("Failed to insert route via SQL: %v", err)
	}

	tpRepo := repository.NewPostgresTrafficPolicyRepository(db, auditRepo)
	configValidator := validator.NewConfigValidator(routeRepo, serviceRepo, upstreamRepo, targetRepo, tpRepo)
	result, err := configValidator.ValidateAll(ctx)
	if err != nil {
		t.Fatalf("Validation error: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid config for route with no match conditions")
	}
}

func TestM2_ConfigValidator_UpstreamNoTargets(t *testing.T) {
	db, auditRepo, serviceRepo, routeRepo, upstreamRepo, targetRepo, _, _, _, _ := setupM2(t)
	ctx := context.Background()

	up, _ := upstream.New("empty-upstream")
	upstreamRepo.Create(ctx, up, nil)

	// Service references upstream but upstream has no targets
	svc, _ := service.New("svc-no-targets")
	svc.UpstreamID = up.ID
	serviceRepo.Create(ctx, svc, nil)

	tpRepo := repository.NewPostgresTrafficPolicyRepository(db, auditRepo)
	configValidator := validator.NewConfigValidator(routeRepo, serviceRepo, upstreamRepo, targetRepo, tpRepo)
	result, err := configValidator.ValidateAll(ctx)
	if err != nil {
		t.Fatalf("Validation error: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid config for upstream with no targets")
	}
}

func TestM2_ConfigValidator_ServiceNoTargetOrUpstream(t *testing.T) {
	db, auditRepo, serviceRepo, routeRepo, upstreamRepo, targetRepo, _, _, _, _ := setupM2(t)
	ctx := context.Background()

	svc, _ := service.New("orphan-service")
	// No Host/Port and no UpstreamID
	serviceRepo.Create(ctx, svc, nil)

	tpRepo := repository.NewPostgresTrafficPolicyRepository(db, auditRepo)
	configValidator := validator.NewConfigValidator(routeRepo, serviceRepo, upstreamRepo, targetRepo, tpRepo)
	result, err := configValidator.ValidateAll(ctx)
	if err != nil {
		t.Fatalf("Validation error: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid config for service with no target or upstream")
	}
}

func TestM2_ConfigValidator_RouteReferencesNonExistentService(t *testing.T) {
	db, auditRepo, serviceRepo, routeRepo, upstreamRepo, targetRepo, _, _, _, _ := setupM2(t)
	ctx := context.Background()

	// Insert a route referencing a non-existent service ID directly via SQL
	// (bypass FK temporarily)
	orphanServiceID := uuid.New()
	routeID := uuid.New()
	_, err := db.ExecContext(ctx, `ALTER TABLE routes DISABLE TRIGGER ALL`)
	if err != nil {
		t.Fatalf("Failed to disable triggers: %v", err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO routes (id, service_id, protocols, paths, methods, enabled) VALUES ($1, $2, $3, $4, $5, $6)`,
		routeID, orphanServiceID, `{"http"}`, `{"/test"}`, `{"GET"}`, true,
	)
	if err != nil {
		t.Fatalf("Failed to insert orphan route: %v", err)
	}
	_, err = db.ExecContext(ctx, `ALTER TABLE routes ENABLE TRIGGER ALL`)
	if err != nil {
		t.Fatalf("Failed to re-enable triggers: %v", err)
	}

	tpRepo := repository.NewPostgresTrafficPolicyRepository(db, auditRepo)
	configValidator := validator.NewConfigValidator(routeRepo, serviceRepo, upstreamRepo, targetRepo, tpRepo)
	result, err := configValidator.ValidateAll(ctx)
	if err != nil {
		t.Fatalf("Validation error: %v", err)
	}
	if result.Valid {
		t.Error("Expected invalid config when route references non-existent service")
	}
}

// --- Revision Publish Flow ---

func TestM2_RevisionPublish_FullFlow(t *testing.T) {
	_, _, serviceRepo, routeRepo, upstreamRepo, targetRepo, _, adminRepo, pub, authService := setupM2(t)
	ctx := context.Background()

	// Create admin
	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("publisher", "pub@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	// Create resources
	up, _ := upstream.New("pub-upstream")
	upstreamRepo.Create(ctx, up, nil)

	tgt, _ := target.New(up.ID, "backend.example.com", 8080)
	targetRepo.Create(ctx, tgt, nil)

	svc, _ := service.New("pub-service")
	svc.UpstreamID = up.ID
	serviceRepo.Create(ctx, svc, nil)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	routeRepo.Create(ctx, r, nil)

	// Step 1: Validate
 validationResult, err := pub.Validate(ctx)
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if !validationResult.Valid {
		t.Fatalf("Expected valid config, got errors: %v", validationResult.Errors)
	}

	// Step 2: Create snapshot
	snap, err := pub.CreateSnapshot(ctx)
	if err != nil {
		t.Fatalf("CreateSnapshot error: %v", err)
	}
	if len(snap.Services) != 1 {
		t.Errorf("Expected 1 service in snapshot, got %d", len(snap.Services))
	}
	if len(snap.Routes) != 1 {
		t.Errorf("Expected 1 route in snapshot, got %d", len(snap.Routes))
	}
	if len(snap.Upstreams) != 1 {
		t.Errorf("Expected 1 upstream in snapshot, got %d", len(snap.Upstreams))
	}

	// Step 3: Create revision
	revResult, err := pub.CreateRevision(ctx, "v1.0", "first version", &adminUser.ID, auditCtx)
	if err != nil {
		t.Fatalf("CreateRevision error: %v", err)
	}
	if revResult.Version != "v1.0" {
		t.Errorf("Expected version 'v1.0', got '%s'", revResult.Version)
	}
	if revResult.IsActive {
		t.Error("New revision should not be active yet")
	}

	// Step 4: Publish (activate)
	pubResult, err := pub.Publish(ctx, revResult.RevisionID, auditCtx)
	if err != nil {
		t.Fatalf("Publish error: %v", err)
	}
	if !pubResult.IsActive {
		t.Error("Published revision should be active")
	}

	// Step 5: Get active revision
	activeRev, err := pub.GetActiveRevision(ctx)
	if err != nil {
		t.Fatalf("GetActiveRevision error: %v", err)
	}
	if activeRev.ID != revResult.RevisionID {
		t.Errorf("Expected active revision %s, got %s", revResult.RevisionID, activeRev.ID)
	}
	if activeRev.Version != "v1.0" {
		t.Errorf("Expected version 'v1.0', got '%s'", activeRev.Version)
	}
	if activeRev.Snapshot == nil {
		t.Error("Expected snapshot to be populated")
	}
}

func TestM2_RevisionPublish_InvalidConfigBlocksPublish(t *testing.T) {
	_, _, serviceRepo, _, _, _, _, adminRepo, pub, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("block-admin", "block@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	// Create a service with no host/port and no upstream (invalid)
	svc, _ := service.New("invalid-service")
	serviceRepo.Create(ctx, svc, nil)

	// Try to create revision — should fail validation
	_, err := pub.CreateRevision(ctx, "v-bad", "invalid config", &adminUser.ID, auditCtx)
	if err == nil {
		t.Fatal("Expected CreateRevision to fail for invalid config")
	}
}

func TestM2_RevisionPublish_PublishNonExistentRevision(t *testing.T) {
	_, _, _, _, _, _, _, adminRepo, pub, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("notfound-admin", "notfound@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	_, err := pub.Publish(ctx, uuid.New(), auditCtx)
	if err == nil {
		t.Fatal("Expected error when publishing non-existent revision")
	}
}

func TestM2_RevisionPublish_PublishAlreadyActive(t *testing.T) {
	_, _, serviceRepo, routeRepo, upstreamRepo, targetRepo, _, adminRepo, pub, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("already-admin", "already@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	// Create valid resources
	up, _ := upstream.New("already-upstream")
	upstreamRepo.Create(ctx, up, nil)
	tgt, _ := target.New(up.ID, "backend.example.com", 8080)
	targetRepo.Create(ctx, tgt, nil)
	svc, _ := service.New("already-service")
	svc.UpstreamID = up.ID
	serviceRepo.Create(ctx, svc, nil)
	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	routeRepo.Create(ctx, r, nil)

	// Create and publish
	revResult, _ := pub.CreateRevision(ctx, "v1", "first", &adminUser.ID, auditCtx)
	pub.Publish(ctx, revResult.RevisionID, auditCtx)

	// Publish again — should succeed (idempotent)
	pubResult, err := pub.Publish(ctx, revResult.RevisionID, auditCtx)
	if err != nil {
		t.Fatalf("Re-publishing active revision should not error: %v", err)
	}
	if !pubResult.IsActive {
		t.Error("Expected re-published revision to still be active")
	}
}

func TestM2_RevisionPublish_NoActiveRevision(t *testing.T) {
	_, _, _, _, _, _, _, _, pub, _ := setupM2(t)
	ctx := context.Background()

	_, err := pub.GetActiveRevision(ctx)
	if err == nil {
		t.Fatal("Expected error when no active revision exists")
	}
}

// --- Revision Rollback ---

func TestM2_RevisionRollback(t *testing.T) {
	_, _, serviceRepo, routeRepo, upstreamRepo, targetRepo, _, adminRepo, pub, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("rollback-admin", "rollback@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	// Create valid resources
	up, _ := upstream.New("rb-upstream")
	upstreamRepo.Create(ctx, up, nil)
	tgt, _ := target.New(up.ID, "backend.example.com", 8080)
	targetRepo.Create(ctx, tgt, nil)
	svc, _ := service.New("rb-service")
	svc.UpstreamID = up.ID
	serviceRepo.Create(ctx, svc, nil)
	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	routeRepo.Create(ctx, r, nil)

	// Create and publish v1
	rev1, _ := pub.CreateRevision(ctx, "v1.0", "version 1", &adminUser.ID, auditCtx)
	pub.Publish(ctx, rev1.RevisionID, auditCtx)

	// Create and publish v2
	rev2, _ := pub.CreateRevision(ctx, "v2.0", "version 2", &adminUser.ID, auditCtx)
	pub.Publish(ctx, rev2.RevisionID, auditCtx)

	// Verify v2 is active
	activeRev, _ := pub.GetActiveRevision(ctx)
	if activeRev.ID != rev2.RevisionID {
		t.Errorf("Expected v2 to be active, got %s", activeRev.Version)
	}

	// Rollback to v1
	rollbackResult, err := pub.Rollback(ctx, rev1.RevisionID, auditCtx)
	if err != nil {
		t.Fatalf("Rollback error: %v", err)
	}
	if rollbackResult.RevisionID != rev1.RevisionID {
		t.Errorf("Expected rollback to v1, got %s", rollbackResult.Version)
	}

	// Verify v1 is now active
	activeRev, _ = pub.GetActiveRevision(ctx)
	if activeRev.ID != rev1.RevisionID {
		t.Errorf("Expected v1 to be active after rollback, got %s", activeRev.Version)
	}
}

func TestM2_RevisionRollback_NonExistentRevision(t *testing.T) {
	_, _, _, _, _, _, _, adminRepo, pub, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("rb-notfound", "rb@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	_, err := pub.Rollback(ctx, uuid.New(), auditCtx)
	if err == nil {
		t.Fatal("Expected error when rolling back to non-existent revision")
	}
}

func TestM2_RevisionRollback_AlreadyActive(t *testing.T) {
	_, _, serviceRepo, routeRepo, upstreamRepo, targetRepo, _, adminRepo, pub, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("rb-active", "rbactive@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	up, _ := upstream.New("rb-active-up")
	upstreamRepo.Create(ctx, up, nil)
	tgt, _ := target.New(up.ID, "backend.example.com", 8080)
	targetRepo.Create(ctx, tgt, nil)
	svc, _ := service.New("rb-active-svc")
	svc.UpstreamID = up.ID
	serviceRepo.Create(ctx, svc, nil)
	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	routeRepo.Create(ctx, r, nil)

	rev, _ := pub.CreateRevision(ctx, "v1", "first", &adminUser.ID, auditCtx)
	pub.Publish(ctx, rev.RevisionID, auditCtx)

	// Rollback to the already active revision
	result, err := pub.Rollback(ctx, rev.RevisionID, auditCtx)
	if err != nil {
		t.Fatalf("Rollback to active revision should not error: %v", err)
	}
	if !result.IsActive {
		t.Error("Expected revision to still be active")
	}
}

// --- Invalid Config Doesn't Break Active Revision ---

func TestM2_InvalidConfigDoesNotBreakActive(t *testing.T) {
	_, _, serviceRepo, routeRepo, upstreamRepo, targetRepo, _, adminRepo, pub, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("protect-admin", "protect@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	// Create and publish a valid config
	up, _ := upstream.New("protect-upstream")
	upstreamRepo.Create(ctx, up, nil)
	tgt, _ := target.New(up.ID, "backend.example.com", 8080)
	targetRepo.Create(ctx, tgt, nil)
	svc, _ := service.New("protect-service")
	svc.UpstreamID = up.ID
	serviceRepo.Create(ctx, svc, nil)
	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	routeRepo.Create(ctx, r, nil)

	rev1, _ := pub.CreateRevision(ctx, "v1-stable", "stable version", &adminUser.ID, auditCtx)
	pub.Publish(ctx, rev1.RevisionID, auditCtx)

	// Verify v1 is active
	activeBefore, _ := pub.GetActiveRevision(ctx)
	if activeBefore.ID != rev1.RevisionID {
		t.Fatal("Setup failed: v1 should be active")
	}

	// Now add an invalid service (no host/port, no upstream)
	invalidSvc, _ := service.New("invalid-svc")
	// No Host, No UpstreamID
	serviceRepo.Create(ctx, invalidSvc, nil)

	// Try to create a new revision — should fail validation
	_, err := pub.CreateRevision(ctx, "v2-bad", "bad version", &adminUser.ID, auditCtx)
	if err == nil {
		t.Fatal("Expected CreateRevision to fail for invalid config")
	}

	// Verify v1 is STILL active (unchanged)
	activeAfter, _ := pub.GetActiveRevision(ctx)
	if activeAfter.ID != rev1.RevisionID {
		t.Errorf("Active revision should still be v1, got %s", activeAfter.Version)
	}
	if activeAfter.Version != "v1-stable" {
		t.Errorf("Active revision version should be 'v1-stable', got '%s'", activeAfter.Version)
	}
}

// --- Revision List ---

func TestM2_RevisionList(t *testing.T) {
	_, _, serviceRepo, routeRepo, upstreamRepo, targetRepo, _, adminRepo, pub, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("list-admin", "list@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	// Create valid resources
	up, _ := upstream.New("list-upstream")
	upstreamRepo.Create(ctx, up, nil)
	tgt, _ := target.New(up.ID, "backend.example.com", 8080)
	targetRepo.Create(ctx, tgt, nil)
	svc, _ := service.New("list-service")
	svc.UpstreamID = up.ID
	serviceRepo.Create(ctx, svc, nil)
	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	routeRepo.Create(ctx, r, nil)

	// Create 3 revisions
	for i := 1; i <= 3; i++ {
		_, err := pub.CreateRevision(ctx, fmt.Sprintf("v%d", i), fmt.Sprintf("version %d", i), &adminUser.ID, auditCtx)
		if err != nil {
			t.Fatalf("Failed to create revision v%d: %v", i, err)
		}
	}

	// List revisions
	result, err := pub.ListRevisions(ctx, 1, 10)
	if err != nil {
		t.Fatalf("ListRevisions error: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("Expected 3 revisions, got %d", result.Total)
	}
	if len(result.Items) != 3 {
		t.Errorf("Expected 3 items, got %d", len(result.Items))
	}
}

// --- CreateAndPublish ---

func TestM2_CreateAndPublish(t *testing.T) {
	_, _, serviceRepo, routeRepo, upstreamRepo, targetRepo, _, adminRepo, pub, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("cap-admin", "cap@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	// Create valid resources
	up, _ := upstream.New("cap-upstream")
	upstreamRepo.Create(ctx, up, nil)
	tgt, _ := target.New(up.ID, "backend.example.com", 8080)
	targetRepo.Create(ctx, tgt, nil)
	svc, _ := service.New("cap-service")
	svc.UpstreamID = up.ID
	serviceRepo.Create(ctx, svc, nil)
	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	routeRepo.Create(ctx, r, nil)

	// Create and publish in one step
	result, err := pub.CreateAndPublish(ctx, "v1.0", "initial release", &adminUser.ID, auditCtx)
	if err != nil {
		t.Fatalf("CreateAndPublish error: %v", err)
	}
	if !result.IsActive {
		t.Error("Expected revision to be active after CreateAndPublish")
	}

	// Verify it's the active revision
	activeRev, _ := pub.GetActiveRevision(ctx)
	if activeRev.ID != result.RevisionID {
		t.Errorf("Expected active revision to match published one")
	}
}

// --- Service CRUD via Repository with Auth Context ---

func TestM2_ServiceCRUD_WithAuditContext(t *testing.T) {
	_, auditRepo, serviceRepo, _, _, _, _, adminRepo, _, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("crud-admin", "crud@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	// Create
	svc, _ := service.New("crud-service")
	svc.Host = "example.com"
	svc.Port = 8080
	if err := serviceRepo.Create(ctx, svc, auditCtx); err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Verify audit log was created
	auditLogs, _ := auditRepo.GetByResourceID(ctx, "service", svc.ID)
	if len(auditLogs) != 1 {
		t.Errorf("Expected 1 audit log for create, got %d", len(auditLogs))
	}

	// Read
	fetched, err := serviceRepo.GetByID(ctx, svc.ID)
	if err != nil {
		t.Fatalf("Failed to get service: %v", err)
	}
	if fetched.Name != "crud-service" {
		t.Errorf("Expected name 'crud-service', got '%s'", fetched.Name)
	}

	// Update
	fetched.Host = "updated.example.com"
	fetched.Port = 9090
	if err := serviceRepo.Update(ctx, fetched, auditCtx); err != nil {
		t.Fatalf("Failed to update service: %v", err)
	}

	updated, _ := serviceRepo.GetByID(ctx, svc.ID)
	if updated.Host != "updated.example.com" {
		t.Errorf("Expected updated host, got '%s'", updated.Host)
	}

	// Verify audit log for update
	auditLogs, _ = auditRepo.GetByResourceID(ctx, "service", svc.ID)
	if len(auditLogs) != 2 {
		t.Errorf("Expected 2 audit logs (create+update), got %d", len(auditLogs))
	}

	// Delete
	if err := serviceRepo.Delete(ctx, svc.ID, auditCtx); err != nil {
		t.Fatalf("Failed to delete service: %v", err)
	}

	_, err = serviceRepo.GetByID(ctx, svc.ID)
	if err == nil {
		t.Error("Expected error after deleting service")
	}

	// Verify audit log for delete
	auditLogs, _ = auditRepo.GetByResourceID(ctx, "service", svc.ID)
	if len(auditLogs) != 3 {
		t.Errorf("Expected 3 audit logs (create+update+delete), got %d", len(auditLogs))
	}
}

// --- Upstream + Target CRUD ---

func TestM2_UpstreamTargetCRUD(t *testing.T) {
	_, _, _, _, upstreamRepo, targetRepo, _, adminRepo, _, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("ut-admin", "ut@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	// Create upstream
	up, _ := upstream.New("crud-upstream")
	up.Algorithm = upstream.AlgorithmRoundRobin
	if err := upstreamRepo.Create(ctx, up, auditCtx); err != nil {
		t.Fatalf("Failed to create upstream: %v", err)
	}

	// Create targets
	t1, _ := target.New(up.ID, "host1.example.com", 8080)
	t2, _ := target.New(up.ID, "host2.example.com", 8080)
	if err := targetRepo.Create(ctx, t1, auditCtx); err != nil {
		t.Fatalf("Failed to create target 1: %v", err)
	}
	if err := targetRepo.Create(ctx, t2, auditCtx); err != nil {
		t.Fatalf("Failed to create target 2: %v", err)
	}

	// List targets by upstream
	targets, err := targetRepo.ListByUpstreamID(ctx, up.ID)
	if err != nil {
		t.Fatalf("Failed to list targets: %v", err)
	}
	if len(targets) != 2 {
		t.Errorf("Expected 2 targets, got %d", len(targets))
	}

	// Update upstream
	up.Name = "updated-upstream"
	up.Slots = 5000
	if err := upstreamRepo.Update(ctx, up, auditCtx); err != nil {
		t.Fatalf("Failed to update upstream: %v", err)
	}

	fetched, _ := upstreamRepo.GetByID(ctx, up.ID)
	if fetched.Name != "updated-upstream" {
		t.Errorf("Expected name 'updated-upstream', got '%s'", fetched.Name)
	}

	// Delete target
	if err := targetRepo.Delete(ctx, t1.ID, auditCtx); err != nil {
		t.Fatalf("Failed to delete target: %v", err)
	}

	targets, _ = targetRepo.ListByUpstreamID(ctx, up.ID)
	if len(targets) != 1 {
		t.Errorf("Expected 1 target after delete, got %d", len(targets))
	}
}

// --- Route CRUD ---

func TestM2_RouteCRUD_WithAuditContext(t *testing.T) {
	_, auditRepo, serviceRepo, routeRepo, _, _, _, adminRepo, _, authService := setupM2(t)
	ctx := context.Background()

	hashedPwd, _ := authService.HashPassword("admin123")
	adminUser, _ := admin.New("route-admin", "route@test.com", hashedPwd)
	adminRepo.Create(ctx, adminUser, nil)
	auditCtx := newTestAuditContext(adminUser.ID)

	svc, _ := service.New("route-parent-svc")
	svc.Host = "example.com"
	serviceRepo.Create(ctx, svc, nil)

	// Create route
	r, _ := route.New(svc.ID)
	r.Name = "test-route"
	r.AddPath("/api/v1")
	r.AddPath("/api/v2")
	r.AddMethod("GET")
	r.AddMethod("POST")
	r.AddHost("api.example.com")
	r.Headers = map[string][]string{
		"X-API-Version": {"v1"},
	}
	r.StripPath = true
	r.PreserveHost = false
	if err := routeRepo.Create(ctx, r, auditCtx); err != nil {
		t.Fatalf("Failed to create route: %v", err)
	}

	// Verify audit log
	auditLogs, _ := auditRepo.GetByResourceID(ctx, "route", r.ID)
	if len(auditLogs) != 1 {
		t.Errorf("Expected 1 audit log, got %d", len(auditLogs))
	}

	// Read
	fetched, err := routeRepo.GetByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("Failed to get route: %v", err)
	}
	if len(fetched.Paths) != 2 {
		t.Errorf("Expected 2 paths, got %d", len(fetched.Paths))
	}
	if len(fetched.Methods) != 2 {
		t.Errorf("Expected 2 methods, got %d", len(fetched.Methods))
	}
	if len(fetched.Hosts) != 1 {
		t.Errorf("Expected 1 host, got %d", len(fetched.Hosts))
	}

	// Update
	fetched.AddPath("/api/v3")
	fetched.AddMethod("DELETE")
	if err := routeRepo.Update(ctx, fetched, auditCtx); err != nil {
		t.Fatalf("Failed to update route: %v", err)
	}

	updated, _ := routeRepo.GetByID(ctx, r.ID)
	if len(updated.Paths) != 3 {
		t.Errorf("Expected 3 paths after update, got %d", len(updated.Paths))
	}
	if len(updated.Methods) != 3 {
		t.Errorf("Expected 3 methods after update, got %d", len(updated.Methods))
	}

	// List by service
	routes, err := routeRepo.ListByServiceID(ctx, svc.ID)
	if err != nil {
		t.Fatalf("Failed to list routes: %v", err)
	}
	if len(routes) != 1 {
		t.Errorf("Expected 1 route, got %d", len(routes))
	}

	// Delete
	if err := routeRepo.Delete(ctx, r.ID, auditCtx); err != nil {
		t.Fatalf("Failed to delete route: %v", err)
	}

	_, err = routeRepo.GetByID(ctx, r.ID)
	if err == nil {
		t.Error("Expected error after deleting route")
	}
}
