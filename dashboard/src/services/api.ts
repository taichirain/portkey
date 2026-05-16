import axios, { AxiosInstance, AxiosError, AxiosResponse } from 'axios';
import * as types from '../types';

const API_BASE_URL = '/api/v1';

interface ApiResponse<T = unknown> {
  success: boolean;
  data?: T;
  error?: {
    message: string;
    code?: string;
  };
}

class ApiService {
  private client: AxiosInstance;
  private token: string | null = null;

  constructor() {
    this.client = axios.create({
      baseURL: API_BASE_URL,
      timeout: 30000,
      headers: {
        'Content-Type': 'application/json',
      },
    });

    this.token = localStorage.getItem('auth_token');
    if (this.token) {
      this.setAuthToken(this.token);
    }

    this.client.interceptors.response.use(
      (response: AxiosResponse<ApiResponse>) => {
        if (response.data && response.data.success && response.data.data !== undefined) {
          return { ...response, data: response.data.data } as AxiosResponse;
        }
        if (response.data && response.data.success) {
          return { ...response, data: null } as AxiosResponse;
        }
        return response;
      },
      (error: AxiosError) => {
        if (error.response?.status === 401) {
          const requestUrl = error.config?.url || '';
          if (!requestUrl.includes('/login')) {
            this.clearAuthToken();
            window.location.href = '/login';
          }
        }
        return Promise.reject(error);
      }
    );
  }

  setAuthToken(token: string) {
    this.token = token;
    localStorage.setItem('auth_token', token);
    this.client.defaults.headers.common['Authorization'] = `Bearer ${token}`;
  }

  clearAuthToken() {
    this.token = null;
    localStorage.removeItem('auth_token');
    delete this.client.defaults.headers.common['Authorization'];
  }

  isAuthenticated(): boolean {
    return !!this.token;
  }

  getToken(): string | null {
    return this.token;
  }

  async login(request: types.LoginRequest): Promise<types.LoginResponse> {
    const response = await this.client.post<types.LoginResponse>('/login', request);
    if (response.data && (response.data as types.LoginResponse).token) {
      this.setAuthToken((response.data as types.LoginResponse).token);
    }
    return response.data as types.LoginResponse;
  }

  async logout(): Promise<void> {
    this.clearAuthToken();
  }

  async getServices(page = 1, pageSize = 50): Promise<types.PaginationResponse<types.Service>> {
    const response = await this.client.get<types.PaginationResponse<types.Service>>('/services', {
      params: { page, page_size: pageSize }
    });
    return response.data as types.PaginationResponse<types.Service>;
  }

  async getServiceById(id: string): Promise<types.Service> {
    const response = await this.client.get<types.Service>('/services', {
      params: { id }
    });
    return response.data as types.Service;
  }

  async getServiceByName(name: string): Promise<types.Service> {
    const response = await this.client.get<types.Service>('/services', {
      params: { name }
    });
    return response.data as types.Service;
  }

  async createService(request: types.CreateServiceRequest): Promise<types.Service> {
    const response = await this.client.post<types.Service>('/services', request);
    return response.data as types.Service;
  }

  async updateService(id: string, request: types.UpdateServiceRequest): Promise<types.Service> {
    const response = await this.client.put<types.Service>(`/services?id=${id}`, request);
    return response.data as types.Service;
  }

  async deleteService(id: string): Promise<void> {
    await this.client.delete(`/services?id=${id}`);
  }

  async getRoutes(page = 1, pageSize = 50): Promise<types.PaginationResponse<types.Route>> {
    const response = await this.client.get<types.PaginationResponse<types.Route>>('/routes', {
      params: { page, page_size: pageSize }
    });
    return response.data as types.PaginationResponse<types.Route>;
  }

  async getRouteById(id: string): Promise<types.Route> {
    const response = await this.client.get<types.Route>('/routes', {
      params: { id }
    });
    return response.data as types.Route;
  }

  async createRoute(request: types.CreateRouteRequest): Promise<types.Route> {
    const response = await this.client.post<types.Route>('/routes', request);
    return response.data as types.Route;
  }

  async updateRoute(id: string, request: types.UpdateRouteRequest): Promise<types.Route> {
    const response = await this.client.put<types.Route>(`/routes?id=${id}`, request);
    return response.data as types.Route;
  }

  async deleteRoute(id: string): Promise<void> {
    await this.client.delete(`/routes?id=${id}`);
  }

  async getUpstreams(page = 1, pageSize = 50): Promise<types.PaginationResponse<types.Upstream>> {
    const response = await this.client.get<types.PaginationResponse<types.Upstream>>('/upstreams', {
      params: { page, page_size: pageSize }
    });
    return response.data as types.PaginationResponse<types.Upstream>;
  }

  async getUpstreamById(id: string): Promise<types.Upstream> {
    const response = await this.client.get<types.Upstream>('/upstreams', {
      params: { id }
    });
    return response.data as types.Upstream;
  }

  async getUpstreamByName(name: string): Promise<types.Upstream> {
    const response = await this.client.get<types.Upstream>('/upstreams', {
      params: { name }
    });
    return response.data as types.Upstream;
  }

  async createUpstream(request: types.CreateUpstreamRequest): Promise<types.Upstream> {
    const response = await this.client.post<types.Upstream>('/upstreams', request);
    return response.data as types.Upstream;
  }

  async updateUpstream(id: string, request: types.UpdateUpstreamRequest): Promise<types.Upstream> {
    const response = await this.client.put<types.Upstream>(`/upstreams?id=${id}`, request);
    return response.data as types.Upstream;
  }

  async deleteUpstream(id: string): Promise<void> {
    await this.client.delete(`/upstreams?id=${id}`);
  }

  async getTargets(page = 1, pageSize = 50): Promise<types.PaginationResponse<types.Target>> {
    const response = await this.client.get<types.PaginationResponse<types.Target>>('/targets', {
      params: { page, page_size: pageSize }
    });
    return response.data as types.PaginationResponse<types.Target>;
  }

  async getTargetById(id: string): Promise<types.Target> {
    const response = await this.client.get<types.Target>('/targets', {
      params: { id }
    });
    return response.data as types.Target;
  }

  async getTargetsByUpstreamId(upstreamId: string): Promise<types.Target[]> {
    const response = await this.client.get<types.Target[]>('/targets', {
      params: { upstream_id: upstreamId }
    });
    return response.data as types.Target[];
  }

  async createTarget(request: types.CreateTargetRequest): Promise<types.Target> {
    const response = await this.client.post<types.Target>('/targets', request);
    return response.data as types.Target;
  }

  async updateTarget(id: string, request: types.UpdateTargetRequest): Promise<types.Target> {
    const response = await this.client.put<types.Target>(`/targets?id=${id}`, request);
    return response.data as types.Target;
  }

  async deleteTarget(id: string): Promise<void> {
    await this.client.delete(`/targets?id=${id}`);
  }

  async getConsumers(page = 1, pageSize = 50): Promise<types.PaginationResponse<types.Consumer>> {
    const response = await this.client.get<types.PaginationResponse<types.Consumer>>('/consumers', {
      params: { page, page_size: pageSize }
    });
    return response.data as types.PaginationResponse<types.Consumer>;
  }

  async getConsumerById(id: string): Promise<types.Consumer> {
    const response = await this.client.get<types.Consumer>('/consumers', {
      params: { id }
    });
    return response.data as types.Consumer;
  }

  async getConsumerByUsername(username: string): Promise<types.Consumer> {
    const response = await this.client.get<types.Consumer>('/consumers', {
      params: { username }
    });
    return response.data as types.Consumer;
  }

  async getConsumerByCustomId(customId: string): Promise<types.Consumer> {
    const response = await this.client.get<types.Consumer>('/consumers', {
      params: { custom_id: customId }
    });
    return response.data as types.Consumer;
  }

  async createConsumer(request: types.CreateConsumerRequest): Promise<types.Consumer> {
    const response = await this.client.post<types.Consumer>('/consumers', request);
    return response.data as types.Consumer;
  }

  async updateConsumer(id: string, request: types.UpdateConsumerRequest): Promise<types.Consumer> {
    const response = await this.client.put<types.Consumer>(`/consumers?id=${id}`, request);
    return response.data as types.Consumer;
  }

  async deleteConsumer(id: string): Promise<void> {
    await this.client.delete(`/consumers?id=${id}`);
  }

  async getPlugins(page = 1, pageSize = 50): Promise<types.PaginationResponse<types.Plugin>> {
    const response = await this.client.get<types.PaginationResponse<types.Plugin>>('/plugins', {
      params: { page, page_size: pageSize }
    });
    return response.data as types.PaginationResponse<types.Plugin>;
  }

  async getPluginById(id: string): Promise<types.Plugin> {
    const response = await this.client.get<types.Plugin>('/plugins', {
      params: { id }
    });
    return response.data as types.Plugin;
  }

  async getPluginsByName(name: string): Promise<types.Plugin[]> {
    const response = await this.client.get<types.Plugin[]>('/plugins', {
      params: { name }
    });
    return response.data as types.Plugin[];
  }

  async getPluginsByRouteId(routeId: string): Promise<types.Plugin[]> {
    const response = await this.client.get<types.Plugin[]>('/plugins', {
      params: { route_id: routeId }
    });
    return response.data as types.Plugin[];
  }

  async getPluginsByServiceId(serviceId: string): Promise<types.Plugin[]> {
    const response = await this.client.get<types.Plugin[]>('/plugins', {
      params: { service_id: serviceId }
    });
    return response.data as types.Plugin[];
  }

  async getPluginsByConsumerId(consumerId: string): Promise<types.Plugin[]> {
    const response = await this.client.get<types.Plugin[]>('/plugins', {
      params: { consumer_id: consumerId }
    });
    return response.data as types.Plugin[];
  }

  async getGlobalPlugins(): Promise<types.Plugin[]> {
    const response = await this.client.get<types.Plugin[]>('/plugins/global');
    return response.data as types.Plugin[];
  }

  async createPlugin(request: types.CreatePluginRequest): Promise<types.Plugin> {
    const response = await this.client.post<types.Plugin>('/plugins', request);
    return response.data as types.Plugin;
  }

  async updatePlugin(id: string, request: types.UpdatePluginRequest): Promise<types.Plugin> {
    const response = await this.client.put<types.Plugin>(`/plugins?id=${id}`, request);
    return response.data as types.Plugin;
  }

  async deletePlugin(id: string): Promise<void> {
    await this.client.delete(`/plugins?id=${id}`);
  }

  async getRevisions(page = 1, pageSize = 50): Promise<types.PaginationResponse<types.Revision>> {
    const response = await this.client.get<types.PaginationResponse<types.Revision>>('/revisions', {
      params: { page, page_size: pageSize }
    });
    return response.data as types.PaginationResponse<types.Revision>;
  }

  async getRevisionById(id: string): Promise<types.Revision> {
    const response = await this.client.get<types.Revision>('/revisions', {
      params: { id }
    });
    return response.data as types.Revision;
  }

  async getActiveRevision(): Promise<types.ActiveRevisionResponse> {
    const response = await this.client.get<types.ActiveRevisionResponse>('/revisions/active');
    return response.data as types.ActiveRevisionResponse;
  }

  async validateConfig(): Promise<types.ValidationResponse> {
    const response = await this.client.post<types.ValidationResponse>('/revisions/validate');
    return response.data as types.ValidationResponse;
  }

  async createSnapshot(): Promise<types.SnapshotResponse> {
    const response = await this.client.post<types.SnapshotResponse>('/revisions/snapshot');
    return response.data as types.SnapshotResponse;
  }

  async createRevision(request: types.CreateRevisionRequest): Promise<types.Revision> {
    const response = await this.client.post<types.Revision>('/revisions', request);
    return response.data as types.Revision;
  }

  async publishRevision(request: types.PublishRevisionRequest): Promise<types.Revision> {
    const response = await this.client.post<types.Revision>('/revisions/publish', request);
    return response.data as types.Revision;
  }

  async createAndPublish(request: types.CreateRevisionRequest): Promise<types.Revision> {
    const response = await this.client.post<types.Revision>('/revisions/create-and-publish', request);
    return response.data as types.Revision;
  }

  async rollback(request: types.RollbackRequest): Promise<types.Revision> {
    const response = await this.client.post<types.Revision>('/revisions/rollback', request);
    return response.data as types.Revision;
  }

  async getPublicActiveRevision(): Promise<types.ActiveRevisionResponse> {
    const response = await axios.get<ApiResponse<types.ActiveRevisionResponse>>('/api/v1/public/active-revision');
    if (response.data && response.data.success && response.data.data) {
      return response.data.data;
    }
    return response.data as unknown as types.ActiveRevisionResponse;
  }

  async getAuditLogs(page = 1, pageSize = 50): Promise<types.PaginationResponse<types.AuditLog>> {
    const response = await this.client.get<types.PaginationResponse<types.AuditLog>>('/audit-logs', {
      params: { page, page_size: pageSize }
    });
    return response.data as types.PaginationResponse<types.AuditLog>;
  }

  async getTrafficPolicies(page = 1, pageSize = 50): Promise<types.PaginationResponse<types.TrafficPolicy>> {
    const response = await this.client.get<types.PaginationResponse<types.TrafficPolicy>>('/traffic-policies', {
      params: { page, page_size: pageSize }
    });
    return response.data as types.PaginationResponse<types.TrafficPolicy>;
  }

  async getTrafficPolicyById(id: string): Promise<types.TrafficPolicy> {
    const response = await this.client.get<types.TrafficPolicy>('/traffic-policies', {
      params: { id }
    });
    return response.data as types.TrafficPolicy;
  }

  async getTrafficPoliciesByRouteId(routeId: string): Promise<types.TrafficPolicy[]> {
    const response = await this.client.get<types.TrafficPolicy[]>('/traffic-policies', {
      params: { route_id: routeId }
    });
    return response.data as types.TrafficPolicy[];
  }

  async createTrafficPolicy(request: types.CreateTrafficPolicyRequest): Promise<types.TrafficPolicy> {
    const response = await this.client.post<types.TrafficPolicy>('/traffic-policies', request);
    return response.data as types.TrafficPolicy;
  }

  async updateTrafficPolicy(id: string, request: types.UpdateTrafficPolicyRequest): Promise<types.TrafficPolicy> {
    const response = await this.client.put<types.TrafficPolicy>(`/traffic-policies?id=${id}`, request);
    return response.data as types.TrafficPolicy;
  }

  async deleteTrafficPolicy(id: string): Promise<void> {
    await this.client.delete(`/traffic-policies?id=${id}`);
  }

  async getMonitoringMetrics(): Promise<types.MonitoringMetricsResponse> {
    const response = await this.client.get<types.MonitoringMetricsResponse>('/monitoring/metrics');
    return response.data as types.MonitoringMetricsResponse;
  }

  async getDPStatus(): Promise<types.DPStatusResponse> {
    const response = await this.client.get<types.DPStatusResponse>('/monitoring/dp-status');
    return response.data as types.DPStatusResponse;
  }
}

export const apiService = new ApiService();
export default apiService;
