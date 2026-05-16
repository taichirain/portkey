-- Portkey API Gateway - Add Traffic Policies
-- Version: 0003
-- Description: Add traffic_policies table for canary/traffic splitting

-- traffic_policies: Traffic splitting and canary release policies
CREATE TABLE traffic_policies (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name                VARCHAR(255),
    route_id            UUID NOT NULL REFERENCES routes(id) ON DELETE CASCADE,
    priority            INTEGER NOT NULL DEFAULT 0,
    type                VARCHAR(32) NOT NULL,
    match_config        JSONB NOT NULL,
    target_service_id   UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    enabled             BOOLEAN DEFAULT TRUE,
    tags                TEXT[],
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT          uk_traffic_policies_route_priority UNIQUE (route_id, priority)
);

CREATE INDEX idx_traffic_policies_route_id ON traffic_policies(route_id);
CREATE INDEX idx_traffic_policies_target_service_id ON traffic_policies(target_service_id);
CREATE INDEX idx_traffic_policies_enabled ON traffic_policies(enabled);
CREATE INDEX idx_traffic_policies_tags ON traffic_policies USING GIN(tags);
CREATE INDEX idx_traffic_policies_type ON traffic_policies(type);

-- Add updated_at trigger for traffic_policies
CREATE TRIGGER update_traffic_policies_updated_at BEFORE UPDATE ON traffic_policies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
