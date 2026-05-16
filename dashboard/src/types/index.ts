export interface ApiResponse<T = unknown> {
  success: boolean;
  data?: T;
  error?: {
    message: string;
    code?: string;
  };
}

export interface PaginationResponse<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  token: string;
  expires_in: number;
  admin_id: string;
  username: string;
  tenant_id?: string;
  roles?: string[];
  permissions?: string[];
}

export interface User {
  id: string;
  username: string;
  tenantId?: string;
  roles: string[];
  permissions: string[];
}

export const SystemRoles = {
  SUPER_ADMIN: 'super_admin',
  TENANT_ADMIN: 'tenant_admin',
  DEVELOPER: 'developer',
  VIEWER: 'viewer',
} as const;

export type SystemRoleType = typeof SystemRoles[keyof typeof SystemRoles];

export const Permissions = {
  SERVICES_READ: 'service:read',
  SERVICES_WRITE: 'service:update',
  SERVICES_DELETE: 'service:delete',
  SERVICES_CREATE: 'service:create',
  ROUTES_READ: 'route:read',
  ROUTES_WRITE: 'route:update',
  ROUTES_DELETE: 'route:delete',
  ROUTES_CREATE: 'route:create',
  UPSTREAMS_READ: 'upstream:read',
  UPSTREAMS_WRITE: 'upstream:update',
  UPSTREAMS_DELETE: 'upstream:delete',
  UPSTREAMS_CREATE: 'upstream:create',
  TARGETS_READ: 'target:read',
  TARGETS_WRITE: 'target:update',
  TARGETS_DELETE: 'target:delete',
  TARGETS_CREATE: 'target:create',
  CONSUMERS_READ: 'consumer:read',
  CONSUMERS_WRITE: 'consumer:update',
  CONSUMERS_DELETE: 'consumer:delete',
  CONSUMERS_CREATE: 'consumer:create',
  PLUGINS_READ: 'plugin:read',
  PLUGINS_WRITE: 'plugin:update',
  PLUGINS_DELETE: 'plugin:delete',
  PLUGINS_CREATE: 'plugin:create',
  REVISIONS_READ: 'revision:read',
  REVISIONS_WRITE: 'revision:update',
  REVISIONS_DELETE: 'revision:delete',
  REVISIONS_CREATE: 'revision:create',
  REVISIONS_PUBLISH: 'revision:publish',
  REVISIONS_ROLLBACK: 'revision:rollback',
  AUDITS_READ: 'audit:read',
  ADMINS_READ: 'admin:read',
  ADMINS_WRITE: 'admin:update',
  ADMINS_DELETE: 'admin:delete',
  ADMINS_CREATE: 'admin:create',
  ROLES_READ: 'role:read',
  ROLES_WRITE: 'role:update',
  ROLES_DELETE: 'role:delete',
  ROLES_CREATE: 'role:create',
  ROLES_ASSIGN: 'role:assign',
  TENANTS_READ: 'tenant:read',
  TENANTS_WRITE: 'tenant:update',
  TENANTS_DELETE: 'tenant:delete',
  TENANTS_CREATE: 'tenant:create',
  CREDENTIALS_READ: 'credential:read',
  CREDENTIALS_WRITE: 'credential:update',
  CREDENTIALS_DELETE: 'credential:delete',
  CREDENTIALS_CREATE: 'credential:create',
  TRAFFIC_POLICIES_READ: 'traffic_policy:read',
  TRAFFIC_POLICIES_WRITE: 'traffic_policy:update',
  TRAFFIC_POLICIES_DELETE: 'traffic_policy:delete',
  TRAFFIC_POLICIES_CREATE: 'traffic_policy:create',
} as const;

export type PermissionType = typeof Permissions[keyof typeof Permissions];

export interface Service {
  id: string;
  name: string;
  protocol: 'http' | 'https';
  host: string;
  port: number;
  path: string;
  upstream_id: string;
  retries: number;
  connect_timeout: number;
  write_timeout: number;
  read_timeout: number;
  tags: string[];
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface Route {
  id: string;
  name: string;
  service_id: string;
  protocols: string[];
  methods: string[];
  hosts: string[];
  paths: string[];
  headers: Record<string, string[]>;
  strip_path: boolean;
  preserve_host: boolean;
  regex_priority: number;
  tags: string[];
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface Upstream {
  id: string;
  name: string;
  algorithm: string;
  slots: number;
  tags: string[];
  created_at: string;
  updated_at: string;
}

export interface Target {
  id: string;
  upstream_id: string;
  target: string;
  port: number;
  weight: number;
  tags: string[];
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface Consumer {
  id: string;
  username: string;
  custom_id: string;
  tags: string[];
  created_at: string;
  updated_at: string;
}

export interface Plugin {
  id: string;
  name: string;
  route_id?: string;
  service_id?: string;
  consumer_id?: string;
  config: Record<string, unknown>;
  protocols: string[];
  enabled: boolean;
  run_on: 'first' | 'second' | 'last' | 'all';
  tags: string[];
  created_at: string;
  updated_at: string;
}

export interface Revision {
  id: string;
  version: string;
  description: string;
  snapshot: SnapshotData;
  created_at: string;
  updated_at: string;
  published_at?: string;
  created_by?: string;
}

export interface CreateServiceRequest {
  name: string;
  protocol?: 'http' | 'https';
  host?: string;
  port?: number;
  path?: string;
  upstream_id?: string;
  retries?: number;
  connect_timeout?: number;
  write_timeout?: number;
  read_timeout?: number;
  tags?: string[];
  enabled?: boolean;
}

export interface UpdateServiceRequest {
  name?: string;
  protocol?: string;
  host?: string;
  port?: number;
  path?: string;
  upstream_id?: string;
  retries?: number;
  connect_timeout?: number;
  write_timeout?: number;
  read_timeout?: number;
  tags?: string[];
  enabled?: boolean;
}

export interface CreateRouteRequest {
  name: string;
  service_id: string;
  protocols?: string[];
  methods?: string[];
  hosts?: string[];
  paths?: string[];
  headers?: Record<string, string[]>;
  strip_path?: boolean;
  preserve_host?: boolean;
  regex_priority?: number;
  tags?: string[];
  enabled?: boolean;
}

export interface UpdateRouteRequest {
  name?: string;
  service_id?: string;
  protocols?: string[];
  methods?: string[];
  hosts?: string[];
  paths?: string[];
  headers?: Record<string, string[]>;
  strip_path?: boolean;
  preserve_host?: boolean;
  regex_priority?: number;
  tags?: string[];
  enabled?: boolean;
}

export interface CreateUpstreamRequest {
  name: string;
  algorithm?: string;
  slots?: number;
  tags?: string[];
}

export interface UpdateUpstreamRequest {
  name?: string;
  algorithm?: string;
  slots?: number;
  tags?: string[];
}

export interface CreateTargetRequest {
  upstream_id: string;
  target: string;
  port: number;
  weight?: number;
  tags?: string[];
  enabled?: boolean;
}

export interface UpdateTargetRequest {
  target?: string;
  port?: number;
  weight?: number;
  tags?: string[];
  enabled?: boolean;
}

export interface CreateConsumerRequest {
  username?: string;
  custom_id?: string;
  tags?: string[];
}

export interface UpdateConsumerRequest {
  username?: string;
  custom_id?: string;
  tags?: string[];
}

export interface CreatePluginRequest {
  name: string;
  route_id?: string;
  service_id?: string;
  consumer_id?: string;
  config: Record<string, unknown>;
  protocols?: string[];
  enabled?: boolean;
  run_on?: string;
  tags?: string[];
}

export interface UpdatePluginRequest {
  name?: string;
  route_id?: string;
  service_id?: string;
  consumer_id?: string;
  config?: Record<string, unknown>;
  protocols?: string[];
  enabled?: boolean;
  run_on?: string;
  tags?: string[];
}

export interface CreateRevisionRequest {
  version: string;
  description?: string;
}

export interface PublishRevisionRequest {
  revision_id: string;
}

export interface RollbackRequest {
  target_revision_id: string;
}

export interface ValidationResponse {
  valid: boolean;
  errors?: { field: string; message: string }[];
}

export interface SnapshotResponse {
  timestamp: string;
  services_count: number;
  routes_count: number;
  upstreams_count: number;
}

export interface SnapshotData {
  services?: unknown[];
  routes?: unknown[];
  upstreams?: unknown[];
  targets?: unknown[];
  consumers?: unknown[];
  plugins?: unknown[];
}

export interface ActiveRevisionResponse {
  revision_id: string;
  version: string;
  description: string;
  published_at?: string;
  snapshot: SnapshotData;
}

export interface AuditLog {
  id: string;
  tenant_id: string;
  admin_id?: string;
  action: string;
  resource_type: string;
  resource_id?: string;
  old_value?: Record<string, unknown>;
  new_value?: Record<string, unknown>;
  client_ip: string;
  user_agent: string;
  request_id: string;
  created_at: string;
}

export type TrafficPolicyType = 
  | 'header' 
  | 'weight' 
  | 'consumer' 
  | 'query' 
  | 'cookie' 
  | 'ip' 
  | 'path' 
  | 'method' 
  | 'tag' 
  | 'compound' 
  | 'fallback';

export type MatchOperator = 
  | 'exact' 
  | 'prefix' 
  | 'suffix' 
  | 'contains' 
  | 'regex' 
  | 'not_exact' 
  | 'not_contains' 
  | 'greater_than' 
  | 'less_than' 
  | 'greater_equal' 
  | 'less_equal';

export type CompoundOperator = 'and' | 'or';

export type TagMatchMode = 'any' | 'all' | 'exact';

export interface HeaderMatchConfig {
  header: string;
  value: string;
  operator?: MatchOperator;
}

export interface WeightMatchConfig {
  percentage: number;
}

export interface QueryMatchConfig {
  key: string;
  value: string;
  operator?: MatchOperator;
}

export interface CookieMatchConfig {
  name: string;
  value?: string;
  operator?: MatchOperator;
}

export interface IPMatchConfig {
  ip_list?: string[];
  cidr_list?: string[];
  negate?: boolean;
}

export interface PathMatchConfig {
  pattern: string;
  operator?: MatchOperator;
}

export interface MethodMatchConfig {
  methods: string[];
  negate?: boolean;
}

export interface TagMatchConfig {
  tags: string[];
  match_mode?: TagMatchMode;
  source_type?: string;
}

export interface ConsumerMatchConfig {
  consumer_ids?: string[];
  tags?: string[];
  match_mode?: TagMatchMode;
}

export interface CompoundCondition {
  type: TrafficPolicyType;
  match_config: Record<string, unknown>;
}

export interface CompoundMatchConfig {
  operator: CompoundOperator;
  conditions: CompoundCondition[];
}

export interface FallbackMatchConfig {
  fallback_service_id: string;
  health_check_type?: string;
  min_healthy_targets?: number;
}

export interface TrafficPolicy {
  id: string;
  tenant_id: string;
  name: string;
  route_id: string;
  priority: number;
  type: TrafficPolicyType;
  match_config: Record<string, unknown>;
  target_service_id: string;
  enabled: boolean;
  tags: string[];
  rule_set_id?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateTrafficPolicyRequest {
  name?: string;
  route_id: string;
  priority?: number;
  type: TrafficPolicyType;
  match_config: Record<string, unknown>;
  target_service_id: string;
  enabled?: boolean;
  tags?: string[];
}

export interface UpdateTrafficPolicyRequest {
  name?: string;
  route_id?: string;
  priority?: number;
  type?: TrafficPolicyType;
  match_config?: Record<string, unknown>;
  target_service_id?: string;
  enabled?: boolean;
  tags?: string[];
}

export interface DPStatus {
  revision_id: string;
  version?: string;
  last_updated_at?: string;
  healthy: boolean;
  error?: string;
}

export interface PublishResult {
  success: boolean;
  revision_id: string;
  version: string;
  dp_status?: DPStatus[];
  error?: string;
}

export interface RollbackResult {
  success: boolean;
  from_revision_id: string;
  to_revision_id: string;
  to_version: string;
  dp_status?: DPStatus[];
  error?: string;
}

export interface StatusDistribution {
  '2xx': number;
  '3xx': number;
  '4xx': number;
  '5xx': number;
}

export interface AggregatedMetrics {
  qps_1m: number;
  error_rate_1m: number;
  avg_latency_ms_1m: number;
  requests_active: number;
  requests_total: number;
  errors_total: number;
  rate_limited_total: number;
  policy_hit_total: number;
  status_distribution: StatusDistribution;
}

export interface DPInstanceMetrics {
  name: string;
  url: string;
  online: boolean;
  qps_1m: number;
  error_rate_1m: number;
  avg_latency_ms_1m: number;
  requests_active: number;
  requests_total: number;
  errors_total: number;
  rate_limited_total: number;
  policy_hit_total: number;
  status_distribution: StatusDistribution;
  revision_mismatch?: boolean;
  uptime_seconds: number;
}

export interface ActiveRevisionInfo {
  id: string;
  version: string;
  published_at?: string;
}

export interface MonitoringMetricsResponse {
  aggregated: AggregatedMetrics;
  per_dp: DPInstanceMetrics[];
  active_revision?: ActiveRevisionInfo;
}

export interface ConfigStats {
  services_count: number;
  routes_count: number;
  upstreams_count: number;
  traffic_policies_count: number;
}

export interface DPStatusInstance {
  name: string;
  url: string;
  online: boolean;
  revision_id: string;
  revision_mismatch: boolean;
  uptime_seconds: number;
  config_stats: ConfigStats;
}

export interface DPStatusResponse {
  cp_active_revision_id: string;
  cp_active_revision_version: string;
  dp_instances: DPStatusInstance[];
}
