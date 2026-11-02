import type { Page } from '@playwright/test';

import { expect, test } from '../../../../../../hack/tests/fixtures/auth';
import { adminStorageStatePath } from '../../../../../../hack/tests/fixtures/auth-state';
import { config, workspacePath } from '../../../../../../hack/tests/fixtures/config';

import {
  mintEmpToken,
  newAdminApi,
  pickActiveEmployees,
  readEmpJwtSecret,
  resolveTenantId,
} from '../support/eval360';

// TC-3 员工端飞书免登：卡片链接 → 飞书授权 → 回调换票 → 回到原目标页。
//
// 飞书授权页与 code 换票是外部依赖：用 page.route 拦截 open.feishu.cn（只记录授权 URL）
// 并替身 POST /x/linapro-employee-core/api/v1/emp/login（返回用真实员工签发的 JWT），其余员工端接口走真实后端。
// 断言落在业务结果：授权 URL 携带卡片链接的企业应用 id、回调地址不带参数、state 保留
// 原始目标路径（不得双重编码）；回调后用 state 中的应用 id 换票并回到原目标页；
// 换票失败时展示登录失败且地址栏不残留一次性 code。

const cardLarkAppId = 'cli_e2e_card';
// 员工换票接口的真实挂载路径（employee-core 插件路由前缀）。须精确到插件前缀：
// 宽泛的 **/api/v1/emp/login 会把错误路径的请求也替身掉，掩盖 404。
const empLoginRoute = '**/x/linapro-employee-core/api/v1/emp/login';
const cardTarget = `/eval-360/emp/todo?larkAppId=${cardLarkAppId}`;

interface AuthorizeCapture {
  appId: string;
  redirectUri: string;
  state: { a: string; k: string; l: string; r: string };
}

// 拦截飞书授权页：记录授权请求并返回一个空白页（不访问外网）。
async function stubFeishuAuthorize(page: Page) {
  await page.route('https://open.feishu.cn/**', (route) =>
    route.fulfill({ body: '<html><body>feishu</body></html>', contentType: 'text/html' }),
  );
}

// 无员工会话打开卡片链接，等页面整页跳到（被拦截的）飞书授权页后返回授权 URL 解析结果。
async function openCardLinkWithoutSession(page: Page): Promise<AuthorizeCapture> {
  await page.goto(workspacePath(cardTarget), { waitUntil: 'domcontentloaded' });
  await page.waitForURL(/^https:\/\/open\.feishu\.cn\/open-apis\/authen\/v1\/index/);
  const url = new URL(page.url());
  return {
    appId: url.searchParams.get('app_id') ?? '',
    redirectUri: url.searchParams.get('redirect_uri') ?? '',
    state: JSON.parse(url.searchParams.get('state') ?? '{}'),
  };
}

// 按飞书回跳方式把 code/state 追加到回调地址（hash 路由时落入 hash 路由 query）。
function buildCallbackURL(capture: AuthorizeCapture, code: string): string {
  const params = new URLSearchParams({ code, state: JSON.stringify(capture.state) });
  const sep = capture.redirectUri.includes('?') ? '&' : '?';
  return `${capture.redirectUri}${sep}${params.toString()}`;
}

test.describe('TC-3 员工端飞书免登（卡片链接 → 授权 → 换票 → 回到目标页）', () => {
  test('TC-3a: 无会话打开卡片链接，跳转飞书授权并携带卡片的企业应用 id 与原始目标路径', async ({
    browser,
  }) => {
    const context = await browser.newContext({ baseURL: config.baseURL });
    const page = await context.newPage();
    try {
      await stubFeishuAuthorize(page);
      const capture = await openCardLinkWithoutSession(page);

      // 授权用卡片链接指定的飞书企业应用。
      expect(capture.appId).toBe(cardLarkAppId);
      // 回调地址固定为登录页且不带任何参数（便于飞书重定向白名单精确匹配）。
      const redirect = new URL(capture.redirectUri);
      expect(redirect.search).toBe('');
      expect(`${redirect.pathname}${redirect.hash}`).toMatch(/eval-360\/emp\/login$/);
      // state 往返企业应用 id、登录页与原始目标路径（原样、未被双重编码）。
      expect(capture.state.a).toBe(cardLarkAppId);
      expect(capture.state.l).toBe('/eval-360/emp/login');
      expect(capture.state.r).toBe(cardTarget);
      // 卡片链接的企业应用 id 已持久化到系统源 localStorage，供后续 401 重登选用同一应用。
      const appIdInStore = await context.storageState().then((state) =>
        state.origins
          .flatMap((origin) => origin.localStorage)
          .find((entry) => entry.name === 'linapro:emp-lark-app-id')?.value,
      );
      expect(appIdInStore).toBe(cardLarkAppId);
    } finally {
      await context.close();
    }
  });

  test('TC-3b: 飞书回调后用 state 中的企业应用 id 换票，回到卡片原目标页且地址栏不残留 code', async ({
    browser,
  }) => {
    const api = await newAdminApi(adminStorageStatePath);
    const context = await browser.newContext({ baseURL: config.baseURL });
    const page = await context.newPage();
    try {
      const secret = readEmpJwtSecret();
      expect(secret, '缺少员工 JWT 密钥（plugin.linapro-employee-core.emp_jwt_secret）').not.toBe('');
      const [employee] = await pickActiveEmployees(api, 1);
      const tenantId = resolveTenantId(adminStorageStatePath);
      const token = mintEmpToken(secret, employee!.empId, tenantId);

      await stubFeishuAuthorize(page);
      const capture = await openCardLinkWithoutSession(page);

      // 替身飞书换票：记录请求体，返回真实员工的 JWT。
      let loginBody: { code?: string; larkAppId?: string } = {};
      await page.route(empLoginRoute, async (route) => {
        loginBody = route.request().postDataJSON();
        await route.fulfill({
          contentType: 'application/json',
          json: {
            code: 0,
            data: {
              empId: employee!.empId,
              mokaEmployeeNo: employee!.employeeNo,
              name: employee!.name,
              openId: 'ou_e2e',
              tenantId,
              token,
            },
            message: 'ok',
          },
        });
      });

      await page.goto(buildCallbackURL(capture, 'e2e-code'), { waitUntil: 'domcontentloaded' });

      // 回到卡片原目标页：员工待办页真实渲染（换票得到的 JWT 被员工端接口接受）。
      await expect(page.getByTestId('eval360-emp-todo')).toBeVisible();
      expect(loginBody).toEqual({ code: 'e2e-code', larkAppId: cardLarkAppId });
      expect(page.url()).toContain('/eval-360/emp/todo');
      expect(page.url()).not.toContain('code=');
      expect(
        await page.evaluate(() => localStorage.getItem('linapro:emp-auth-token')),
      ).toBe(token);
    } finally {
      await context.close();
      await api.dispose();
    }
  });

  test('TC-3c: 换票失败时展示登录失败与重试，地址栏不残留一次性 code', async ({ browser }) => {
    const context = await browser.newContext({ baseURL: config.baseURL });
    const page = await context.newPage();
    try {
      await stubFeishuAuthorize(page);
      const capture = await openCardLinkWithoutSession(page);

      await page.route(empLoginRoute, (route) =>
        route.fulfill({
          contentType: 'application/json',
          json: { code: 1, data: null, message: 'E2E 登录码无效' },
        }),
      );
      await page.goto(buildCallbackURL(capture, 'e2e-bad-code'), {
        waitUntil: 'domcontentloaded',
      });

      await expect(page.getByText('登录失败')).toBeVisible();
      // 后端错误文案原样展示；重试按钮渲染译文（Ant Design 会在两字按钮间插入空格）。
      await expect(page.locator('.ant-result-subtitle')).toHaveText('E2E 登录码无效');
      await expect(page.getByTestId('eval360-emp-login-retry')).toHaveText(/^重\s?试$/);
      await expect.poll(() => page.url()).not.toContain('code=');
    } finally {
      await context.close();
    }
  });
});
