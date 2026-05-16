-- Portkey API Gateway - Migration 0005
-- Description: Multi Control Plane High Availability Support
--  1. Fix unique index for active revision (add tenant_id support)
--  2. Add distributed lock table for coordination
--  3. Add CP instance health check table

-- Enable UUID extension if not exists
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ========== 1. Fix active revision unique index ==========
-- 问题：原索引 idx_config_revisions_single_active 只基于 is_active，不支持多租户隔离
-- 在多租户场景下，每个租户应该有自己独立的 active revision

-- 先删除旧索引
DROP INDEX IF EXISTS idx_config_revisions_single_active;

-- 创建新的支持多租户的唯一索引
-- 使用部分索引，确保每个租户同一时间只有一个 active revision
CREATE UNIQUE INDEX idx_config_revisions_single_active_per_tenant 
ON config_revisions(tenant_id, is_active) 
WHERE is_active = TRUE;

-- 同时，为了兼容无租户的场景（tenant_id 为 NULL），需要单独处理
-- 注意：PostgreSQL 中 NULL 在唯一索引中是不相等的，所以需要额外处理

-- 创建一个函数来确保 tenant_id 为 NULL 时也只有一个 active revision
CREATE OR REPLACE FUNCTION check_single_active_revision_no_tenant()
RETURNS TRIGGER AS $$
BEGIN
    -- 如果新记录是 active 且 tenant_id 为 NULL
    IF NEW.is_active = TRUE AND NEW.tenant_id IS NULL THEN
        -- 检查是否已存在其他 active 且 tenant_id 为 NULL 的记录
        IF EXISTS (
            SELECT 1 FROM config_revisions 
            WHERE is_active = TRUE 
            AND tenant_id IS NULL 
            AND id != NEW.id
        ) THEN
            RAISE EXCEPTION 'Only one active revision allowed with no tenant';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- 为无租户场景创建触发器
DROP TRIGGER IF EXISTS trigger_check_single_active_revision_no_tenant ON config_revisions;
CREATE TRIGGER trigger_check_single_active_revision_no_tenant
BEFORE INSERT OR UPDATE ON config_revisions
FOR EACH ROW
EXECUTE FUNCTION check_single_active_revision_no_tenant();

-- ========== 2. Distributed Lock Table ==========
-- 用于多 CP 实例间的分布式锁协调

CREATE TABLE distributed_locks (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    lock_key    VARCHAR(255) NOT NULL,
    holder_id   UUID NOT NULL,
    holder_info TEXT,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT  uk_distributed_locks_key UNIQUE (lock_key)
);

CREATE INDEX idx_distributed_locks_expires_at ON distributed_locks(expires_at);

-- 添加 updated_at 触发器
CREATE TRIGGER update_distributed_locks_updated_at BEFORE UPDATE ON distributed_locks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ========== 3. CP Instance Health Check Table ==========
-- 用于跟踪 CP 实例的健康状态，支持故障检测

CREATE TABLE cp_instances (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    instance_id     VARCHAR(255) NOT NULL,
    host            VARCHAR(255) NOT NULL,
    port            INTEGER NOT NULL,
    status          VARCHAR(50) NOT NULL DEFAULT 'starting',
    version         VARCHAR(100),
    metadata        JSONB DEFAULT '{}',
    last_heartbeat  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT      uk_cp_instances_instance_id UNIQUE (instance_id)
);

CREATE INDEX idx_cp_instances_status ON cp_instances(status);
CREATE INDEX idx_cp_instances_last_heartbeat ON cp_instances(last_heartbeat);

-- 添加 updated_at 触发器
CREATE TRIGGER update_cp_instances_updated_at BEFORE UPDATE ON cp_instances
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ========== 4. Revision Publish Lock Helpers ==========
-- 为 revision 发布操作创建专门的锁 key 常量

-- 创建一个函数用于获取发布锁的 key
CREATE OR REPLACE FUNCTION get_revision_publish_lock_key(p_tenant_id UUID)
RETURNS VARCHAR AS $$
BEGIN
    IF p_tenant_id IS NULL THEN
        RETURN 'revision:publish:global';
    ELSE
        RETURN 'revision:publish:' || p_tenant_id::text;
    END IF;
END;
$$ language 'plpgsql' IMMUTABLE;
