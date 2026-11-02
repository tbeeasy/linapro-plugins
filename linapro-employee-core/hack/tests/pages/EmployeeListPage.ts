import { expect, type Locator, type Page } from '@playwright/test';

import { workspacePath } from '../../../../../../hack/tests/fixtures/config';

const pluginMenuPattern = /通讯录管理|Contacts/iu;
const employeeMenuPattern = /员工列表/iu;

// EmployeeListPage 封装后台「通讯录管理 → 员工列表」页与详情抽屉的交互。
export class EmployeeListPage {
  readonly page: Page;

  constructor(page: Page) {
    this.page = page;
  }

  get sidebarMenu(): Locator {
    return this.page
      .locator('aside')
      .filter({ has: this.page.locator('ul[role="menu"]') })
      .first();
  }

  get grid(): Locator {
    return this.page.getByTestId('linapro-employee-core-employee-grid').first();
  }

  get drawer(): Locator {
    return this.page.locator('[role="dialog"]').filter({ visible: true }).first();
  }

  /** goto 打开列表页并等待表格渲染。 */
  async goto() {
    await this.page.goto(workspacePath('/contacts/employees'), {
      waitUntil: 'domcontentloaded',
    });
    await expect(this.grid).toBeVisible();
  }

  /** menuVisible 判断侧栏是否出现「通讯录管理 → 员工列表」。 */
  async menuVisible() {
    await expect(this.sidebarMenu.getByText(pluginMenuPattern).first()).toBeVisible();
    await expect(this.sidebarMenu.getByText(employeeMenuPattern).first()).toBeVisible();
  }

  row(name: string): Locator {
    return this.page.locator('tr', { hasText: name }).first();
  }

  /** search 在搜索栏按姓名/工号过滤并查询。 */
  async search(keyword: string) {
    await this.grid.getByPlaceholder(/姓名或工号/iu).fill(keyword);
    await this.grid.getByRole('button', { name: /搜索|查询|Search|Query/iu }).click();
  }

  /** openDetail 点行内「详情」打开抽屉，等待基本信息渲染。 */
  async openDetail(name: string) {
    await this.row(name).getByRole('button', { name: /详情|Detail/iu }).click();
    await expect(this.drawer).toBeVisible();
    await expect(this.drawer.getByText('员工详情')).toBeVisible();
  }

  /** closeDrawer 用 Esc 关闭抽屉（抽屉只有图标关闭键，无文字按钮）。 */
  async closeDrawer() {
    await this.page.keyboard.press('Escape');
    await expect(this.drawer).toBeHidden();
  }

  /** unbindIdentity 在抽屉里解绑指定 open_id 的身份。 */
  async unbindIdentity(openId: string) {
    const line = this.drawer.locator('tr', { hasText: openId }).first();
    await line.getByRole('button', { name: /解绑|Unbind/iu }).click();
    await this.page
      .locator('.ant-popconfirm')
      .getByRole('button', { name: /确\s*定|OK/iu })
      .click();
  }
}
