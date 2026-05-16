//go:build integration
// +build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/config"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

var testDB *postgres.DB

func getTestConfig() *config.PostgresConfig {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		host = "127.0.0.1"
	}

	port := 5432
	if p := os.Getenv("TEST_DB_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}

	user := os.Getenv("TEST_DB_USER")
	if user == "" {
		user = "postgres"
	}

	password := os.Getenv("TEST_DB_PASSWORD")

	database := os.Getenv("TEST_DB_NAME")
	if database == "" {
		database = "portkey_test"
	}

	return &config.PostgresConfig{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		Database: database,
		SSLMode:  "disable",
	}
}

func SetupTestDB(t *testing.T) *postgres.DB {
	t.Helper()

	if testDB != nil {
		return testDB
	}

	var err error
	testDB, err = postgres.New(getTestConfig())
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	return testDB
}

func CleanupTables(t *testing.T, db *postgres.DB) {
	t.Helper()

	ctx := context.Background()

	tables := []string{
		"audit_logs",
		"config_revisions",
		"plugins",
		"credentials",
		"consumers",
		"targets",
		"routes",
		"services",
		"upstreams",
		"admins",
		"distributed_locks",
		"cp_instances",
	}

	for _, table := range tables {
		_, err := db.ExecContext(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
		if err != nil {
			t.Logf("Warning: failed to truncate table %s: %v", table, err)
		}
	}
}

func createTestAdmin(t *testing.T, db *postgres.DB, username string) uuid.UUID {
	t.Helper()

	ctx := context.Background()
	adminID := uuid.New()

	query := `
		INSERT INTO admins (id, username, password_hash, enabled)
		VALUES ($1, $2, $3, $4)
	`

	_, err := db.ExecContext(ctx, query, adminID, username, "hashed_password", true)
	if err != nil {
		t.Fatalf("Failed to create test admin: %v", err)
	}

	return adminID
}

func newTestAuditContext(adminID uuid.UUID) *repository.AuditContext {
	return &repository.AuditContext{
		AdminID:   &adminID,
		ClientIP:  "127.0.0.1",
		UserAgent: "test-agent",
		RequestID: "test-req-" + uuid.New().String(),
	}
}

func countRows(t *testing.T, db *postgres.DB, table string) int {
	t.Helper()

	ctx := context.Background()
	var count int

	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	err := db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count rows in %s: %v", table, err)
	}

	return count
}

func tableExists(t *testing.T, db *postgres.DB, tableName string) bool {
	t.Helper()

	ctx := context.Background()
	var exists bool

	query := `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = $1
		)
	`

	err := db.QueryRowContext(ctx, query, tableName).Scan(&exists)
	if err != nil {
		t.Fatalf("Failed to check if table exists: %v", err)
	}

	return exists
}

func getSQLDB(t *testing.T, cfg *config.PostgresConfig) *sql.DB {
	t.Helper()

	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, cfg.SSLMode,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	return db
}
