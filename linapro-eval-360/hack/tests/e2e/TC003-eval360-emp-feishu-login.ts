import type { Browser, BrowserContext, Page } from '@host-tests/support/playwright';

import { expect, test } from '../../../../../../hack/tests/fixtures/auth';
import { adminStorageStatePath } from '../../../../../../hack/tests/fixtures/auth-state';
import { config, workspacePath } from '../../../../../../hack/tests/fixtures/config';

import {
  mintEmpToken,
  newAdminApi,
  pickActiveEmployees,
  readEmpJwtSecret,
  resolveTenantId,
  seedEmployeeSession,
} from '../support/eval360';

// TC-3 员工端飞书 H5 JSAPI 原地免登（卡片链接 → tt.requestAuthCode → 换票 → 目标页，全程不整页跳转）。
//
// 飞书客户端与 code 换票是外部依赖：
//   - 用飞书 UA 打开浏览器上下文，并经 addInitScript 注入 window.h5sdk / window.tt 替身
//     （记录 requestAuthCode 收到的 appId，每次返回新的 e2e-code-N）。h5sdk 已存在时员工端不再加载 CDN 脚本；
//   - 替身 POST /x/linapro-employee-core/api/v1/emp/login（返回用真实员工签发的 JWT），其余员工端接口走真实后端；
//   - 拦截飞书 CDN 与 open.feishu.cn：任何对网页 OAuth 授权页的访问都记为失败（OAuth 已删除）。
// 断言落在业务结果：换票使用卡片链接的企业应用 id、地址栏从不出现 code/state、员工端接口接受换得的 JWT、
// 换票失败后「重试」重新取 code、token 过期后原地重登并只换一次票、非飞书环境只提示「请在飞书中打开」。

const cardLarkAppId = 'cli_e2e_card';
// 员工换票接口的真实挂载路径（employee-core 插件路由前缀）。须精确到插件前缀：
// 宽泛的 **/api/v1/emp/login 会把错误路径的请求也替身掉，掩盖 404。
const empLoginRoute = '**/x/linapro-employee-core/api/v1/emp/login';
const cardTarget = `/eval-360/emp/todo?larkAppId=${cardLarkAppId}`;
const larkUserAgent =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36 Lark/7.30.8 LarkLocale/zh_CN ttnet SDK-Version/7.30.8 WebApp';

interface LoginBody {
  code?: string;
  larkAppId?: string;
}

// 页面内飞书 SDK 替身：h5sdk.ready 立即回调；tt.requestAuthCode 记录 appId 并按次返回新 code。
function installLarkSdkStub() {
  const calls: string[] = [];
  let seq = 0;
  (window as any).__larkAuthCalls = calls;
  (window as any).h5sdk = { ready: (cb: () => void) => cb() };
  (window as any).tt = {
    requestAuthCode: (o: { appId: string; success: (r: { code: string }) => void }) => {
      calls.push(o.appId);
      seq += 1;
      o.success({ code: `e2e-code-${seq}` });
    },
  };
}

// 新建飞书 UA 的浏览器上下文；withSdk=false 时不注入 SDK 替身（用于 SDK 不可用场景）。
async function newLarkContext(browser: Browser, withSdk = true): Promise<BrowserContext> {
  const context = await browser.newContext({ baseURL: config.baseURL, userAgent: larkUserAgent });
  if (withSdk) {
    await context.addInitScript(installLarkSdkStub);
  }
  return context;
}

// 记录页面访问过的全部地址，用于断言从未整页跳转到飞书授权页、地址栏从未出现 code/state。
function trackNavigations(page: Page): string[] {
  const urls: string[] = [];
  page.on('framenavigated', (frame) => {
    if (frame === page.mainFrame()) urls.push(frame.url());
  });
  return urls;
}

// 拦截外部飞书地址：网页 OAuth 授权页一律记为违规访问；CDN 由调用方决定返回内容。
async function guardFeishuHosts(page: Page, sdkBody = '') {
  const oauthHits: string[] = [];
  await page.route('https://open.feishu.cn/**', (route) => {
    oauthHits.push(route.request().url());
    return route.abort();
  });
  await page.route('https://lf-scm-cn.feishucdn.com/**', (route) =>
    route.fulfill({ body: sdkBody, contentType: 'application/javascript' }),
  );
  return oauthHits;
}

// 替身换票接口：记录每次请求体，按 respond 返回。
async function stubEmpLogin(page: Page, respond: (body: LoginBody, n: number) => object) {
  const bodies: LoginBody[] = [];
  await page.route(empLoginRoute, async (route) => {
    const body = route.request().postDataJSON() as LoginBody;
    bodies.push(body);
    await route.fulfill({ contentType: 'application/json', json: respond(body, bodies.length) });
  });
  return bodies;
}

// 准备一个真实在职员工的 JWT（员工端接口走真实后端校验）。
async function realEmployeeLogin() {
  const api = await newAdminApi(adminStorageStatePath);
  try {
    const secret = readEmpJwtSecret();
    expect(secret, '缺少员工 JWT 密钥（plugin.linapro-employee-core.emp_jwt_secret）').not.toBe('');
    const [employee] = await pickActiveEmployees(api, 1);
    const tenantId = resolveTenantId(adminStorageStatePath);
    const token = mintEmpToken(secret, employee!.empId, tenantId);
    const loginOk = {
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
    };
    return { loginOk, token };
  } finally {
    await api.dispose();
  }
}

test.describe('TC-3 员工端飞书 H5 JSAPI 原地免登', () => {
  test('TC-3a: 飞书内无会话打开卡片链接，原地取 code 换票后进入待办，地址栏无 code/state、不跳授权页', async ({
    browser,
  }) => {
    const { loginOk, token } = await realEmployeeLogin();
    const context = await newLarkContext(browser);
    const page = await context.newPage();
    try {
      const oauthHits = await guardFeishuHosts(page);
      const loginBodies = await stubEmpLogin(page, () => loginOk);
      const visited = trackNavigations(page);

      await page.goto(workspacePath(cardTarget), { waitUntil: 'domcontentloaded' });

      // 换票得到的 JWT 被真实员工端接口接受：待办页真实渲染。
      await expect(page.getByTestId('eval360-emp-todo')).toBeVisible();
      // requestAuthCode 使用卡片链接的企业应用 id，换票请求携带同一 appId 与取到的 code。
      expect(await page.evaluate(() => (window as any).__larkAuthCalls)).toEqual([cardLarkAppId]);
      expect(loginBodies).toEqual([{ code: 'e2e-code-1', larkAppId: cardLarkAppId }]);
      expect(await page.evaluate(() => localStorage.getItem('linapro:emp-auth-token'))).toBe(token);
      expect(await page.evaluate(() => localStorage.getItem('linapro:emp-lark-app-id'))).toBe(
        cardLarkAppId,
      );
      // 原地免登：从未访问飞书 OAuth 授权页，地址栏从未出现 code / state。
      expect(oauthHits).toEqual([]);
      for (const url of visited) {
        expect(url).not.toMatch(/[?&#](code|state)=/);
      }
      expect(page.url()).toContain('/eval-360/emp/todo');
    } finally {
      await context.close();
    }
  });

  test('TC-3b: 换票失败展示原因与重试；重试重新取新 code，不复用已失败的 code', async ({ browser }) => {
    const context = await newLarkContext(browser);
    const page = await context.newPage();
    try {
      const oauthHits = await guardFeishuHosts(page);
      const loginBodies = await stubEmpLogin(page, () => ({
        code: 1,
        data: null,
        message: 'E2E 登录码无效',
      }));

      await page.goto(workspacePath(cardTarget), { waitUntil: 'domcontentloaded' });

      await expect(page.getByText('登录失败')).toBeVisible();
      // 后端错误文案原样展示；重试按钮渲染译文（Ant Design 会在两字按钮间插入空格）。
      await expect(page.locator('.ant-result-subtitle')).toHaveText('E2E 登录码无效');
      const retry = page.getByTestId('eval360-emp-login-retry');
      await expect(retry).toHaveText(/^重\s?试$/);

      await retry.click();
      await expect.poll(() => loginBodies.length).toBe(2);

      // 两次换票分别使用两次 requestAuthCode 取到的不同 code。
      expect(loginBodies.map((b) => b.code)).toEqual(['e2e-code-1', 'e2e-code-2']);
      expect(loginBodies.every((b) => b.larkAppId === cardLarkAppId)).toBe(true);
      await expect(page.locator('.ant-result-subtitle')).toHaveText('E2E 登录码无效');
      // 失败不降级到 OAuth。
      expect(oauthHits).toEqual([]);
      expect(page.url()).not.toMatch(/[?&](code|state)=/);
    } finally {
      await context.close();
    }
  });

  test('TC-3c: 非飞书环境打开卡片链接只提示「请在飞书中打开」，不加载 SDK、不发起任何授权', async ({
    browser,
  }) => {
    const context = await browser.newContext({ baseURL: config.baseURL });
    const page = await context.newPage();
    try {
      const sdkRequests: string[] = [];
      page.on('request', (req) => {
        if (/feishucdn\.com|open\.feishu\.cn/.test(req.url())) sdkRequests.push(req.url());
      });
      const loginBodies = await stubEmpLogin(page, () => ({ code: 0, data: null, message: 'ok' }));

      await page.goto(workspacePath(cardTarget), { waitUntil: 'domcontentloaded' });

      await expect(page.getByTestId('emp-login-not-in-lark')).toBeVisible();
      await expect(page.getByText('请在飞书中打开')).toBeVisible();
      await expect(
        page.getByText('该页面需要飞书身份，请从飞书消息卡片或飞书工作台进入。'),
      ).toBeVisible();
      expect(sdkRequests).toEqual([]);
      expect(loginBodies).toEqual([]);
    } finally {
      await context.close();
    }
  });

  test('TC-3d: 飞书 UA 下 SDK 加载后不可用，展示错误与重试，不降级到 OAuth', async ({ browser }) => {
    const context = await newLarkContext(browser, false);
    const page = await context.newPage();
    try {
      // CDN 返回空脚本：加载成功但没有定义 window.h5sdk。
      const oauthHits = await guardFeishuHosts(page, '/* empty */');
      const loginBodies = await stubEmpLogin(page, () => ({ code: 0, data: null, message: 'ok' }));

      await page.goto(workspacePath(cardTarget), { waitUntil: 'domcontentloaded' });

      await expect(page.getByText('飞书客户端组件加载失败，请检查网络后重试。')).toBeVisible();
      await expect(page.getByTestId('eval360-emp-login-retry')).toBeVisible();
      expect(loginBodies).toEqual([]);
      expect(oauthHits).toEqual([]);
    } finally {
      await context.close();
    }
  });

  test('TC-3e: 员工 JWT 过期后操作页面，原地重新取 code 换票并重放请求，不经过登录页', async ({
    browser,
  }) => {
    const { loginOk, token } = await realEmployeeLogin();
    const context = await newLarkContext(browser);
    const page = await context.newPage();
    try {
      // 预置一个后端不接受的旧票（模拟 2 小时后过期），企业应用 id 为卡片写入值。
      await seedEmployeeSession(page, 'expired-e2e-token', cardLarkAppId);
      const oauthHits = await guardFeishuHosts(page);
      const loginBodies = await stubEmpLogin(page, () => loginOk);
      const visited = trackNavigations(page);

      await page.goto(workspacePath('/eval-360/emp/todo'), { waitUntil: 'domcontentloaded' });

      // 待办接口先 401，原地换票后重放成功：页面直接渲染，未经过登录页。
      await expect(page.getByTestId('eval360-emp-todo')).toBeVisible();
      expect(loginBodies).toEqual([{ code: 'e2e-code-1', larkAppId: cardLarkAppId }]);
      expect(await page.evaluate(() => (window as any).__larkAuthCalls)).toEqual([cardLarkAppId]);
      expect(await page.evaluate(() => localStorage.getItem('linapro:emp-auth-token'))).toBe(token);
      expect(visited.some((url) => url.includes('/eval-360/emp/login'))).toBe(false);
      expect(oauthHits).toEqual([]);
    } finally {
      await context.close();
    }
  });
});
