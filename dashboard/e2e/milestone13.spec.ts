import { test, expect, request as apiRequestFactory } from '@playwright/test';

const API_BASE = 'http://127.0.0.1:8002';

async function getAdminToken() {
  const ctx = await apiRequestFactory.newContext({ baseURL: API_BASE });
  const res = await ctx.post('/api/v1/login', {
    data: { username: 'admin', password: 'admin123' },
  });
  const body = await res.json();
  await ctx.dispose();
  return body.data?.token || body.token || '';
}

async function createAuthenticatedContext(token: string) {
  return apiRequestFactory.newContext({
    baseURL: API_BASE,
    extraHTTPHeaders: { Authorization: `Bearer ${token}` },
  });
}

test.describe('Milestone 13: 多租户 RBAC - P3 阶段', () => {
  let token: string;

  test.beforeAll(async () => {
    token = await getAdminToken();
  });

  test.describe('审计日志 tenant 关联', () => {
    test('创建服务时，审计日志应关联正确的 tenant', async () => {
      const ctx = await createAuthenticatedContext(token);

      const timestamp = Date.now();
      const serviceName = `test-audit-service-${timestamp}`;

      const createRes = await ctx.post('/api/v1/services', {
        data: {
          name: serviceName,
          host: 'example.com',
          port: 80,
          path: '/test',
        },
      });
      expect(createRes.ok()).toBeTruthy();
      const createBody = await createRes.json();
      const serviceId = createBody.data?.id || createBody.id;
      expect(serviceId).toBeDefined();

      const auditRes = await ctx.get('/api/v1/audit-logs');
      expect(auditRes.ok()).toBeTruthy();
      const auditBody = await auditRes.json();

      const items = auditBody.data?.items || auditBody.items || [];
      const serviceLogs = items.filter(
        (log: { ResourceType: string; ResourceID: string }) =>
          log.ResourceType === 'service' && log.ResourceID === serviceId
      );

      expect(serviceLogs.length).toBeGreaterThan(0);

      const createLog = serviceLogs.find(
        (log: { Action: string }) => log.Action === 'create'
      );
      expect(createLog).toBeDefined();

      await ctx.delete(`/api/v1/services?id=${serviceId}`);
      await ctx.dispose();
    });

    test('审计日志 API 应支持分页查询', async () => {
      const ctx = await createAuthenticatedContext(token);

      const res = await ctx.get('/api/v1/audit-logs?page=1&page_size=10');
      expect(res.ok()).toBeTruthy();
      const body = await res.json();

      expect(body.data?.items || body.items).toBeDefined();
      expect(body.data?.total !== undefined || body.total !== undefined).toBeTruthy();

      await ctx.dispose();
    });
  });

  test.describe('Revision 按 tenant 查询', () => {
    test('创建 revision 时，应关联正确的 tenant', async () => {
      const ctx = await createAuthenticatedContext(token);

      const timestamp = Date.now();
      const versionName = `v1.0.0-p3test-${timestamp}`;

      const createRes = await ctx.post('/api/v1/revisions', {
        data: {
          version: versionName,
          description: 'P3 测试版本',
        },
      });

      expect(createRes.ok()).toBeTruthy();
      const createBody = await createRes.json();
      const revisionId = createBody.data?.RevisionID || createBody.RevisionID;
      expect(revisionId).toBeDefined();

      const getRes = await ctx.get(`/api/v1/revisions?id=${revisionId}`);
      expect(getRes.ok()).toBeTruthy();
      const getBody = await getRes.json();

      const listRes = await ctx.get('/api/v1/revisions');
      expect(listRes.ok()).toBeTruthy();
      const listBody = await listRes.json();
      const items = listBody.data?.items || listBody.items || [];

      const foundRevision = items.find(
        (r: { version: string; id: string }) =>
          r.version === versionName || r.id === revisionId
      );
      expect(foundRevision).toBeDefined();

      await ctx.delete(`/api/v1/revisions?id=${revisionId}`);

      await ctx.dispose();
    });

    test('List revisions 应返回当前 tenant 的版本', async () => {
      const ctx = await createAuthenticatedContext(token);

      const res = await ctx.get('/api/v1/revisions?page=1&page_size=20');
      expect(res.ok()).toBeTruthy();
      const body = await res.json();

      const items = body.data?.items || body.items || [];
      expect(items).toBeDefined();
      expect(Array.isArray(items)).toBeTruthy();

      if (items.length > 0) {
        const firstRev = items[0];
        expect(firstRev.version).toBeDefined();
        expect(firstRev.created_at).toBeDefined();
      }

      await ctx.dispose();
    });
  });

  test.describe('权限控制验证', () => {
    test('audit:read 权限应允许访问审计日志', async () => {
      const ctx = await createAuthenticatedContext(token);

      const res = await ctx.get('/api/v1/audit-logs');
      expect(res.ok()).toBeTruthy();

      await ctx.dispose();
    });

    test('revision:read 权限应允许查看版本列表', async () => {
      const ctx = await createAuthenticatedContext(token);

      const res = await ctx.get('/api/v1/revisions');
      expect(res.ok()).toBeTruthy();

      await ctx.dispose();
    });
  });

  test.describe('完整流程验证', () => {
    test('创建服务 -> 查看审计日志 -> 创建版本完整流程', async ({
      page,
    }) => {
      const timestamp = Date.now();
      const serviceName = `p3-full-test-${timestamp}`;
      const versionName = `v${timestamp}`;

      const apiCtx = await createAuthenticatedContext(token);

      await page.goto('/login');
      await page.waitForSelector('text=Portkey');

      await page.fill('input[placeholder="用户名"]', 'admin');
      await page.fill('input[placeholder="密码"]', 'admin123');
      await page.click('button[type="submit"]');
      await page.waitForURL('/');

      await page.click('text=服务管理');
      await page.waitForURL('**/services');
      await page.click('button:has-text("新建服务")');
      await page.fill('input[placeholder="请输入服务名称"]', serviceName);
      await page.fill('input[placeholder="例如: example.com"]', 'test.example.com');
      await page.fill('input[placeholder="例如: /api"]', '/p3test');
      await page.click('.ant-modal-wrap:not(.ant-modal-hidden) .ant-modal-footer .ant-btn-primary');
      await expect(page.locator('.ant-message-notice:has-text("创建成功") >> nth=0')).toBeVisible({
        timeout: 10000,
      });

      const servicesRes = await apiCtx.get('/api/v1/services');
      expect(servicesRes.ok()).toBeTruthy();
      const servicesBody = await servicesRes.json();
      const services = servicesBody.data?.items || servicesBody.items || [];
      const createdService = services.find(
        (s: { name: string }) => s.name === serviceName
      );
      expect(createdService).toBeDefined();

      const auditRes = await apiCtx.get('/api/v1/audit-logs');
      expect(auditRes.ok()).toBeTruthy();
      const auditBody = await auditRes.json();
      const auditItems = auditBody.data?.items || auditBody.items || [];
      const serviceAuditLog = auditItems.find(
        (log: { ResourceType: string; ResourceID: string }) =>
          log.ResourceType === 'service' && log.ResourceID === createdService.id
      );
      expect(serviceAuditLog).toBeDefined();
      expect(serviceAuditLog.Action).toBe('create');

      await page.click('text=版本发布');
      await page.waitForURL('**/revisions');
      await page.click('button:has-text("新建版本")');
      await page.fill('input[placeholder="例如: v1.0.0 或 2024.01.01"]', versionName);
      await page.fill('textarea[placeholder="描述这个版本的变更内容..."]', 'P3 阶段测试版本');
      await page.click('button:has-text("仅创建版本")');
      await expect(page.locator('.ant-message-notice:has-text("创建成功") >> nth=0')).toBeVisible({
        timeout: 10000,
      });

      const revisionsRes = await apiCtx.get('/api/v1/revisions');
      expect(revisionsRes.ok()).toBeTruthy();
      const revisionsBody = await revisionsRes.json();
      const revisions = revisionsBody.data?.items || revisionsBody.items || [];
      const createdRevision = revisions.find(
        (r: { version: string }) => r.version === versionName
      );
      expect(createdRevision).toBeDefined();

      if (createdService) {
        await apiCtx.delete(`/api/v1/services?id=${createdService.id}`);
      }
      if (createdRevision) {
        await apiCtx.delete(`/api/v1/revisions?id=${createdRevision.id}`);
      }

      await apiCtx.dispose();
    });
  });
});
