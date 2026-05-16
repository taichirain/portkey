import { test, expect, request } from '@playwright/test';

const API_BASE = 'http://127.0.0.1:8002';

async function getAdminToken() {
  const ctx = await request.newContext({ baseURL: API_BASE });
  const res = await ctx.post('/api/v1/login', {
    data: { username: 'admin', password: 'admin123' },
  });
  const body = await res.json();
  await ctx.dispose();
  return body.data?.token || body.token || '';
}

async function cleanupTestData(token: string) {
  const ctx = await request.newContext({
    baseURL: API_BASE,
    extraHTTPHeaders: { Authorization: `Bearer ${token}` },
  });

  // 清理 revisions（先清理，避免外键约束问题）
  const revRes = await ctx.get('/api/v1/revisions');
  const revBody = await revRes.json();
  for (const r of revBody.data?.items || []) {
    if (r.version?.includes('e2e')) {
      await ctx.delete(`/api/v1/revisions?id=${r.id}`).catch(() => {});
    }
  }

  // 清理 routes
  const routesRes = await ctx.get('/api/v1/routes');
  const routesBody = await routesRes.json();
  for (const r of routesBody.data?.items || []) {
    if (r.name?.includes('e2e')) {
      await ctx.delete(`/api/v1/routes?id=${r.id}`).catch(() => {});
    }
  }

  // 清理 services
  const svcRes = await ctx.get('/api/v1/services');
  const svcBody = await svcRes.json();
  for (const s of svcBody.data?.items || []) {
    if (s.name?.includes('e2e')) {
      await ctx.delete(`/api/v1/services?id=${s.id}`).catch(() => {});
    }
  }

  // 清理 targets
  const tgtRes = await ctx.get('/api/v1/targets');
  const tgtBody = await tgtRes.json();
  for (const t of tgtBody.data?.items || []) {
    if (t.target?.includes('127.0.0.1') && t.port === 8080) {
      await ctx.delete(`/api/v1/targets?id=${t.id}`).catch(() => {});
    }
  }

  // 清理 upstreams（最后清理，因为 target/service 可能依赖它）
  const upRes = await ctx.get('/api/v1/upstreams');
  const upBody = await upRes.json();
  for (const u of upBody.data?.items || []) {
    if (u.name?.includes('e2e')) {
      await ctx.delete(`/api/v1/upstreams?id=${u.id}`).catch(() => {});
    }
  }

  // 清理 consumers
  const consRes = await ctx.get('/api/v1/consumers');
  const consBody = await consRes.json();
  for (const c of consBody.data?.items || []) {
    if (c.username?.includes('e2e') || c.custom_id?.includes('e2e')) {
      await ctx.delete(`/api/v1/consumers?id=${c.id}`).catch(() => {});
    }
  }

  // 清理 plugins
  const plugRes = await ctx.get('/api/v1/plugins');
  const plugBody = await plugRes.json();
  for (const p of plugBody.data?.items || []) {
    if (p.name?.includes('e2e')) {
      await ctx.delete(`/api/v1/plugins?id=${p.id}`).catch(() => {});
    }
  }

  await ctx.dispose();
}

test.describe('Milestone 9: Dashboard 最小可用版', () => {
  test.beforeAll(async () => {
    const token = await getAdminToken();
    await cleanupTestData(token);
  });

  test.beforeEach(async ({ page }) => {
    await page.goto('/login');
    await page.waitForSelector('text=Portkey');
  });

  test('完整配置创建与发布主链路', async ({ page }) => {
    // 1. 登录
    await page.fill('input[placeholder="用户名"]', 'admin');
    await page.fill('input[placeholder="密码"]', 'admin123');
    await page.click('button[type="submit"]');
    await page.waitForURL('/');
    await expect(page.locator('main .ant-card').filter({ hasText: '监控概览' })).toBeVisible();

    // 2. 创建上游 (Upstream)
    await page.click('text=上游管理');
    await page.waitForURL('**/upstreams');
    await page.click('button:has-text("新建上游")');
    const timestamp = Date.now();
    const upstreamName = `test-upstream-e2e-${timestamp}`;
    const serviceName = `test-service-e2e-${timestamp}`;
    const routeName = `test-route-e2e-${timestamp}`;
    const versionName = `v1.0.0-e2e-${timestamp}`;
    await page.fill('input[placeholder="请输入上游名称"]', upstreamName);
    await page.click('.ant-modal-wrap:not(.ant-modal-hidden) .ant-modal-footer .ant-btn-primary');
    await expect(page.locator('.ant-message-notice:has-text("创建成功") >> nth=0')).toBeVisible({ timeout: 10000 });

    // 3. 创建目标 (Target)
    await page.click('text=目标管理');
    await page.waitForURL('**/targets');
    await page.click('button:has-text("新建目标")');
    // 选择上游
    await page.click('.ant-modal-body .ant-select:has(.ant-select-arrow) >> nth=0');
    await page.click(`.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item:has-text("${upstreamName}")`);
    await page.fill('input[placeholder="例如: 192.168.1.100 或 backend.example.com"]', '127.0.0.1');
    // Port InputNumber has no placeholder, default is 80
    await page.fill('.ant-form-item-label:has-text("端口") + .ant-form-item-control input', '8080');
    await page.click('.ant-modal-wrap:not(.ant-modal-hidden) .ant-modal-footer .ant-btn-primary');
    await expect(page.locator('.ant-message-notice:has-text("创建成功") >> nth=0')).toBeVisible({ timeout: 10000 });

    // 4. 创建服务 (Service)
    await page.click('text=服务管理');
    await page.waitForURL('**/services');
    await page.click('button:has-text("新建服务")');
    await page.fill('input[placeholder="请输入服务名称"]', serviceName);
    await page.fill('input[placeholder="例如: example.com"]', 'localhost');
    await page.fill('input[placeholder="例如: /api"]', '/test');
    await page.click('.ant-modal-wrap:not(.ant-modal-hidden) .ant-modal-footer .ant-btn-primary');
    await expect(page.locator('.ant-message-notice:has-text("创建成功") >> nth=0')).toBeVisible({ timeout: 10000 });

    // 5. 创建路由 (Route)
    await page.click('text=路由管理');
    await page.waitForURL('**/routes');
    await page.click('button:has-text("新建路由")');
    await page.fill('input[placeholder="请输入路由名称"]', routeName);
    // 选择服务
    await page.click('.ant-modal-body .ant-select:has(.ant-select-arrow) >> nth=0');
    await page.click(`.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item:has-text("${serviceName}")`);
    // 添加路径
    await page.click('.ant-modal-body .ant-select:has-text("输入路径后回车添加")');
    await page.fill('.ant-select-selection-search-input >> nth=-1', '/test-e2e');
    await page.keyboard.press('Enter');
    await page.click('.ant-modal-wrap:not(.ant-modal-hidden) .ant-modal-footer .ant-btn-primary');
    await expect(page.locator('.ant-message-notice:has-text("创建成功") >> nth=0')).toBeVisible({ timeout: 10000 });

    // 6. 进入版本发布页面
    await page.click('text=版本发布');
    await page.waitForURL('**/revisions');

    // 7. 验证配置
    await page.click('button:has-text("验证配置")');
    await expect(page.locator('.ant-message-notice:has-text("配置验证通过")')).toBeVisible({ timeout: 10000 });

    // 8. 创建快照
    await page.click('button:has-text("创建快照")');
    await expect(page.locator('.ant-message-notice:has-text("快照创建成功")')).toBeVisible({ timeout: 10000 });

    // 9. 创建并发布版本
    await page.click('button:has-text("新建版本")');
    await page.fill('input[placeholder="例如: v1.0.0 或 2024.01.01"]', versionName);
    await page.fill('textarea[placeholder="描述这个版本的变更内容..."]', 'E2E 测试自动创建的版本');
    await page.click('button:has-text("创建并发布")');
    await expect(page.locator('.ant-message-notice:has-text("版本创建并发布成功")')).toBeVisible({ timeout: 10000 });

    // 10. 检查活跃版本显示
    await expect(page.locator('text=当前活跃版本')).toBeVisible();
    await expect(page.locator(`text=${versionName}`).first()).toBeVisible();

    // 11. 返回监控概览，确认显示 active revision
    await page.click('text=监控概览');
    await page.waitForURL('/');
    await expect(page.locator('text=当前配置版本')).toBeVisible();
    await expect(page.locator(`text=${versionName}`).first()).toBeVisible();
  });

  test('登录页错误密码不显示提示 — Bug 验证', async ({ page }) => {
    await expect(page.locator('text=Portkey')).toBeVisible();
    await expect(page.locator('text=API Gateway 管理后台')).toBeVisible();
    await expect(page.locator('input[placeholder="用户名"]')).toHaveValue('admin');
    await expect(page.locator('input[placeholder="密码"]')).toHaveValue('admin123');

    // 输入错误密码并提交
    await page.fill('input[placeholder="用户名"]', 'admin');
    await page.fill('input[placeholder="密码"]', 'wrongpassword');
    await page.click('button[type="submit"]');

    // Bug: api.ts 的 401 interceptor 会执行 window.location.href = '/login'
    // 导致页面刷新，Login.tsx 中的 message.error('用户名或密码错误') 无法显示
    await page.waitForTimeout(1000);
    const errorMessage = page.locator('.ant-message-notice:has-text("用户名或密码错误")');
    await expect(errorMessage).not.toBeVisible();

    // 页面应停留在登录页
    await expect(page.locator('text=Portkey')).toBeVisible();
    await expect(page).toHaveURL(/login/);
  });

  test('各管理页面可访问', async ({ page }) => {
    // 先登录
    await page.fill('input[placeholder="用户名"]', 'admin');
    await page.fill('input[placeholder="密码"]', 'admin123');
    await page.click('button[type="submit"]');
    await page.waitForURL('/');

    const pages = [
      { name: '路由管理', path: '/routes' },
      { name: '服务管理', path: '/services' },
      { name: '上游管理', path: '/upstreams' },
      { name: '目标管理', path: '/targets' },
      { name: '消费者管理', path: '/consumers' },
      { name: '插件管理', path: '/plugins' },
      { name: '版本发布', path: '/revisions' },
    ];

    for (const p of pages) {
      await page.click(`text=${p.name}`);
      await page.waitForURL(`**${p.path}`);
      await expect(page.locator(`text=${p.name}`).first()).toBeVisible();
    }
  });
});
