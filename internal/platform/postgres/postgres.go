package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/taichirain/portkey/internal/config"
	_ "github.com/lib/pq"
)

type DB struct {
	*sql.DB
}

func New(cfg *config.PostgresConfig) (*DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password='%s' dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, cfg.SSLMode,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库连接失败: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库连接测试失败: %w", err)
	}

	return &DB{db}, nil
}

func (db *DB) Ping(ctx context.Context) error {
	return db.DB.PingContext(ctx)
}

type Tx struct {
	*sql.Tx
}

func (db *DB) BeginTx(ctx context.Context) (*Tx, error) {
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Tx{tx}, nil
}
