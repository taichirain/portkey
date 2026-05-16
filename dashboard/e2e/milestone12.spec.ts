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

function generateId() {
  return Date.now();
}

test.describe('Milestone 11-12: Traffic Policy (灰度发布)', () => {
  let token: string;
  const testId = generateId();

  test.beforeAll(async () => {
    token = await getAdminToken();
  });

  test('Traffic Policy API: Header 匹配策略完整流程', async () => {
    const ctx = await request.newContext({
      baseURL: API_BASE,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` },
    });

    // 1. 创建两个服务：stable 和 canary（必须不同，因为 target_service_id 不能等于 route 的 service_id）
    const stableSvcRes = await ctx.post('/api/v1/services', {
      data: {
        name: `stable-svc-${testId}`,
        host: 'localhost',
        port: 8080,
        path: '/api',
      },
    });
    expect(stableSvcRes.ok()).toBe(true);
    const stableSvc = await stableSvcRes.json();
    const stableSvcId = stableSvc.data?.id || stableSvc.id;

    const canarySvcRes = await ctx.post('/api/v1/services', {
      data: {
        name: `canary-svc-${testId}`,
        host: 'localhost',
        port: 8081,
        path: '/api',
      },
    });
    expect(canarySvcRes.ok()).toBe(true);
    const canarySvc = await canarySvcRes.json();
    const canarySvcId = canarySvc.data?.id || canarySvc.id;

    // 2. 创建路由，绑定到 stable 服务
    const routeRes = await ctx.post('/api/v1/routes', {
      data: {
        name: `test-route-${testId}`,
        service_id: stableSvcId,
        methods: ['GET', 'POST'],
        paths: [`/api-${testId}`],
      },
    });
    expect(routeRes.ok()).toBe(true);
    const route = await routeRes.json();
    const routeId = route.data?.id || route.id;

    // 3. 创建 Traffic Policy：Header 匹配 (X-Canary: true) 路由到 canary
    const policyRes = await ctx.post('/api/v1/traffic-policies', {
      data: {
        name: `header-policy-${testId}`,
        route_id: routeId,
        priority: 1,
        type: 'header',
        match_config: {
          header: 'X-Canary',
          value: 'true',
          operator: 'exact',
        },
        target_service_id: canarySvcId,
        enabled: true,
        tags: ['canary', 'header'],
      },
    });
    expect(policyRes.ok()).toBe(true);
    const policy = await policyRes.json();
    const policyId = policy.data?.id || policy.id;

    // 4. 验证创建的 policy - GetByID API 返回 {success: true, data: policy}
    const getRes = await ctx.get(`/api/v1/traffic-policies?id=${policyId}`);
    expect(getRes.ok()).toBe(true);
    const getBody = await getRes.json();
    const getPolicy = getBody.data;
    expect(getPolicy.name).toBe(`header-policy-${testId}`);
    expect(getPolicy.type).toBe('header');

    // 5. 按 route_id 列出 policies - ListByRouteID 返回 {success: true, data: [policy1, policy2, ...]}
    const listRes = await ctx.get(`/api/v1/traffic-policies?route_id=${routeId}`);
    expect(listRes.ok()).toBe(true);
    const listBody = await listRes.json();
    const policies = listBody.data;
    expect(Array.isArray(policies)).toBe(true);
    expect(policies.length).toBeGreaterThan(0);

    // 6. 更新 policy：修改 priority
    const updateRes = await ctx.put(`/api/v1/traffic-policies?id=${policyId}`, {
      data: {
        priority: 10,
      },
    });
    expect(updateRes.ok()).toBe(true);

    // 7. 验证更新
    const getRes2 = await ctx.get(`/api/v1/traffic-policies?id=${policyId}`);
    expect(getRes2.ok()).toBe(true);
    const getBody2 = await getRes2.json();
    const getPolicy2 = getBody2.data;
    expect(getPolicy2.priority).toBe(10);

    // 8. 验证配置（使用正确的 API 路径：/api/v1/revisions/validate）
    const validateRes = await ctx.post('/api/v1/revisions/validate');
    expect(validateRes.ok()).toBe(true);
    const validateBody = await validateRes.json();
    expect(validateBody.data?.valid).toBe(true);

    // 9. 禁用 policy
    const disableRes = await ctx.put(`/api/v1/traffic-policies?id=${policyId}`, {
      data: {
        enabled: false,
      },
    });
    expect(disableRes.ok()).toBe(true);

    // 10. 删除 policy
    const deleteRes = await ctx.delete(`/api/v1/traffic-policies?id=${policyId}`);
    expect(deleteRes.ok()).toBe(true);

    // 清理
    await ctx.delete(`/api/v1/routes?id=${routeId}`).catch(() => {});
    await ctx.delete(`/api/v1/services?id=${canarySvcId}`).catch(() => {});
    await ctx.delete(`/api/v1/services?id=${stableSvcId}`).catch(() => {});
    await ctx.dispose();
  });

  test('Traffic Policy API: Weight 权重策略（按比例切流）', async () => {
    const ctx = await request.newContext({
      baseURL: API_BASE,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` },
    });

    // 1. 创建两个服务（必须不同）
    const stableSvcRes = await ctx.post('/api/v1/services', {
      data: {
        name: `weight-stable-${testId}`,
        host: 'localhost',
        port: 8080,
        path: '/',
      },
    });
    expect(stableSvcRes.ok()).toBe(true);
    const stableSvc = await stableSvcRes.json();
    const stableSvcId = stableSvc.data?.id || stableSvc.id;

    const canarySvcRes = await ctx.post('/api/v1/services', {
      data: {
        name: `weight-canary-${testId}`,
        host: 'localhost',
        port: 8081,
        path: '/',
      },
    });
    expect(canarySvcRes.ok()).toBe(true);
    const canarySvc = await canarySvcRes.json();
    const canarySvcId = canarySvc.data?.id || canarySvc.id;

    // 2. 创建路由
    const routeRes = await ctx.post('/api/v1/routes', {
      data: {
        name: `weight-route-${testId}`,
        service_id: stableSvcId,
        methods: ['GET'],
        paths: [`/weight-${testId}`],
      },
    });
    expect(routeRes.ok()).toBe(true);
    const route = await routeRes.json();
    const routeId = route.data?.id || route.id;

    // 3. 创建 Weight 策略：20% 流量切到 canary
    const policyRes = await ctx.post('/api/v1/traffic-policies', {
      data: {
        name: `weight-policy-${testId}`,
        route_id: routeId,
        priority: 1,
        type: 'weight',
        match_config: {
          percentage: 20,
        },
        target_service_id: canarySvcId,
        enabled: true,
        tags: ['canary', 'weight'],
      },
    });
    expect(policyRes.ok()).toBe(true);
    const policy = await policyRes.json();
    const policyId = policy.data?.id || policy.id;

    // 4. 验证 policy 的 type
    const getRes = await ctx.get(`/api/v1/traffic-policies?id=${policyId}`);
    expect(getRes.ok()).toBe(true);
    const getBody = await getRes.json();
    const getPolicy = getBody.data;
    expect(getPolicy.type).toBe('weight');

    // 5. 创建另一个 Weight 策略：10% 流量切到另一个服务（测试 priority）
    const anotherSvcRes = await ctx.post('/api/v1/services', {
      data: {
        name: `weight-another-${testId}`,
        host: 'localhost',
        port: 8082,
        path: '/',
      },
    });
    expect(anotherSvcRes.ok()).toBe(true);
    const anotherSvc = await anotherSvcRes.json();
    const anotherSvcId = anotherSvc.data?.id || anotherSvc.id;

    const policy2Res = await ctx.post('/api/v1/traffic-policies', {
      data: {
        name: `weight-policy-2-${testId}`,
        route_id: routeId,
        priority: 2,
        type: 'weight',
        match_config: {
          percentage: 10,
        },
        target_service_id: anotherSvcId,
        enabled: true,
        tags: ['canary'],
      },
    });
    expect(policy2Res.ok()).toBe(true);
    const policy2 = await policy2Res.json();
    const policy2Id = policy2.data?.id || policy2.id;

    // 6. 验证配置
    const validateRes = await ctx.post('/api/v1/revisions/validate');
    expect(validateRes.ok()).toBe(true);
    const validateBody = await validateRes.json();
    expect(validateBody.data?.valid).toBe(true);

    // 清理
    await ctx.delete(`/api/v1/traffic-policies?id=${policy2Id}`).catch(() => {});
    await ctx.delete(`/api/v1/traffic-policies?id=${policyId}`).catch(() => {});
    await ctx.delete(`/api/v1/routes?id=${routeId}`).catch(() => {});
    await ctx.delete(`/api/v1/services?id=${anotherSvcId}`).catch(() => {});
    await ctx.delete(`/api/v1/services?id=${canarySvcId}`).catch(() => {});
    await ctx.delete(`/api/v1/services?id=${stableSvcId}`).catch(() => {});
    await ctx.dispose();
  });

  test('Traffic Policy API: Query 参数匹配策略', async () => {
    const ctx = await request.newContext({
      baseURL: API_BASE,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` },
    });

    // 1. 创建服务（必须不同）
    const stableSvcRes = await ctx.post('/api/v1/services', {
      data: {
        name: `query-stable-${testId}`,
        host: 'localhost',
        port: 8080,
        path: '/',
      },
    });
    expect(stableSvcRes.ok()).toBe(true);
    const stableSvc = await stableSvcRes.json();
    const stableSvcId = stableSvc.data?.id || stableSvc.id;

    const betaSvcRes = await ctx.post('/api/v1/services', {
      data: {
        name: `query-beta-${testId}`,
        host: 'localhost',
        port: 8081,
        path: '/',
      },
    });
    expect(betaSvcRes.ok()).toBe(true);
    const betaSvc = await betaSvcRes.json();
    const betaSvcId = betaSvc.data?.id || betaSvc.id;

    // 2. 创建路由
    const routeRes = await ctx.post('/api/v1/routes', {
      data: {
        name: `query-route-${testId}`,
        service_id: stableSvcId,
        methods: ['GET'],
        paths: [`/query-${testId}`],
      },
    });
    expect(routeRes.ok()).toBe(true);
    const route = await routeRes.json();
    const routeId = route.data?.id || route.id;

    // 3. 创建 Query 策略：?env=beta 路由到 beta 服务
    const policyRes = await ctx.post('/api/v1/traffic-policies', {
      data: {
        name: `query-policy-${testId}`,
        route_id: routeId,
        priority: 1,
        type: 'query',
        match_config: {
          key: 'env',
          value: 'beta',
          operator: 'exact',
        },
        target_service_id: betaSvcId,
        enabled: true,
        tags: ['beta'],
      },
    });
    expect(policyRes.ok()).toBe(true);
    const policy = await policyRes.json();
    const policyId = policy.data?.id || policy.id;

    // 4. 验证配置
    const validateRes = await ctx.post('/api/v1/revisions/validate');
    expect(validateRes.ok()).toBe(true);
    const validateBody = await validateRes.json();
    expect(validateBody.data?.valid).toBe(true);

    // 清理
    await ctx.delete(`/api/v1/traffic-policies?id=${policyId}`).catch(() => {});
    await ctx.delete(`/api/v1/routes?id=${routeId}`).catch(() => {});
    await ctx.delete(`/api/v1/services?id=${betaSvcId}`).catch(() => {});
    await ctx.delete(`/api/v1/services?id=${stableSvcId}`).catch(() => {});
    await ctx.dispose();
  });

  test('Traffic Policy API: 非法配置验证', async () => {
    const ctx = await request.newContext({
      baseURL: API_BASE,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` },
    });

    // 1. 创建依赖资源
    const svcRes = await ctx.post('/api/v1/services', {
      data: {
        name: `validate-svc-${testId}`,
        host: 'localhost',
        port: 8080,
        path: '/',
      },
    });
    expect(svcRes.ok()).toBe(true);
    const svc = await svcRes.json();
    const svcId = svc.data?.id || svc.id;

    const routeRes = await ctx.post('/api/v1/routes', {
      data: {
        name: `validate-route-${testId}`,
        service_id: svcId,
        methods: ['GET'],
        paths: [`/validate-${testId}`],
      },
    });
    expect(routeRes.ok()).toBe(true);
    const route = await routeRes.json();
    const routeId = route.data?.id || route.id;

    // 2. 测试缺失 match_config
    const badPolicy1 = await ctx.post('/api/v1/traffic-policies', {
      data: {
        name: `bad-policy-1-${testId}`,
        route_id: routeId,
        priority: 1,
        type: 'header',
        match_config: {},
        target_service_id: svcId,
      },
    });
    expect(badPolicy1.ok()).toBe(false);
    expect(badPolicy1.status()).toBe(400);

    // 3. 测试 weight percentage 超出范围
    const badPolicy2 = await ctx.post('/api/v1/traffic-policies', {
      data: {
        name: `bad-policy-2-${testId}`,
        route_id: routeId,
        priority: 1,
        type: 'weight',
        match_config: {
          percentage: 150,
        },
        target_service_id: svcId,
      },
    });
    expect(badPolicy2.ok()).toBe(false);
    expect(badPolicy2.status()).toBe(400);

    // 4. 测试 header 类型缺少 header 字段
    const badPolicy3 = await ctx.post('/api/v1/traffic-policies', {
      data: {
        name: `bad-policy-3-${testId}`,
        route_id: routeId,
        priority: 1,
        type: 'header',
        match_config: {
          value: 'test',
        },
        target_service_id: svcId,
      },
    });
    expect(badPolicy3.ok()).toBe(false);
    expect(badPolicy3.status()).toBe(400);

    // 5. 测试无效的 type
    const badPolicy4 = await ctx.post('/api/v1/traffic-policies', {
      data: {
        name: `bad-policy-4-${testId}`,
        route_id: routeId,
        priority: 1,
        type: 'invalid_type',
        match_config: {
          header: 'X-Test',
          value: 'test',
        },
        target_service_id: svcId,
      },
    });
    expect(badPolicy4.ok()).toBe(false);
    expect(badPolicy4.status()).toBe(400);

    // 清理
    await ctx.delete(`/api/v1/routes?id=${routeId}`).catch(() => {});
    await ctx.delete(`/api/v1/services?id=${svcId}`).catch(() => {});
    await ctx.dispose();
  });
});
