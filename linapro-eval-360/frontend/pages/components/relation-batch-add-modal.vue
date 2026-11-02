<script setup lang="ts">
// 新增评价关系弹窗：按「组」录入，一组 = 一个被评估人 + 其多名评价人。
// 每组五栏并排（均为 TextArea）：被评估人姓名/职务/部门每组只填一行（提交时校验，
// 职务/部门可选），评价人姓名/角色为多行文本，按行号一一对应、一行一个评价人，方便批量粘贴。
// 不录入工号：后端按在职员工姓名匹配，任一姓名在职重名或找不到时整批拒绝（不写入任何数据）
// 并点名问题姓名，故全部组收集完后**一次**提交。
// 「添加」新增一整组。角色为自由文本，后端按名称 upsert，不依赖预先配置的角色。
import { ref } from 'vue';

import { useVbenModal } from '@vben/common-ui';

import { Button, Input, message } from 'ant-design-vue';

import { $t } from '#/locales';
import { relationAdd, type AddRelationItem } from '../eval-client';

const { TextArea } = Input;

const emit = defineEmits<{ reload: [] }>();

// 一组待录入的数据。
interface Group {
  key: number;
  evaluateeName: string;
  // 被评估人职务/部门：可选，每组至多一行。
  evaluateeJobTitle: string;
  evaluateeDepartment: string;
  // 右侧两栏多行文本，按行号一一对应。
  evaluatorNames: string;
  roleNames: string;
}

const questionnaireId = ref<number>(0);
const groups = ref<Group[]>([]);

// 组 key 自增（脚本环境不能用 Date.now/随机数，用普通计数器）。
let keySeq = 0;

function makeGroup(): Group {
  keySeq += 1;
  return {
    key: keySeq,
    evaluateeName: '',
    evaluateeJobTitle: '',
    evaluateeDepartment: '',
    evaluatorNames: '',
    roleNames: '',
  };
}

function addGroup() {
  groups.value.push(makeGroup());
}

// 删除某一组（误添加时可移除）；至少保留一组。
function removeGroup(key: number) {
  if (groups.value.length <= 1) return;
  groups.value = groups.value.filter((g) => g.key !== key);
}

// 统计某栏文本中已填写的行数（非空行），用于表头实时回显。
function lineCount(text: string): number {
  return nonEmptyLines(text).length;
}

// 取某栏文本中的非空行（已去首尾空白）。
function nonEmptyLines(text: string): string[] {
  return text
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean);
}

const [BasicModal, modalApi] = useVbenModal({
  onOpenChange(open) {
    if (!open) return;
    const data = modalApi.getData<{
      questionnaireId: number;
      // 从「被评估人所在行」打开时预填其姓名/职务/部门，省去手动重复输入。
      evaluateeName?: string;
      evaluateeJobTitle?: string;
      evaluateeDepartment?: string;
    }>();
    questionnaireId.value = data?.questionnaireId ?? 0;
    keySeq = 0;
    const first = makeGroup();
    if (data?.evaluateeName) first.evaluateeName = data.evaluateeName;
    if (data?.evaluateeJobTitle) first.evaluateeJobTitle = data.evaluateeJobTitle;
    if (data?.evaluateeDepartment) {
      first.evaluateeDepartment = data.evaluateeDepartment;
    }
    groups.value = [first];
  },
  async onConfirm() {
    const tasks: AddRelationItem[] = [];

    for (let gi = 0; gi < groups.value.length; gi++) {
      const g = groups.value[gi]!;
      // 被评估人三栏与评价人同为多行文本，但每组只应填一行，取非空行校验。
      const eeNames = nonEmptyLines(g.evaluateeName);
      const eeJobTitles = nonEmptyLines(g.evaluateeJobTitle);
      const eeDepartments = nonEmptyLines(g.evaluateeDepartment);
      const er = g.evaluatorNames.split('\n');
      const rn = g.roleNames.split('\n');
      const lineCount = Math.max(er.length, rn.length);

      // 先把右侧两栏按行号配对，保留「至少填了一项」的行（部分填写后面再报错）。
      const lines: { row: number; name: string; role: string; filled: number }[] = [];
      for (let i = 0; i < lineCount; i++) {
        const name = (er[i] ?? '').trim();
        const role = (rn[i] ?? '').trim();
        const filled = [name, role].filter(Boolean).length;
        if (filled === 0) continue;
        lines.push({ row: i + 1, name, role, filled });
      }

      // 整组皆空：视为未填，跳过。
      if (
        eeNames.length === 0 &&
        eeJobTitles.length === 0 &&
        eeDepartments.length === 0 &&
        lines.length === 0
      ) {
        continue;
      }

      if (eeNames.length === 0) {
        message.warning(
          $t('plugin.linapro-eval-360.page.relation.batchEvaluateeRequired', {
            group: gi + 1,
          }),
        );
        return;
      }
      // 被评估人每组只能一行。
      if (
        eeNames.length > 1 ||
        eeJobTitles.length > 1 ||
        eeDepartments.length > 1
      ) {
        message.warning(
          $t('plugin.linapro-eval-360.page.relation.batchEvaluateeSingleLine', {
            group: gi + 1,
          }),
        );
        return;
      }
      const name = eeNames[0]!;
      // 职务/部门可选：留空时后端保留已有被评估人的原值。
      const jobTitle = eeJobTitles[0] ?? '';
      const department = eeDepartments[0] ?? '';

      if (lines.length === 0) {
        message.warning(
          $t('plugin.linapro-eval-360.page.relation.batchEvaluatorEmpty', {
            group: gi + 1,
          }),
        );
        return;
      }
      for (const ln of lines) {
        // 部分填写：姓名与角色必须齐全。
        if (ln.filled < 2) {
          message.warning(
            $t(
              'plugin.linapro-eval-360.page.relation.batchEvaluatorIncomplete',
              { group: gi + 1, row: ln.row },
            ),
          );
          return;
        }
        tasks.push({
          evaluateeName: name,
          evaluateeJobTitle: jobTitle,
          evaluateeDepartment: department,
          evaluatorName: ln.name,
          roleName: ln.role,
        });
      }
    }

    if (tasks.length === 0) {
      message.warning($t('plugin.linapro-eval-360.page.relation.batchEmpty'));
      return;
    }

    modalApi.setState({ confirmLoading: true });
    try {
      // 一次提交：任一姓名重名/找不到时后端整批拒绝，错误提示（点名问题姓名）由请求拦截器展示，弹窗保持打开便于修改。
      await relationAdd({ questionnaireId: questionnaireId.value, items: tasks });
      message.success(
        $t('plugin.linapro-eval-360.page.relation.batchSuccess', {
          count: tasks.length,
        }),
      );
      emit('reload');
      modalApi.close();
    } finally {
      modalApi.setState({ confirmLoading: false });
    }
  },
});
</script>

<template>
  <BasicModal
    :title="$t('plugin.linapro-eval-360.page.relation.addTitle')"
    class="eval360-batch-add-modal"
  >
    <div class="flex flex-col gap-3">
      <div class="text-sm text-gray-500">
        {{ $t('plugin.linapro-eval-360.page.relation.batchGridHint') }}
      </div>

      <!-- 每组一块：五栏并排，均为 TextArea。左三栏=被评估人姓名/职务/部门（每组只填一行，
           提交时校验；职务/部门可选），右两栏=评价人姓名/角色（多行文本，按行号一一对应）。 -->
      <div
        v-for="g in groups"
        :key="g.key"
        class="eval360-group-row"
      >
        <div
          class="eval360-split-box"
          data-testid="eval360-batch-group"
        >
          <div class="eval360-split-head">
          <span>
            {{ $t('plugin.linapro-eval-360.page.relation.evaluateeName') }}
            <i class="eval360-split-count">{{ lineCount(g.evaluateeName) }}</i>
          </span>
          <span>
            {{ $t('plugin.linapro-eval-360.page.relation.evaluateeJobTitle') }}
            <i class="eval360-split-count">{{ lineCount(g.evaluateeJobTitle) }}</i>
          </span>
          <span>
            {{ $t('plugin.linapro-eval-360.page.relation.evaluateeDepartment') }}
            <i class="eval360-split-count">{{ lineCount(g.evaluateeDepartment) }}</i>
          </span>
          <span>
            {{ $t('plugin.linapro-eval-360.page.relation.evaluatorName') }}
            <i class="eval360-split-count">{{ lineCount(g.evaluatorNames) }}</i>
          </span>
          <span>
            {{ $t('plugin.linapro-eval-360.page.relation.roleName') }}
            <i class="eval360-split-count">{{ lineCount(g.roleNames) }}</i>
          </span>
        </div>
        <div class="eval360-split-body">
          <!-- 被评估人：与评价人同为多行输入框，但每组只应填一行，提交时校验。 -->
          <div class="eval360-split-pane">
            <TextArea
              v-model:value="g.evaluateeName"
              class="eval360-ta"
              :bordered="false"
              data-testid="eval360-batch-evaluatee-name"
              :placeholder="$t('plugin.linapro-eval-360.page.relation.evaluateeName')"
            />
          </div>
          <!-- 被评估人职务/部门：可选，每组至多一行。 -->
          <div class="eval360-split-pane">
            <TextArea
              v-model:value="g.evaluateeJobTitle"
              class="eval360-ta"
              :bordered="false"
              data-testid="eval360-batch-evaluatee-job-title"
              :placeholder="$t('plugin.linapro-eval-360.page.relation.optionalPlaceholder')"
            />
          </div>
          <div class="eval360-split-pane">
            <TextArea
              v-model:value="g.evaluateeDepartment"
              class="eval360-ta"
              :bordered="false"
              data-testid="eval360-batch-evaluatee-department"
              :placeholder="$t('plugin.linapro-eval-360.page.relation.optionalPlaceholder')"
            />
          </div>
          <!-- 评价人两栏：多行文本，按行号一一对应。 -->
          <div class="eval360-split-pane">
            <TextArea
              v-model:value="g.evaluatorNames"
              class="eval360-ta"
              :bordered="false"
              data-testid="eval360-batch-evaluator-names"
              :placeholder="$t('plugin.linapro-eval-360.page.relation.evaluatorName')"
            />
          </div>
          <div class="eval360-split-pane">
            <TextArea
              v-model:value="g.roleNames"
              class="eval360-ta"
              :bordered="false"
              data-testid="eval360-batch-role-names"
              :placeholder="$t('plugin.linapro-eval-360.page.relation.roleNamePlaceholder')"
            />
          </div>
        </div>
        </div>
        <Button
          type="text"
          danger
          size="small"
          class="eval360-group-remove"
          :class="{ 'eval360-group-remove--hidden': groups.length <= 1 }"
          data-testid="eval360-batch-remove-group"
          @click="removeGroup(g.key)"
        >
          {{ $t('plugin.linapro-eval-360.page.relation.batchRemoveGroup') }}
        </Button>
      </div>

      <Button type="dashed" block data-testid="eval360-batch-add-group" @click="addGroup">
        + {{ $t('plugin.linapro-eval-360.page.relation.batchAddGroup') }}
      </Button>
    </div>
  </BasicModal>
</template>

<!-- 弹窗宽度不用 Tailwind 任意值类（w-[1100px]）：该类只出现在插件里，打包产物的 CSS 若没生成它，
     弹窗会退化为 fixed + inset-x-0 + 宽度 auto，铺满整屏。这里用普通样式兜底。
     弹窗内容被传送到 body，scoped 样式命中不到，所以这一块不加 scoped；
     未分层的样式优先级高于 Tailwind 的 utilities 层，能覆盖框架默认的 w-[520px]。
     仅在 ≥768px 时生效，窄屏仍走框架的全屏模式。 -->
<style>
@media (min-width: 768px) {
  .eval360-batch-add-modal {
    width: 1100px;
    max-width: 95vw;
  }
}
</style>

<style scoped>
/* 每组外框：五栏并排的一个可纵向拖拽缩放的框，内部各栏铺满、按行号同步。
   高度相较旧版缩小（约 140px），因为可以添加多组数据。 */
.eval360-split-box {
  display: flex;
  flex-direction: column;
  height: 140px;
  min-height: 90px;
  max-height: 60vh;
  overflow: hidden;
  resize: vertical;
  border: 1px solid var(--ant-color-border, #d9d9d9);
  border-radius: 6px;
}

/* 表头五栏等比，与下方五栏对齐。 */
.eval360-split-head {
  display: flex;
  flex: none;
  padding: 6px 12px;
  font-size: 12px;
  color: var(--ant-color-text-secondary, rgb(0 0 0 / 45%));
  border-bottom: 1px solid var(--ant-color-border, #d9d9d9);
}
.eval360-split-head > span {
  flex: 1 1 0;
}
.eval360-split-head > span + span {
  padding-left: 24px;
}
/* 表头内的行数标记：紧跟栏名显示当前已填写行数。 */
.eval360-split-count {
  margin-left: 4px;
  font-style: normal;
  color: var(--ant-color-primary, #1677ff);
}
/* 每组一行：左侧为五栏方框，右侧为删除按钮（位于最后一个 TextArea 的右边）。 */
.eval360-group-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.eval360-group-row > .eval360-split-box {
  flex: 1 1 auto;
  min-width: 0;
}
/* 删除按钮：与方框顶部对齐，不参与方框内部的五栏等分。 */
.eval360-group-remove {
  flex: none;
  padding: 0 8px;
}
/* 只有一组时隐藏按钮但保留其占位宽度，避免方框宽度抖动。 */
.eval360-group-remove--hidden {
  visibility: hidden;
}

/* 主体：五栏 + 栏间竖线；撑满整块剩余高度。 */
.eval360-split-body {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
}
/* 每栏为一个 flex 容器，承载一个铺满的 Input.TextArea。 */
.eval360-split-pane {
  display: flex;
  flex: 1 1 0;
  min-width: 0;
}
.eval360-split-pane + .eval360-split-pane {
  border-left: 1px solid var(--ant-color-border, #d9d9d9); /* 栏间竖线 */
}
/* 无边框文本域铺满整栏、禁止单独拖拽缩放（整块统一拖拽）。 */
.eval360-ta {
  flex: 1 1 auto;
  min-height: 0;
}
.eval360-ta :deep(textarea),
.eval360-ta.ant-input {
  height: 100% !important;
  padding: 8px 12px;
  font-size: 14px;
  line-height: 1.6;
  resize: none;
}
</style>
