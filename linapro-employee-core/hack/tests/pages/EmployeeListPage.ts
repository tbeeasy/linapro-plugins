import { expect, type Locator, type Page } from '@playwright/test';

import { workspacePath } from '../../../../../../hack/tests/fixtures/config';

const pluginMenuPattern = /通讯录管理|Contacts/iu;
const employeeMenuPattern = /员工列表/iu;
// antd 会给两字按钮的中文自动插入空格（「搜 索」「详 情」「解 绑」），
// 所以按钮文案的正则必须在字之间允许空白，否则永远匹配不到。
const twoCharButton = (text: string) => new RegExp([...text].join('\\s*'), 'u');
const searchButtonPattern = new RegExp(
  [twoCharButton('搜索').source, '查询', 'Search', 'Query'].join('|'),
  'iu',
);
const detailButtonPattern = new RegExp(
  [twoCharButton('详情').source, 'Detail'].join('|'),
  'iu',
);
const unbindButtonPattern = new RegExp(
  [twoCharButton('解绑').source, 'Unbind'].join('|'),
  'iu',
);

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

  /** gotoHome 打开工作台首页并等待侧栏渲染，用于断言菜单投影结果。 */
  async gotoHome() {
    await this.page.goto(workspacePath('/dashboard/analytics'), {
      waitUntil: 'domcontentloaded',
    });
    await expect(this.sidebarMenu).toBeVisible();
  }

  /** menuVisible 判断侧栏是否出现「通讯录管理 → 员工列表」。 */
  async menuVisible() {
    await expect(this.sidebarMenu.getByText(pluginMenuPattern).first()).toBeVisible();
    await expect(this.sidebarMenu.getByText(employeeMenuPattern).first()).toBeVisible();
  }

  row(name: string): Locator {
    return this.page.locator('tr', { hasText: name }).first();
  }

  /** detailButton 按 EmpID 定位行内「详情」按钮。 */
  detailButton(empId: number): Locator {
    // 操作列 fixed:"right"，vxe 会把按钮渲染在与数据行分开的固定列容器里，
    // 用 tr 的 hasText 永远找不到它；页面给每行按钮挂了 employee-detail-<empId>。
    return this.page.getByTestId(`employee-detail-${empId}`).first();
  }

  /** search 在搜索栏按姓名/工号过滤并查询。 */
  async search(keyword: string) {
    await this.grid.getByPlaceholder(/姓名或工号/iu).fill(keyword);
    await this.grid.getByRole('button', { name: searchButtonPattern }).click();
  }

  /** openDetail 点行内「详情」打开抽屉，等待基本信息渲染。 */
  async openDetail(empId: number) {
    // testid 直接挂在按钮元素上，不能再向内找 button。
    await this.detailButton(empId).click();
    await expect(this.drawer).toBeVisible();
    await expect(this.drawer.getByText('员工详情')).toBeVisible();
    await this.waitForDrawerSettled();
  }

  /**
   * waitForDrawerSettled 等抽屉滑入动画结束。
   * 抽屉从右侧滑入（500ms），动画中途截图会拍到被裁掉一半的内容，
   * 也会让基于抽屉内元素的断言撞上动画中间态。
   */
  async waitForDrawerSettled() {
    await expect
      .poll(
        async () => {
          const first = await this.drawer.boundingBox();
          await this.page.waitForTimeout(120);
          const second = await this.drawer.boundingBox();
          if (!first || !second) {
            return false;
          }
          return (
            Math.abs(first.x - second.x) < 1 &&
            Math.abs(first.width - second.width) < 1
          );
        },
        { message: '抽屉滑入动画未在预期时间内稳定', timeout: 5000 },
      )
      .toBe(true);
  }

  /** closeDrawer 用 Esc 关闭抽屉（抽屉只有图标关闭键，无文字按钮）。 */
  async closeDrawer() {
    await this.page.keyboard.press('Escape');
    await expect(this.drawer).toBeHidden();
  }

  /** unbindIdentity 在抽屉里解绑指定 open_id 的身份。 */
  async unbindIdentity(openId: string) {
    const line = this.drawer.locator('tr', { hasText: openId }).first();
    await line.getByRole('button', { name: unbindButtonPattern }).click();
    await this.page
      .locator('.ant-popconfirm')
      .getByRole('button', { name: /确\s*定|OK/iu })
      .click();
  }
}
