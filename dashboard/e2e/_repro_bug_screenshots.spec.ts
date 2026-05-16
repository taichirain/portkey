import { test } from '@playwright/test';

test('截屏复现登录 Bug', async ({ page }) => {
  await page.goto('http://localhost:5173/login');
  await page.waitForTimeout(500);
  await page.screenshot({ path: '/tmp/bug_step1_login.png', fullPage: true });

  await page.fill('input[placeholder="用户名"]', 'admin');
  await page.fill('input[placeholder="密码"]', 'wrongpassword');
  await page.click('button[type="submit"]');

  // 等待 interceptor 刷新完成
  await page.waitForTimeout(1500);
  await page.screenshot({ path: '/tmp/bug_step2_after_wrong_password.png', fullPage: true });
});
