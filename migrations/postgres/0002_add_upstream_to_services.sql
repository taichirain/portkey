-- Portkey API Gateway - Migration 0002
-- Description: Add upstream_id column to services table

ALTER TABLE services ADD COLUMN upstream_id UUID REFERENCES upstreams(id) ON DELETE SET NULL;

CREATE INDEX idx_services_upstream_id ON services(upstream_id);
