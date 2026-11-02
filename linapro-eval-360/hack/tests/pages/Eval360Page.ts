import { expect, type Locator, type Page } from '@playwright/test';

import { workspacePath } from '../../../../../../hack/tests/fixtures/config';

const pluginMenuPattern = /360\s*测评/i;
const confirmButtonPattern = /确\s*定|确\s*认|OK/iu;

// Eval360Page 封装 360 测评管理端（项目列表 → 项目详情 → 问卷详情三菜单）的全部页面交互。
// 深层级由单挂载点内的视图状态切换，不产生 URL 变化，故所有跳转都靠点击驱动。
export class Eval360Page {
  readonly page: Page;

  constructor(page: Page) {
    this.page = page;
  }

  // ---- 菜单与列表 ----

  get sidebarMenu(): Locator {
    return this.page
      .locator('aside')
      .filter({ has: this.page.locator('ul[role="menu"]') })
      .first();
  }

  async goto() {
    // 插件路由不在宿主 fixture 的工作台路径白名单内，须显式拼工作台 base（如 /admin）。
    await this.page.goto(workspacePath('/eval-360/projects'), { waitUntil: 'domcontentloaded' });
    await expect(this.sidebarMenu.getByText(pluginMenuPattern).first()).toBeVisible();
    await expect(this.page.getByTestId('eval360-project-list')).toBeVisible();
  }

  projectRow(name: string): Locator {
    return this.page.locator('tr', { hasText: name }).first();
  }

  // createProject 经真实 UI 在项目抽屉中新建项目：名称、描述、起止日期（date-only）。
  // 断言落到业务结果——列表刷新后出现新建的项目行与其描述列。
  async createProject(
    name: string,
    description = '',
    dates?: { start?: string; end?: string },
  ) {
    await this.page.getByTestId('eval360-project-create').click();
    const drawer = this.page.locator('[role="dialog"]').filter({ visible: true }).first();
    await expect(drawer).toBeVisible();
    await drawer.getByLabel('项目名称').first().fill(name);
    if (description) {
      // 描述字段为 Textarea，按 label 定位填入。
      await drawer.getByLabel('描述').first().fill(description);
    }
    if (dates?.start) {
      await drawer.getByLabel('开始时间').first().click();
      await this.page.keyboard.type(dates.start);
      await this.page.keyboard.press('Enter');
    }
    if (dates?.end) {
      await drawer.getByLabel('截止时间').first().click();
      await this.page.keyboard.type(dates.end);
      await this.page.keyboard.press('Enter');
    }
    await drawer.getByRole('button', { name: confirmButtonPattern }).click();
    // 列表刷新后新项目行可见（业务结果：项目已落库并出现在列表）。
    await expect(this.page.getByText(name).first()).toBeVisible();
  }

  async openProject(name: string) {
    await this.projectRow(name).getByTestId('eval360-project-detail').click();
    await expect(this.page.getByTestId('eval360-project-detail-panel')).toBeVisible();
  }

  async sendProject() {
    await this.page.getByTestId('eval360-project-send').first().click();
  }

  async openQuestionnaire(title: string) {
    await this.page
      .getByTestId('eval360-questionnaire-card')
      .filter({ hasText: title })
      .first()
      .click();
    await expect(this.page.getByTestId('eval360-questionnaire-detail')).toBeVisible();
  }

  // ---- 问卷三菜单 tab ----

  async openTab(tab: '问卷' | '关系' | '报告') {
    const key =
      tab === '关系' ? 'relation' : tab === '报告' ? 'report' : 'questionnaire';
    await this.page.getByTestId(`eval360-nav-${key}`).click();
  }

  // ---- 编辑问卷入口（三选一模式） ----

  get editEntryButton(): Locator {
    return this.page.getByTestId('eval360-questionnaire-edit-entry');
  }

  /**
   * 经「编辑问卷」入口选择编辑模式：keep 直接进编辑器，clear 会先二次确认再进，
   * copy 会弹出复制命名弹窗（由调用方后续处理），不在此助手内点确认。
   */
  async startEdit(mode: 'clear' | 'copy' | 'keep') {
    await this.editEntryButton.click();
    await expect(this.page.getByTestId('eval360-edit-mode-modal')).toBeVisible();
    await this.page.getByTestId(`eval360-edit-mode-${mode}`).click();
    await this.page.getByTestId('eval360-edit-mode-next').click();
    if (mode === 'clear') {
      await this.confirmDialog();
    }
  }

  // ---- 问卷编辑器 ----

  async addDimension(name = '维度1') {
    await this.page.getByTestId('eval360-dimension-add').click();
    // 新增维度改为弹窗录入名称：填名后确认，随后题目编辑器可见。
    const nameInput = this.page.getByTestId('eval360-dimension-name');
    await expect(nameInput).toBeVisible();
    await nameInput.fill(name);
    await this.page.keyboard.press('Enter');
    await expect(this.page.getByTestId('eval360-question-editor')).toBeVisible();
  }

  /**
   * 新建一道单选题：逐行录入文案、分值，可选勾选「允许评语」。
   */
  async createSingleChoice(
    title: string,
    options: Array<{ label: string; score: string; allowText?: boolean }>,
    required = true,
  ) {
    // 底部改为「+ 新增题目」按钮：先点按追加一张空白草稿卡（编辑态），再逐项录入。
    await this.page.getByTestId('eval360-question-add').click();
    await this.setQuestionType('单选');
    // 标题改用富文本编辑器（TiptapEditor，非 <input>）：点击 contenteditable 后键入。
    const titleEditor = this.page
      .getByTestId('eval360-question-new-title')
      .locator('[contenteditable="true"]');
    await titleEditor.click();
    await titleEditor.fill(title);
    if (required) {
      // 必填开关已移到保存按钮前，按 testid 精确点击，避免与「填写描述」开关混淆。
      await this.page.getByTestId('eval360-question-required').click();
    }
    for (let i = 0; i < options.length; i += 1) {
      if (i > 0) {
        await this.page.getByTestId('eval360-question-add-option').click();
      }
      const option = options[i]!;
      const row = this.page.getByTestId('eval360-question-editor').locator('.flex.items-center.gap-2').nth(i);
      await row.locator('input[type="text"], input:not([type])').first().fill(option.label);
      await row.locator('input[type="number"], .ant-input-number input').first().fill(option.score);
      if (option.allowText) {
        await row.getByRole('checkbox', { name: /允许评语/ }).check();
      }
    }
    await this.page.getByTestId('eval360-question-create').click();
    await expect(this.questionItem(title)).toBeVisible();
  }

  questionItem(title: string): Locator {
    return this.page.getByTestId('eval360-question-item').filter({ hasText: title }).first();
  }

  private async setQuestionType(label: string) {
    const select = this.page.getByTestId('eval360-question-type');
    await select.click();
    await this.page.getByTitle(label, { exact: true }).click();
  }

  /**
   * 经真实编辑器为某道已存题目设置「受限评价者」名单并保存。
   * 选项源为评价关系树中的评价者（label=姓名（工号）），按名称搜索后点选；
   * 保存以 PUT /question/{id} 成功响应作为业务结果。
   */
  async setExcludedEvaluators(questionTitle: string, evaluatorLabels: string[]) {
    const card = this.questionItem(questionTitle);
    await card.hover();
    await card.getByTestId('eval360-question-edit').click();
    // 表单项标签渲染为译文（而非原始 i18n key）。
    await expect(card.getByText('受限评价者', { exact: true })).toBeVisible();
    const select = card.getByTestId('eval360-question-excluded-evaluators');
    for (const label of evaluatorLabels) {
      await select.click();
      await select.locator('input').fill(label);
      await this.page
        .locator('.ant-select-dropdown:visible .ant-select-item-option')
        .filter({ hasText: label })
        .first()
        .click();
    }
    const saved = this.page.waitForResponse(
      (res) => /\/question\/\d+/.test(res.url()) && res.request().method() === 'PUT',
    );
    await card.getByTestId('eval360-question-save').click();
    expect((await saved).ok()).toBeTruthy();
  }

  // ---- 评价关系 ----

  relationImportInput(): Locator {
    return this.page.getByTestId('eval360-relation-import-upload').locator('input[type="file"]');
  }

  // 全量导入在弹窗内：先点工具栏「全量导入」打开弹窗，再向拖拽上传控件投递文件。
  async importRelations(filePath: string) {
    await this.page.getByTestId('eval360-relation-import-btn').click();
    await this.relationImportInput().setInputFiles(filePath);
  }

  // 关闭全量导入弹窗（导入结果提示后回到明细表）。
  async closeImportModal() {
    const modal = this.page
      .locator('.ant-modal')
      .filter({ has: this.page.getByTestId('eval360-relation-import-upload') });
    await modal.locator('.ant-modal-close').click();
    await expect(modal).toBeHidden();
  }

  // 导出评价关系 Excel（列与导入模板一致）。
  async exportRelations(): Promise<Buffer> {
    return this.downloadBlob('eval360-relation-export-btn');
  }

  roleWeightInput(roleName: string): Locator {
    return this.page
      .locator('div')
      .filter({ has: this.page.getByText(roleName, { exact: true }) })
      .getByTestId('eval360-role-weight')
      .first();
  }

  async setRoleWeight(roleName: string, weight: string) {
    const input = this.roleWeightInput(roleName);
    await input.fill(weight);
    await input.press('Enter');
  }

  // 评价关系明细表中某被评估人所在行（按被评估人姓名过滤）。
  evaluateeRow(name: string): Locator {
    return this.page
      .getByTestId('eval360-relation-matrix')
      .locator('tbody tr')
      .filter({ hasText: name });
  }

  // 删除当前被评估人及其全部评价关系：点行内「删除」后在二次确认弹窗点「确定」。
  async deleteEvaluatee(name: string) {
    await this.evaluateeRow(name).getByTestId('eval360-evaluatee-delete-btn').click();
    await this.page.locator('.ant-modal-confirm-btns .ant-btn-primary').click();
  }

  // 当前可见的关系弹窗（新增 / 编辑均为 Vben Modal）。
  relationDialog(): Locator {
    return this.page.locator('[role="dialog"]').filter({ visible: true }).first();
  }

  // 被评估人行的「新增」：弹窗已预填被评估人，只补录一名评价人姓名及其角色（角色按名称 upsert；
  // 不录入工号，后端按在职员工姓名匹配）。
  // 传入 expectPrefill 时先断言弹窗预填了该被评估人的职务/部门（FB-13）。
  async addRelationForEvaluatee(
    evaluateeName: string,
    evaluator: { employeeNo: string; name: string },
    roleName: string,
    expectPrefill?: { jobTitle: string; department: string },
  ) {
    await this.evaluateeRow(evaluateeName).getByTestId('eval360-evaluatee-add-btn').click();
    const dialog = this.relationDialog();
    if (expectPrefill) {
      await expect(dialog.getByTestId('eval360-batch-evaluatee-job-title')).toHaveValue(
        expectPrefill.jobTitle,
      );
      await expect(dialog.getByTestId('eval360-batch-evaluatee-department')).toHaveValue(
        expectPrefill.department,
      );
    }
    await dialog.getByTestId('eval360-batch-evaluator-names').fill(evaluator.name);
    await dialog.getByTestId('eval360-batch-role-names').fill(roleName);
    await dialog.getByRole('button', { name: confirmButtonPattern }).click();
    await expect(dialog).toBeHidden();
  }

  // 工具栏「新增评价关系」填一组（一名被评估人 + 一名评价人）并点确定，不等待弹窗关闭：
  // 供重名/找不到等被拒绝的路径断言错误提示与弹窗仍保持打开。返回该弹窗。
  async submitBatchAdd(evaluateeName: string, evaluatorName: string, roleName: string): Promise<Locator> {
    await this.page.getByTestId('eval360-relation-add-btn').click();
    const dialog = this.relationDialog();
    await dialog.getByTestId('eval360-batch-evaluatee-name').fill(evaluateeName);
    await dialog.getByTestId('eval360-batch-evaluator-names').fill(evaluatorName);
    await dialog.getByTestId('eval360-batch-role-names').fill(roleName);
    await dialog.getByRole('button', { name: confirmButtonPattern }).click();
    return dialog;
  }

  // 被评估人在指定角色列下的单元格：按表头标题定位列序号（角色列为动态列）。
  async roleCell(evaluateeName: string, roleName: string): Promise<Locator> {
    const headers = this.page.getByTestId('eval360-relation-matrix').locator('thead th');
    await expect(headers.filter({ hasText: roleName })).toHaveCount(1);
    const titles = await headers.allInnerTexts();
    const index = titles.findIndex((t) => t.trim() === roleName);
    expect(index).toBeGreaterThan(0);
    return this.evaluateeRow(evaluateeName).locator('td').nth(index);
  }

  // 角色单元格的「编辑」：只管理该被评估人在该角色下的评价人。
  async openRoleEdit(evaluateeName: string, roleName: string) {
    const cell = await this.roleCell(evaluateeName, roleName);
    await cell.getByTestId('eval360-role-edit-btn').click();
    await expect(this.relationDialog()).toBeVisible();
  }

  // FB-14 回归：角色单元格内的评价人必须「一人一行」，宽度不足时由内层容器横向滚动兜底，
  // 不得把「姓名(工号)」从中间折行（修复前该 span 带 break-all，列被压窄后会断成多行）。
  // 判据用 getClientRects().length：折行的行内元素会返回多个矩形，未折行恒为 1 个。
  async expectEvaluatorLabelsSingleLine(
    evaluateeName: string,
    roleName: string,
    expectedLabels: string[],
  ): Promise<void> {
    const cell = await this.roleCell(evaluateeName, roleName);
    const labels = cell.locator('span.whitespace-nowrap');
    await expect(labels).toHaveCount(expectedLabels.length);
    const metrics = await labels.evaluateAll((nodes) =>
      nodes.map((node) => ({
        lines: node.getClientRects().length,
        text: node.textContent ?? '',
      })),
    );
    expect(metrics.map((m) => m.text.trim())).toEqual(expectedLabels);
    for (const metric of metrics) {
      expect(metric.lines).toBe(1);
    }

    // 宽度兜底：单人列横向贴合最长标签（w-max），滚动容器仅在放不下时才产生溢出量。
    // 已评价 / 未评价两组各有一个滚动容器，按标签实际落在哪个容器里取，不能用 .first()：
    // 只渲染未评价一组时 .first() 会命中空的已评价容器，导致宽度断言失去意义。
    const scrollWrapper = cell
      .locator('.overflow-x-auto')
      .filter({ has: this.page.locator('span.whitespace-nowrap') })
      .first();
    await expect(scrollWrapper).toHaveCount(1);
    const widest = await scrollWrapper.evaluate((node) => {
      const inner = node.firstElementChild as HTMLElement | null;
      return {
        clientWidth: node.clientWidth,
        innerWidth: inner ? inner.getBoundingClientRect().width : 0,
        scrollWidth: node.scrollWidth,
      };
    });
    // 内容没被压缩到容器宽以内（否则标签会被压窄进而折行），溢出时才出现横向滚动。
    expect(widest.innerWidth).toBeGreaterThanOrEqual(widest.clientWidth - 1);
    expect(widest.scrollWidth).toBeGreaterThanOrEqual(widest.clientWidth);
  }

  // 在编辑弹窗中删除整个角色及其全部评价关系：点右上角垃圾桶，二次确认后确认。
  // 断言二次确认展示的是译文（中文文案本身，而非 i18n key），再断言角色列整体消失。
  async deleteRoleInEditModal(roleName: string) {
    const dialog = this.relationDialog();
    const deleteButton = dialog.getByTestId('eval360-edit-delete-role-btn');
    await expect(deleteButton).toBeVisible();
    await deleteButton.click();

    const confirm = this.page.locator('.ant-modal-confirm');
    await expect(confirm).toBeVisible();
    // 标题含被删角色名，正文点明「所有被评估人下的全部评价人一并删除」这一影响范围。
    await expect(confirm).toContainText(`确认删除角色「${roleName}」`);
    await expect(confirm).toContainText('该角色在所有被评估人下的全部评价人都会一并删除');
    await confirm.locator('.ant-modal-confirm-btns .ant-btn-primary').click();

    await expect(dialog).toBeHidden();
    await expect(this.relationMatrixHeaders().filter({ hasText: roleName })).toHaveCount(0);
  }

  // 关系明细表的表头（角色列动态生成，用于断言角色列的出现/消失）。
  relationMatrixHeaders(): Locator {
    return this.page.getByTestId('eval360-relation-matrix').locator('thead th');
  }

  // ---- 发布与报告 ----

  get publishButton(): Locator {
    return this.page.getByTestId('eval360-questionnaire-publish');
  }

  // 发布/取消发布：点顶部发放按钮后在二次确认弹窗点「确定」。
  async publish() {
    await this.publishButton.click();
    const confirm = this.page.locator('.ant-modal-confirm');
    await expect(confirm).toBeVisible();
    await confirm.locator('.ant-modal-confirm-btns .ant-btn-primary').click();
    await expect(confirm).toBeHidden();
  }

  async exportByAnswer(): Promise<Buffer> {
    return this.downloadBlob('eval360-export-by-answer');
  }

  async exportByScore(): Promise<Buffer> {
    return this.downloadBlob('eval360-export-by-score');
  }

  private async downloadBlob(testId: string): Promise<Buffer> {
    const download = this.page.waitForEvent('download');
    await this.page.getByTestId(testId).click();
    const artifact = await download;
    const stream = await artifact.createReadStream();
    const chunks: Buffer[] = [];
    for await (const chunk of stream) {
      chunks.push(Buffer.from(chunk));
    }
    return Buffer.concat(chunks);
  }

  // confirmDialog 点击当前可见确认弹窗的主按钮。
  async confirmDialog() {
    const modal = this.page.locator('.ant-modal-confirm, .ant-modal').filter({ visible: true }).first();
    await expect(modal).toBeVisible();
    await modal.getByRole('button', { name: confirmButtonPattern }).click();
  }
}
