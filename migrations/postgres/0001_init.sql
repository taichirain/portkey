-- Portkey API Gateway - Initial Schema
-- Version: 0001
-- Description: Core tables for configuration management

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Core entity tables

-- services: A service represents an upstream API or microservice
CREATE TABLE services (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        VARCHAR(255) NOT NULL,
    protocol    VARCHAR(10) NOT NULL DEFAULT 'http',
    host        VARCHAR(255),
    port        INTEGER,
    path        VARCHAR(1024),
    retries     INTEGER DEFAULT 5,
    connect_timeout INTEGER DEFAULT 60000,
    write_timeout   INTEGER DEFAULT 60000,
    read_timeout    INTEGER DEFAULT 60000,
    tags        TEXT[],
    enabled     BOOLEAN DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT  uk_services_name UNIQUE (name)
);

CREATE INDEX idx_services_enabled ON services(enabled);
CREATE INDEX idx_services_tags ON services USING GIN(tags);

-- routes: Routes define how requests are matched and routed to services
CREATE TABLE routes (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        VARCHAR(255),
    service_id  UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    protocols   TEXT[] NOT NULL DEFAULT '{"http"}',
    methods     TEXT[],
    hosts       TEXT[],
    paths       TEXT[],
    headers     JSONB,
    strip_path  BOOLEAN DEFAULT TRUE,
    preserve_host BOOLEAN DEFAULT FALSE,
    regex_priority INTEGER DEFAULT 0,
    tags        TEXT[],
    enabled     BOOLEAN DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT  uk_routes_name UNIQUE (name)
);

CREATE INDEX idx_routes_service_id ON routes(service_id);
CREATE INDEX idx_routes_enabled ON routes(enabled);
CREATE INDEX idx_routes_tags ON routes USING GIN(tags);
CREATE INDEX idx_routes_methods ON routes USING GIN(methods);
CREATE INDEX idx_routes_hosts ON routes USING GIN(hosts);
CREATE INDEX idx_routes_paths ON routes USING GIN(paths);

-- upstreams: Upstreams represent a group of target servers for load balancing
CREATE TABLE upstreams (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        VARCHAR(255) NOT NULL,
    algorithm   VARCHAR(50) NOT NULL DEFAULT 'round-robin',
    slots       INTEGER DEFAULT 10000,
    healthchecks JSONB,
    hash_on     VARCHAR(50),
    hash_fallback VARCHAR(50),
    hash_on_header VARCHAR(255),
    hash_fallback_header VARCHAR(255),
    hash_on_cookie VARCHAR(255),
    hash_on_cookie_path VARCHAR(1024),
    tags        TEXT[],
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT  uk_upstreams_name UNIQUE (name)
);

CREATE INDEX idx_upstreams_tags ON upstreams USING GIN(tags);


-- targets: Targets are individual servers within an upstream
CREATE TABLE targets (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    upstream_id UUID NOT NULL REFERENCES upstreams(id) ON DELETE CASCADE,
    target      VARCHAR(255) NOT NULL,
    port        INTEGER NOT NULL,
    weight      INTEGER DEFAULT 100,
    tags        TEXT[],
    enabled     BOOLEAN DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_targets_upstream_id ON targets(upstream_id);
CREATE INDEX idx_targets_enabled ON targets(enabled);
CREATE INDEX idx_targets_tags ON targets USING GIN(tags);
CREATE INDEX idx_targets_target_port ON targets(target, port);

-- consumers: Consumers represent users or applications that consume the API
CREATE TABLE consumers (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username    VARCHAR(255),
    custom_id   VARCHAR(255),
    tags        TEXT[],
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT  uk_consumers_username UNIQUE (username),
    CONSTRAINT  uk_consumers_custom_id UNIQUE (custom_id)
);

CREATE INDEX idx_consumers_tags ON consumers USING GIN(tags);

-- credentials: Authentication credentials linked to consumers
CREATE TABLE credentials (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    consumer_id UUID NOT NULL REFERENCES consumers(id) ON DELETE CASCADE,
    type        VARCHAR(50) NOT NULL,
    key         VARCHAR(255),
    secret      TEXT,
    algorithm   VARCHAR(50),
    claims      JSONB,
    tags        TEXT[],
    enabled     BOOLEAN DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_credentials_consumer_id ON credentials(consumer_id);
CREATE INDEX idx_credentials_type ON credentials(type);
CREATE INDEX idx_credentials_key ON credentials(key);
CREATE INDEX idx_credentials_enabled ON credentials(enabled);
CREATE INDEX idx_credentials_tags ON credentials USING GIN(tags);

-- plugins: Plugins that modify request/response processing
CREATE TABLE plugins (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        VARCHAR(255) NOT NULL,
    route_id    UUID REFERENCES routes(id) ON DELETE CASCADE,
    service_id  UUID REFERENCES services(id) ON DELETE CASCADE,
    consumer_id UUID REFERENCES consumers(id) ON DELETE CASCADE,
    config      JSONB NOT NULL,
    protocols   TEXT[] DEFAULT '{"http","https"}',
    enabled     BOOLEAN DEFAULT TRUE,
    run_on      VARCHAR(50) DEFAULT 'first',
    tags        TEXT[],
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_plugins_name ON plugins(name);
CREATE INDEX idx_plugins_route_id ON plugins(route_id);
CREATE INDEX idx_plugins_service_id ON plugins(service_id);
CREATE INDEX idx_plugins_consumer_id ON plugins(consumer_id);
CREATE INDEX idx_plugins_enabled ON plugins(enabled);
CREATE INDEX idx_plugins_tags ON plugins USING GIN(tags);

-- admins: Admin users for the control plane
CREATE TABLE admins (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username    VARCHAR(255) NOT NULL,
    email       VARCHAR(255),
    password_hash VARCHAR(255) NOT NULL,
    rbac_token  UUID,
    enabled     BOOLEAN DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT  uk_admins_username UNIQUE (username),
    CONSTRAINT  uk_admins_email UNIQUE (email)
);

CREATE INDEX idx_admins_enabled ON admins(enabled);

-- audit_logs: Audit trail for all write operations
CREATE TABLE audit_logs (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    admin_id    UUID REFERENCES admins(id) ON DELETE SET NULL,
    action      VARCHAR(50) NOT NULL,
    resource_type VARCHAR(100) NOT NULL,
    resource_id   UUID,
    old_value     JSONB,
    new_value     JSONB,
    client_ip     VARCHAR(50),
    user_agent    TEXT,
    request_id    VARCHAR(100),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX idx_audit_logs_admin_id ON audit_logs(admin_id);
CREATE INDEX idx_audit_logs_action ON audit_logs(action);
CREATE INDEX idx_audit_logs_resource_type ON audit_logs(resource_type);
CREATE INDEX idx_audit_logs_resource_id ON audit_logs(resource_id);

-- config_revisions: Snapshots of configuration for atomic deployment
CREATE TABLE config_revisions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    version     VARCHAR(100) NOT NULL,
    description TEXT,
    snapshot    JSONB NOT NULL,
    is_active   BOOLEAN DEFAULT FALSE,
    created_by  UUID REFERENCES admins(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_config_revisions_is_active ON config_revisions(is_active);
CREATE INDEX idx_config_revisions_created_at ON config_revisions(created_at DESC);
CREATE INDEX idx_config_revisions_published_at ON config_revisions(published_at DESC);
CREATE UNIQUE INDEX idx_config_revisions_single_active ON config_revisions(is_active) WHERE is_active = TRUE;

-- Updated_at trigger function
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Apply updated_at triggers to all tables with updated_at column
CREATE TRIGGER update_services_updated_at BEFORE UPDATE ON services
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_routes_updated_at BEFORE UPDATE ON routes
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_upstreams_updated_at BEFORE UPDATE ON upstreams
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_targets_updated_at BEFORE UPDATE ON targets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_consumers_updated_at BEFORE UPDATE ON consumers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_credentials_updated_at BEFORE UPDATE ON credentials
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_plugins_updated_at BEFORE UPDATE ON plugins
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_admins_updated_at BEFORE UPDATE ON admins
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_config_revisions_updated_at BEFORE UPDATE ON config_revisions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
