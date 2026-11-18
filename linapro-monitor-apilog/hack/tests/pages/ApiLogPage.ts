import type { Locator, Page } from '@playwright/test';

import { expect } from '@playwright/test';

import { workspacePath } from '@host-tests/fixtures/config';

import {
  waitForBusyIndicatorsToClear,
  waitForDialogReady,
  waitForRouteReady,
  waitForTableReady,
} from '@host-tests/support/ui';

/** 接口请求日志页面对象：只封装定位与稳定等待，断言留在用例里。 */
export class ApiLogPage {
  constructor(private readonly page: Page) {}

  /** 菜单路径。 */
  static readonly routePath = '/monitor/apilog';

  get table(): Locator {
    return this.page.locator('.vxe-table').first();
  }

  get rows(): Locator {
    return this.page.locator('.vxe-body--row');
  }

  get searchButton(): Locator {
    return this.page.getByRole('button', { name: /搜\s*索|Search/i }).first();
  }

  get resetButton(): Locator {
    return this.page.getByRole('button', { name: /重\s*置|Reset/i }).first();
  }

  /**
   * 打开页面并等到列表接口完成。
   *
   * 页面加载即会按默认 7 天窗口查询一次，因此直接等这个响应即可，不需要额外触发查询。
   */
  async open(): Promise<void> {
    const responsePromise = this.page.waitForResponse(
      (response) =>
        response.url().includes('/x/linapro-monitor-apilog/api/v1/apilog/records') &&
        response.request().method() === 'GET',
      { timeout: 30000 },
    );
    // 显式补工作台前缀：自建的浏览器上下文没有 fixture 的 workspace goto 包装。
    await this.page.goto(workspacePath(ApiLogPage.routePath));
    await responsePromise.catch(() => undefined);
    await waitForRouteReady(this.page);
    await waitForTableReady(this.page);
  }

  /** 在筛选表单里填写某员工工号并查询。 */
  async filterByCallerNo(callerNo: string): Promise<void> {
    // 按 schema 的 fieldName 定位，不依赖输入框顺序（时间范围本身就占两个输入框）。
    await this.page.locator('input[name="callerNo"]').fill(callerNo);
    await this.searchButton.click();
    await this.page.waitForLoadState('networkidle').catch(() => undefined);
    await waitForBusyIndicatorsToClear(this.table);
  }

  /** 打开第一行详情抽屉并返回抽屉定位器。 */
  async openFirstDetail(): Promise<Locator> {
    await this.page.getByTestId('apilog-detail-open').first().click();
    return await waitForDialogReady(
      this.page.getByLabel('接口请求日志详情'),
      30000,
    );
  }

  /** 断言列表为空状态（没有任何记录行）。 */
  async expectEmpty(): Promise<void> {
    await expect(this.rows).toHaveCount(0);
  }
}
