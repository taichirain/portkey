-- Portkey API Gateway - Migration 0004
-- Description: Add multi-tenant and RBAC support

-- Enable UUID extension if not exists
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ========== 多租户表 ==========

-- tenants: 租户表
CREATE TABLE tenants (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        VARCHAR(255) NOT NULL,
    slug        VARCHAR(100) NOT NULL,
    description TEXT,
    settings    JSONB DEFAULT '{}',
    enabled     BOOLEAN DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT  uk_tenants_slug UNIQUE (slug)
);

CREATE INDEX idx_tenants_enabled ON tenants(enabled);
CREATE INDEX idx_tenants_slug ON tenants(slug);

-- ========== RBAC 表 ==========

-- permissions: 权限定义表（全局）
CREATE TABLE permissions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    resource    VARCHAR(100) NOT NULL,
    action      VARCHAR(50) NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT  uk_permissions_resource_action UNIQUE (resource, action)
);

CREATE INDEX idx_permissions_resource ON permissions(resource);
CREATE INDEX idx_permissions_action ON permissions(action);

-- roles: 角色表（租户隔离）
CREATE TABLE roles (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    is_system   BOOLEAN DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT  uk_roles_tenant_name UNIQUE (tenant_id, name)
);

CREATE INDEX idx_roles_tenant_id ON roles(tenant_id);
CREATE INDEX idx_roles_is_system ON roles(is_system);

-- role_permissions: 角色-权限关联表
CREATE TABLE role_permissions (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    role_id         UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id   UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT      uk_role_permissions UNIQUE (role_id, permission_id)
);

CREATE INDEX idx_role_permissions_role_id ON role_permissions(role_id);
CREATE INDEX idx_role_permissions_permission_id ON role_permissions(permission_id);

-- admin_roles: 管理员-角色关联表（支持跨租户）
CREATE TABLE admin_roles (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    admin_id    UUID NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    role_id     UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT  uk_admin_roles_admin_role_tenant UNIQUE (admin_id, role_id, tenant_id)
);

CREATE INDEX idx_admin_roles_admin_id ON admin_roles(admin_id);
CREATE INDEX idx_admin_roles_role_id ON admin_roles(role_id);
CREATE INDEX idx_admin_roles_tenant_id ON admin_roles(tenant_id);

-- ========== 核心表添加 tenant_id 字段 ==========

-- admins: 添加 tenant_id（管理员所属的默认租户，超级管理员可为空）
ALTER TABLE admins ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE SET NULL;

CREATE INDEX idx_admins_tenant_id ON admins(tenant_id);

-- services
ALTER TABLE services ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

CREATE INDEX idx_services_tenant_id ON services(tenant_id);

-- routes
ALTER TABLE routes ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

CREATE INDEX idx_routes_tenant_id ON routes(tenant_id);

-- upstreams
ALTER TABLE upstreams ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

CREATE INDEX idx_upstreams_tenant_id ON upstreams(tenant_id);

-- targets
ALTER TABLE targets ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

CREATE INDEX idx_targets_tenant_id ON targets(tenant_id);

-- consumers
ALTER TABLE consumers ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

CREATE INDEX idx_consumers_tenant_id ON consumers(tenant_id);

-- credentials
ALTER TABLE credentials ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

CREATE INDEX idx_credentials_tenant_id ON credentials(tenant_id);

-- plugins
ALTER TABLE plugins ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

CREATE INDEX idx_plugins_tenant_id ON plugins(tenant_id);

-- config_revisions
ALTER TABLE config_revisions ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

CREATE INDEX idx_config_revisions_tenant_id ON config_revisions(tenant_id);

-- audit_logs
ALTER TABLE audit_logs ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE SET NULL;

CREATE INDEX idx_audit_logs_tenant_id ON audit_logs(tenant_id);

-- traffic_policies
ALTER TABLE traffic_policies ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

CREATE INDEX idx_traffic_policies_tenant_id ON traffic_policies(tenant_id);

-- ========== 为新增表添加 updated_at 触发器 ==========

CREATE TRIGGER update_tenants_updated_at BEFORE UPDATE ON tenants
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_roles_updated_at BEFORE UPDATE ON roles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ========== 初始化默认权限数据 ==========

-- 服务权限
INSERT INTO permissions (resource, action, description) VALUES
    ('service', 'create', '创建服务'),
    ('service', 'read', '查看服务'),
    ('service', 'update', '更新服务'),
    ('service', 'delete', '删除服务');

-- 路由权限
INSERT INTO permissions (resource, action, description) VALUES
    ('route', 'create', '创建路由'),
    ('route', 'read', '查看路由'),
    ('route', 'update', '更新路由'),
    ('route', 'delete', '删除路由');

-- 上游权限
INSERT INTO permissions (resource, action, description) VALUES
    ('upstream', 'create', '创建上游'),
    ('upstream', 'read', '查看上游'),
    ('upstream', 'update', '更新上游'),
    ('upstream', 'delete', '删除上游');

-- 目标权限
INSERT INTO permissions (resource, action, description) VALUES
    ('target', 'create', '创建目标'),
    ('target', 'read', '查看目标'),
    ('target', 'update', '更新目标'),
    ('target', 'delete', '删除目标');

-- 消费者权限
INSERT INTO permissions (resource, action, description) VALUES
    ('consumer', 'create', '创建消费者'),
    ('consumer', 'read', '查看消费者'),
    ('consumer', 'update', '更新消费者'),
    ('consumer', 'delete', '删除消费者');

-- 凭证权限
INSERT INTO permissions (resource, action, description) VALUES
    ('credential', 'create', '创建凭证'),
    ('credential', 'read', '查看凭证'),
    ('credential', 'update', '更新凭证'),
    ('credential', 'delete', '删除凭证');

-- 插件权限
INSERT INTO permissions (resource, action, description) VALUES
    ('plugin', 'create', '创建插件'),
    ('plugin', 'read', '查看插件'),
    ('plugin', 'update', '更新插件'),
    ('plugin', 'delete', '删除插件');

-- 版本权限
INSERT INTO permissions (resource, action, description) VALUES
    ('revision', 'create', '创建版本'),
    ('revision', 'read', '查看版本'),
    ('revision', 'publish', '发布版本'),
    ('revision', 'rollback', '回滚版本');

-- 审计权限
INSERT INTO permissions (resource, action, description) VALUES
    ('audit', 'read', '查看审计日志');

-- 管理员权限
INSERT INTO permissions (resource, action, description) VALUES
    ('admin', 'create', '创建管理员'),
    ('admin', 'read', '查看管理员'),
    ('admin', 'update', '更新管理员'),
    ('admin', 'delete', '删除管理员');

-- 角色权限
INSERT INTO permissions (resource, action, description) VALUES
    ('role', 'create', '创建角色'),
    ('role', 'read', '查看角色'),
    ('role', 'update', '更新角色'),
    ('role', 'delete', '删除角色'),
    ('role', 'assign', '分配角色');

-- 租户权限（仅超级管理员）
INSERT INTO permissions (resource, action, description) VALUES
    ('tenant', 'create', '创建租户'),
    ('tenant', 'read', '查看租户'),
    ('tenant', 'update', '更新租户'),
    ('tenant', 'delete', '删除租户');

-- 流量策略权限
INSERT INTO permissions (resource, action, description) VALUES
    ('traffic_policy', 'create', '创建流量策略'),
    ('traffic_policy', 'read', '查看流量策略'),
    ('traffic_policy', 'update', '更新流量策略'),
    ('traffic_policy', 'delete', '删除流量策略');
