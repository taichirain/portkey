import { test, expect, request } from '@playwright/test';

const API_BASE = 'http://127.0.0.1:8002';
const DP_BASE = 'http://127.0.0.1:8081';

test('Dashboard 创建灰度策略、发布并回滚到旧版本', async ({ page }) => {
  test.setTimeout(90_000);
  const api = await request.newContext({ baseURL: API_BASE });
  const login = await api.post('/api/v1/login', {
    data: { username: 'admin', password: 'admin123' },
  });
  expect(login.ok()).toBe(true);
  const loginBody = await login.json();
  const token = loginBody.data?.token || loginBody.token;
  const admin = await request.newContext({
    baseURL: API_BASE,
    extraHTTPHeaders: { Authorization: `Bearer ${token}` },
  });

  const suffix = Date.now().toString();
  const stableName = `m16-stable-${suffix}`;
  const canaryName = `m16-canary-${suffix}`;
  const routeName = `m16-route-${suffix}`;
  const policyName = `m16-policy-${suffix}`;
  const baseVersion = `m16-base-${suffix}`;
  const canaryVersion = `m16-canary-${suffix}`;

  const createService = async (name: string, port: number) => {
    const response = await admin.post('/api/v1/services', {
      data: { name, host: '127.0.0.1', port, protocol: 'http' },
    });
    expect(response.ok()).toBe(true);
    const body = await response.json();
    return body.data?.id || body.id;
  };

  const stableID = await createService(stableName, 18080);
  const canaryID = await createService(canaryName, 18081);
  const routeResponse = await admin.post('/api/v1/routes', {
    data: {
      name: routeName,
      service_id: stableID,
      methods: ['GET'],
      paths: [`/m16-${suffix}`],
    },
  });
  expect(routeResponse.ok()).toBe(true);

  await page.goto('/login');
  await page.getByPlaceholder('用户名').fill('admin');
  await page.getByPlaceholder('密码').fill('admin123');
  await page.getByRole('button', { name: '登 录' }).click();
  await page.waitForURL('/');

  const publishVersion = async (version: string) => {
    await page.getByText('版本发布', { exact: true }).click();
    await page.waitForURL('**/revisions');
    await page.getByRole('button', { name: /新建版本/ }).click();
    await page.getByPlaceholder('例如: v1.0.0 或 2024.01.01').fill(version);
    await page.getByRole('button', { name: /创建并发布/ }).click();
    await expect(page.getByText('版本创建并发布成功').first()).toBeVisible();
    await expect(page.getByText(version).first()).toBeVisible();
    const active = await admin.get('/api/v1/revisions/active');
    expect(active.ok()).toBe(true);
    const body = await active.json();
    return body.data?.id || body.id;
  };

  const baseRevisionID = await publishVersion(baseVersion);

  await page.getByText('流量策略', { exact: true }).click();
  await page.waitForURL('**/traffic-policies');
  await page.getByRole('button', { name: /新建策略/ }).click();
  const modal = page.getByRole('dialog', { name: '新建流量策略' });
  await modal.getByPlaceholder('请输入策略名称').fill(policyName);
  await modal.locator('.ant-form-item:has(.ant-form-item-label:has-text("绑定路由")) .ant-select').click();
  await page.locator('.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item').filter({ hasText: routeName }).click();
  await modal.locator('.ant-form-item:has(.ant-form-item-label:has-text("目标服务")) .ant-select').click();
  await page.locator('.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item').filter({ hasText: canaryName }).click();
  await modal.getByPlaceholder('例如: X-Canary').fill('X-Canary');
  await modal.getByPlaceholder('例如: beta').fill('beta');
  await modal.locator('.ant-modal-footer .ant-btn-primary').click();
  await expect(page.locator('.ant-message-notice').filter({ hasText: '创建成功' }).first()).toBeVisible();
  await expect(page.getByText(policyName).first()).toBeVisible();

  const canaryRevisionID = await publishVersion(canaryVersion);
  expect(canaryRevisionID).not.toBe(baseRevisionID);

  const dp = await request.newContext({ baseURL: DP_BASE });
  await expect.poll(async () => {
    const response = await dp.get('/revision');
    const body = await response.json();
    return body.current_revision_id;
  }, { timeout: 30000 }).toBe(canaryRevisionID);

  await page.getByText('运行监控', { exact: true }).click();
  await page.waitForURL('**/monitoring');
  await expect(page.getByText('local-dp').first()).toBeVisible();
  await expect(page.getByText(canaryRevisionID.slice(0, 8)).first()).toBeVisible();

  await page.getByText('版本发布', { exact: true }).click();
  const baseRow = page.locator('tr').filter({ hasText: baseVersion });
  await baseRow.getByRole('button', { name: /回滚/ }).click();
  await page.locator('.ant-popconfirm .ant-btn-primary').click();
  await expect(page.getByText('回滚成功').first()).toBeVisible();
  await expect.poll(async () => {
    const response = await dp.get('/revision');
    const body = await response.json();
    return body.current_revision_id;
  }, { timeout: 30000 }).toBe(baseRevisionID);

  await dp.dispose();
  await admin.dispose();
  await api.dispose();
});
