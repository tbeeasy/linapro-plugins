<script setup lang="ts">
// 矩阵表：纯展示组件，被「题目编辑器（编辑态/只读态）」与「问卷预览」三处复用。
// 组件不持有任何状态、不感知草稿：列与行都以归一化后的形状传入，
// 由调用方决定数据从哪来（编辑态取草稿列、被评估人取评价关系）。
// 编辑态（editable=true）时每个评价项列头挂一个「⋮」操作栏：删除本列 / 允许评语 / 左移 / 右移。
// 面板本身不修改数据，一律 emit 给父级改草稿，保持「只改草稿、随题目保存落库」的口径。
import { ref } from 'vue';

import { IconifyIcon } from '@vben/icons';
import { Dropdown, Menu, MenuDivider, MenuItem, Radio } from 'ant-design-vue';

import { $t } from '#/locales';

interface ColumnView {
  id?: number;
  label: string;
  score: string;
  // 评语配置（只读态/预览态可缺省，缺省即视为不允许评语）。
  allowComment?: number;
  commentRequired?: number;
  commentPlaceholder?: string;
  commentPresets?: string[];
  commentAllowCustom?: number;
}

const props = withDefaults(
  defineProps<{
    // 列 = 评价项：第一行表头显示文案，第二行显示分值。
    columns: ColumnView[];
    // 行名：每个被评估人一行。空数组表示尚无评价关系，此时渲染占位行。
    rows: string[];
    // rows 为空时渲染的灰色占位行数。
    placeholderRows?: number;
    // rows 为空时表格下方的小字提示（调用方传入已翻译文案，空则不显示）。
    emptyHint?: string;
    // 是否显示列头操作栏（仅题目编辑器编辑态为 true）。
    editable?: boolean;
  }>(),
  { placeholderRows: 2, emptyHint: '', editable: false },
);

const emit = defineEmits<{
  // 删除本列（父级负责在草稿中移除并重排 sort）。
  deleteColumn: [index: number];
  // 左移/右移：dir=-1 左移，dir=1 右移。
  moveColumn: [index: number, dir: -1 | 1];
  // 切换「允许评语」总开关。
  toggleAllowComment: [index: number];
  // 切换「必填」（仅在允许评语开启时可用）。
  toggleCommentRequired: [index: number];
  // 打开「提示语」弹窗。
  editCommentPlaceholder: [index: number];
  // 打开「预设填空」弹窗。
  editCommentPresets: [index: number];
}>();

// 占位行：无被评估人时用灰字占位，保证表头与列宽在未导入关系时依然可见、可校验。
const placeholders = Array.from({ length: props.placeholderRows }, (_, i) => i);

// 分值行展示：0 分（含留空保存后的 0、后端返回的 "0.00"）不显示，单元格留空。
function displayScore(score: string): string {
  return Number(score) === 0 ? '' : score;
}

// 子项是否开启：范围从 index 开始的配置项决定子项可用性。
function allowComment(c: ColumnView): boolean {
  return (c.allowComment ?? 0) === 1;
}

// 菜单选中项：用于把已生效的操作标记为蓝色。
// 未开启允许评语时，三个子项只是不可见占位，不计入选中项。
function selectedKeys(c: ColumnView): string[] {
  if (!allowComment(c)) return [];
  const keys = ['allow'];
  if ((c.commentRequired ?? 0) === 1) keys.push('required');
  if ((c.commentPlaceholder ?? '').trim()) keys.push('placeholder');
  if ((c.commentPresets ?? []).length > 0) keys.push('presets');
  return keys;
}

// 首末列不可再移动：对应菜单项禁用。
function canMoveLeft(index: number): boolean {
  return index > 0;
}

function canMoveRight(index: number): boolean {
  return index < props.columns.length - 1;
}

function commentRequired(c: ColumnView): boolean {
  return allowComment(c) && (c.commentRequired ?? 0) === 1;
}

// 当前展开操作栏的列下标（受控）：antd 下拉菜单默认点任一项即收起，
// 「允许评语 / 必填」是开关，需连续点选多个子项，故开关类操作保持展开，
// 删除、移动与打开弹窗类操作才主动收起。
const openIndex = ref<null | number>(null);

function onOpenChange(index: number, open: boolean) {
  openIndex.value = open ? index : null;
}

// closeThen 先收起操作栏再执行动作（删除/移动后列下标会变，弹窗会盖住菜单）。
function closeThen(action: () => void) {
  openIndex.value = null;
  action();
}
</script>

<template>
  <div class="overflow-x-auto">
    <table class="w-full border-collapse text-sm">
      <thead>
        <tr>
          <th class="border border-border p-2 text-left" rowspan="2">
            {{ $t('plugin.linapro-eval-360.page.questionnaire.matrixEvaluateeHeader') }}
          </th>
          <th
            v-for="(c, i) in columns"
            :key="`h1-${i}`"
            class="relative border border-border p-2 text-center font-medium"
            :class="{ 'px-7': editable }"
          >
            <!-- 编辑态左右对称留出 px-7：右侧放操作栏，左侧对称留白保持文案居中且不与图标重叠。 -->
            <div>{{ c.label }}</div>
            <!-- 评语标记：开启允许评语后在列说明下一行显示 [评语可为空] / [评语必填]。
                 两种文案叠放在同一网格单元、只显示其一，列宽恒为两者较宽者；
                 编辑态未开启时整行 invisible 占位：开关「允许评语 / 必填」时列宽与表头高度都不变，操作栏不跳动。 -->
            <div v-if="editable || allowComment(c)">
              <div class="inline-grid text-xs font-normal text-gray-400">
                <span
                  class="col-start-1 row-start-1"
                  :class="{ invisible: !allowComment(c) || commentRequired(c) }"
                  :data-testid="`eval360-matrix-comment-optional-${i}`"
                >
                  {{ $t('plugin.linapro-eval-360.page.questionnaire.commentOptionalTag') }}
                </span>
                <span
                  class="col-start-1 row-start-1"
                  :class="{ invisible: !commentRequired(c) }"
                  :data-testid="`eval360-matrix-comment-required-${i}`"
                >
                  {{ $t('plugin.linapro-eval-360.page.questionnaire.commentRequiredTag') }}
                </span>
              </div>
            </div>
            <!-- 列头操作栏：仅编辑态出现，避免预览态出现不可用入口；贴单元格结束侧边框（LTR 为右、RTL 为左）垂直居中，不跟随文案。 -->
            <div v-if="editable" class="absolute inset-y-0 end-0.5 flex items-center">
              <Dropdown
                :open="openIndex === i"
                :trigger="['click']"
                overlay-class-name="eval360-matrix-col-menu"
                placement="bottomRight"
                @open-change="(open: boolean) => onOpenChange(i, open)"
              >
                <button
                  type="button"
                  class="inline-flex cursor-pointer items-center justify-center rounded border-0 bg-transparent p-0.5 text-gray-600 hover:bg-gray-100 hover:text-primary"
                  :data-testid="`eval360-matrix-col-menu-${i}`"
                  :title="$t('plugin.linapro-eval-360.page.questionnaire.columnActions')"
                >
                  <IconifyIcon icon="lucide:ellipsis-vertical" class="size-[18px]" />
                </button>
                <template #overlay>
                  <Menu :selectable="true" :selected-keys="selectedKeys(c)">
                    <MenuItem key="delete" danger :data-testid="`eval360-matrix-col-delete-${i}`"
                      @click="closeThen(() => emit('deleteColumn', i))">
                      {{ $t('plugin.linapro-eval-360.page.common.delete') }}
                    </MenuItem>
                    <MenuDivider />
                    <!-- 允许评语本身即总开关：点击变蓝=开启，再次点击=关闭；开关不收起菜单。 -->
                    <MenuItem key="allow" :data-testid="`eval360-matrix-col-allow-${i}`"
                      @click="emit('toggleAllowComment', i)">
                      {{ $t('plugin.linapro-eval-360.page.questionnaire.allowComment') }}
                    </MenuItem>
                    <!-- 三个子项可同时生效，开启允许评语后才展开；缩进 + 左侧竖线表示从属于「允许评语」。
                         未开启时仍渲染为「零高度、不可见、禁用」的占位：参与菜单宽度计算但不占高度、不可点，
                         使菜单一打开就是展开后的宽度，开关「允许评语」时菜单不再变宽（宽度随中英文案自适应）。 -->
                    <MenuItem key="required" class="eval360-col-sub-item"
                      :class="{ 'eval360-col-sub-ghost': !allowComment(c) }"
                      :disabled="!allowComment(c)"
                      :data-testid="`eval360-matrix-col-required-${i}`"
                      @click="emit('toggleCommentRequired', i)">
                      {{ $t('plugin.linapro-eval-360.page.questionnaire.commentRequired') }}
                    </MenuItem>
                    <MenuItem key="placeholder" class="eval360-col-sub-item"
                      :class="{ 'eval360-col-sub-ghost': !allowComment(c) }"
                      :disabled="!allowComment(c)"
                      :data-testid="`eval360-matrix-col-placeholder-${i}`"
                      @click="closeThen(() => emit('editCommentPlaceholder', i))">
                      {{ $t('plugin.linapro-eval-360.page.questionnaire.commentPlaceholder') }}
                    </MenuItem>
                    <MenuItem key="presets" class="eval360-col-sub-item"
                      :class="{ 'eval360-col-sub-ghost': !allowComment(c) }"
                      :disabled="!allowComment(c)"
                      :data-testid="`eval360-matrix-col-presets-${i}`"
                      @click="closeThen(() => emit('editCommentPresets', i))">
                      {{ $t('plugin.linapro-eval-360.page.questionnaire.commentPresets') }}
                    </MenuItem>
                    <MenuDivider />
                    <MenuItem key="left" :disabled="!canMoveLeft(i)"
                      :data-testid="`eval360-matrix-col-left-${i}`"
                      @click="closeThen(() => emit('moveColumn', i, -1))">
                      {{ $t('plugin.linapro-eval-360.page.questionnaire.moveLeft') }}
                    </MenuItem>
                    <MenuItem key="right" :disabled="!canMoveRight(i)"
                      :data-testid="`eval360-matrix-col-right-${i}`"
                      @click="closeThen(() => emit('moveColumn', i, 1))">
                      {{ $t('plugin.linapro-eval-360.page.questionnaire.moveRight') }}
                    </MenuItem>
                  </Menu>
                </template>
              </Dropdown>
            </div>
          </th>
        </tr>
        <tr>
          <th
            v-for="(c, i) in columns"
            :key="`h2-${i}`"
            class="border border-border p-1 text-center text-xs text-gray-400"
            data-testid="eval360-matrix-score-cell"
          >
            {{ displayScore(c.score) }}
          </th>
        </tr>
      </thead>
      <tbody>
        <!-- 有被评估人：一人一行，单元格为禁用态单选钮（仅示意结构）。 -->
        <tr v-for="(row, ri) in rows" :key="`r-${ri}`">
          <td class="border border-border p-2">{{ row }}</td>
          <td
            v-for="(_c, i) in columns"
            :key="`c-${ri}-${i}`"
            class="border border-border p-2 text-center"
          >
            <Radio disabled />
          </td>
        </tr>
        <!-- 无被评估人：渲染灰色占位行，行名统一显示表头名，避免整张表消失导致无法配置列。 -->
        <template v-if="rows.length === 0">
          <tr v-for="p in placeholders" :key="`p-${p}`">
            <td class="border border-border p-2 text-gray-400">
              {{ $t('plugin.linapro-eval-360.page.questionnaire.matrixEvaluateeHeader') }}
            </td>
            <td
              v-for="(_c, i) in columns"
              :key="`pc-${p}-${i}`"
              class="border border-border p-2 text-center"
            >
              <Radio disabled />
            </td>
          </tr>
        </template>
      </tbody>
    </table>
    <p v-if="rows.length === 0 && emptyHint" class="mt-1 text-xs text-gray-400">
      {{ emptyHint }}
    </p>
  </div>
</template>

<style>
/* 列头操作栏的「允许评语」子项：缩进并加左侧竖线，表明从属于「允许评语」。
   下拉菜单经 teleport 渲染到 body，scoped 样式够不到，故用全局类并以 overlay 类名限定范围；
   选择器叠加 .ant-dropdown / .ant-dropdown-menu 以压过 antd 的菜单项内边距（同为三级类选择器时按注入顺序会输）。 */
.eval360-matrix-col-menu.ant-dropdown
  .ant-dropdown-menu
  .ant-dropdown-menu-item.eval360-col-sub-item {
  position: relative;
  padding-inline-start: 32px;
  font-size: 13px;
}

.eval360-matrix-col-menu.ant-dropdown
  .ant-dropdown-menu
  .ant-dropdown-menu-item.eval360-col-sub-item::before {
  position: absolute;
  top: 0;
  bottom: 0;
  inset-inline-start: 18px;
  width: 1px;
  content: '';
  background: hsl(var(--border));
}

/* 未开启允许评语时的占位子项：保留宽度、去掉高度与可见性。 */
.eval360-matrix-col-menu.ant-dropdown
  .ant-dropdown-menu
  .ant-dropdown-menu-item.eval360-col-sub-ghost {
  height: 0;
  min-height: 0;
  padding-top: 0;
  padding-bottom: 0;
  overflow: hidden;
  line-height: 0;
  visibility: hidden;
}
</style>
