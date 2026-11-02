import { expect, type Locator, type Page } from '@playwright/test';

import { workspacePath } from '../../../../../../hack/tests/fixtures/config';

const confirmButtonPattern = /确\s*定|确\s*认|OK/iu;

// LarkPushApiKeyPage 封装「系统设置 → 飞书推送凭据」管理页的页面交互。
export class LarkPushApiKeyPage {
  readonly page: Page;

  constructor(page: Page) {
    this.page = page;
  }

  async goto() {
    await this.page.goto(workspacePath('/setting/linapro-lark-push-api-keys'), {
      waitUntil: 'domcontentloaded',
    });
    await expect(this.page.getByTestId('lark-push-api-key-add')).toBeVisible();
  }

  /** 列表中包含某 app_key 的行。 */
  row(appKey: string): Locator {
    return this.page.locator('.vxe-body--row', { hasText: appKey }).first();
  }

  /** 用关键字查询列表，使目标行出现在第一页。 */
  async search(keyword: string) {
    await this.page.getByPlaceholder('名称或 app_key').fill(keyword);
    await this.page.getByRole('button', { name: /搜\s*索|查\s*询|Search/iu }).click();
  }

  get modal(): Locator {
    return this.page.getByRole('dialog').filter({ has: this.page.getByTestId('lark-push-api-key-form') });
  }

  /**
   * 打开新增弹窗并填写表单，返回提交前表单中的 secret。
   * secret 不传时保留弹窗打开时浏览器预填的随机值；传空串表示清空（用于验证必填）。
   */
  async fillCreateForm(input: { appKey: string; group: string; name: string; secret?: string }): Promise<string> {
    await this.page.getByTestId('lark-push-api-key-add').click();
    await expect(this.modal).toBeVisible();
    await this.page.getByTestId('lark-push-api-key-name').fill(input.name);
    await this.page.getByTestId('lark-push-api-key-app-key').fill(input.appKey);
    await this.page.getByTestId('lark-push-api-key-group').click();
    await this.page.locator('.ant-select-item-option', { hasText: input.group }).first().click();
    const secretInput = this.page.getByTestId('lark-push-api-key-secret');
    if (input.secret !== undefined) {
      await secretInput.fill(input.secret);
    }
    return secretInput.inputValue();
  }

  async submitModal() {
    await this.modal.getByRole('button', { name: confirmButtonPattern }).click();
  }

  get secretModal(): Locator {
    return this.page.getByRole('dialog').filter({ has: this.page.getByTestId('lark-push-secret-value') });
  }

  /** 读取密钥弹窗中展示的 secret 并关闭弹窗。 */
  async readSecretAndClose(): Promise<string> {
    await expect(this.secretModal).toBeVisible();
    const input = this.page.getByTestId('lark-push-secret-value');
    await expect(input).not.toHaveValue('');
    const value = await input.inputValue();
    await this.secretModal.getByRole('button', { name: /关\s*闭/u }).click();
    await expect(this.secretModal).toBeHidden();
    return value;
  }

  async openEdit(id: number) {
    await this.page.getByTestId(`lark-push-api-key-edit-${id}`).click();
    await expect(this.modal).toBeVisible();
    await expect(this.page.getByTestId('lark-push-api-key-app-key')).not.toHaveValue('');
  }

  /** 在已打开的弹窗中填写群聊 chat_id；分组有推送应用下拉时选第一个（属于该分组）。 */
  async fillChat(chatId: string) {
    await this.page.getByTestId('lark-push-api-key-chat-id').fill(chatId);
    const pushApp = this.page.getByTestId('lark-push-api-key-chat-push-app');
    if (await pushApp.isVisible()) {
      await pushApp.click();
      await this.page.locator('.ant-select-dropdown:visible .ant-select-item-option').first().click();
    }
  }

  async openSecret(id: number) {
    await this.page.getByTestId(`lark-push-api-key-secret-${id}`).click();
  }

  async toggleStatus(id: number) {
    await this.page.getByTestId(`lark-push-api-key-status-${id}`).click();
  }

  async delete(id: number) {
    await this.page.getByTestId(`lark-push-api-key-delete-${id}`).click();
    await this.page.locator('.ant-popover').getByRole('button', { name: confirmButtonPattern }).click();
  }
}
