<script setup lang="ts">
// 矩阵列（评价项）编辑弹窗：左=文案、右=分值，两个大文本框按行号一一对应，
// 中间竖线分割、整体可纵向拖拽缩放（参考 Moka「编辑选项」）。
// 组件只做「列数组 ↔ 双文本」的转换，不感知具体是哪张题目卡片：
// 打开时把传入的列铺成文本，确定时按行配对后 emit 回去，由父级写回草稿。
import { ref, watch } from 'vue';

import { message, Modal } from 'ant-design-vue';

import { $t } from '#/locales';

const props = defineProps<{
  open: boolean;
  // 待编辑的列；id 用于让后端识别「编辑既有列」，新增列 id=0。
  // 列上还带评语配置：批量编辑只改文案/分值，评语配置按行号原样保留，避免被清空。
  columns: {
    id: number;
    label: string;
    score: string;
    sort: number;
    allowComment: number;
    commentRequired: number;
    commentPlaceholder: string;
    commentPresets: string[];
    commentAllowCustom: number;
  }[];
}>();
const emit = defineEmits<{
  'update:open': [value: boolean];
  submit: [
    columns: {
      id: number;
      label: string;
      score: string;
      sort: number;
      allowComment: number;
      commentRequired: number;
      commentPlaceholder: string;
      commentPresets: string[];
      commentAllowCustom: number;
    }[],
  ];
}>();

// 左右两栏的文本，按行号一一对应；columnIds 记录每行对应的原列 id（按行号），
// 以便确定时把改动写回既有列而非全当新增。
const labelsText = ref('');
const scoresText = ref('');
let columnIds: number[] = [];

watch(
  () => props.open,
  (open) => {
    if (!open) return;
    labelsText.value = props.columns.map((c) => c.label).join('\n');
    scoresText.value = props.columns.map((c) => c.score).join('\n');
    columnIds = props.columns.map((c) => c.id);
  },
);

function submit() {
  // 左右两框按行号配对；丢弃文案为空的行，保留原列 id（缺省 0=新增）。
  // 评语配置按行号沿用原列（本弹窗不编辑评语，只负责不丢配置）；行被删掉时整列一并删除。
  const labels = labelsText.value.split('\n');
  const scores = scoresText.value.split('\n');
  const cols = labels
    .map((label, i) => {
      const source = props.columns[i];
      return {
        id: columnIds[i] ?? 0,
        label: label.trim(),
        // 分值留空代表不计分（后端按 0 处理）。
        score: (scores[i] ?? '').trim() || '0',
        sort: i,
        allowComment: source?.allowComment ?? 0,
        commentRequired: source?.commentRequired ?? 0,
        commentPlaceholder: source?.commentPlaceholder ?? '',
        commentPresets: source?.commentPresets ?? [],
        commentAllowCustom: source?.commentAllowCustom ?? 0,
      };
    })
    .filter((c) => c.label.length > 0);
  if (cols.length === 0) {
    message.warning($t('plugin.linapro-eval-360.page.questionnaire.matrixRequired'));
    return;
  }
  emit('submit', cols.map((c, i) => ({ ...c, sort: i })));
  emit('update:open', false);
}
</script>

<template>
  <!-- 整块为一个输入框：拖拽右下角手柄只改这个框的高度，左右两栏由 flex 撑满、始终同步。 -->
  <Modal
    :open="open"
    :title="$t('plugin.linapro-eval-360.page.questionnaire.editColumns')"
    :width="860"
    centered
    destroy-on-close
    data-testid="eval360-matrix-column-modal"
    @ok="submit"
    @cancel="emit('update:open', false)"
  >
    <div class="eval360-split-box" data-testid="eval360-matrix-column-box">
      <div class="eval360-split-head">
        <span>{{ $t('plugin.linapro-eval-360.page.questionnaire.colLabel') }}</span>
        <span>{{ $t('plugin.linapro-eval-360.page.questionnaire.optionScoreHint') }}</span>
      </div>
      <div class="eval360-split-body">
        <textarea
          v-model="labelsText"
          class="eval360-split-pane"
          :placeholder="$t('plugin.linapro-eval-360.page.questionnaire.colLabelHint')"
          data-testid="eval360-matrix-column-labels"
        ></textarea>
        <textarea
          v-model="scoresText"
          class="eval360-split-pane"
          :placeholder="$t('plugin.linapro-eval-360.page.questionnaire.colScoreHint')"
          data-testid="eval360-matrix-column-scores"
        ></textarea>
      </div>
    </div>
  </Modal>
</template>

<style scoped>
/* 评价项编辑弹窗：整块作为一个可纵向拖拽缩放的输入框（resize: vertical 的手柄只挂在整块上，
   内部两个 textarea 完全铺满、随整块高度一起变化，不会各自独立拖动）。 */
.eval360-split-box {
  display: flex;
  flex-direction: column;
  height: 360px;
  min-height: 160px;
  max-height: 70vh;
  overflow: hidden;
  resize: vertical;
  border: 1px solid var(--ant-color-border, #d9d9d9);
  border-radius: 6px;
}

/* 表头两栏等比，与下方两栏对齐。 */
.eval360-split-head {
  display: flex;
  flex: none;
  padding: 8px 12px;
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

/* 主体：左右两栏 + 中间竖线；撑满整块剩余高度。 */
.eval360-split-body {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
}
.eval360-split-pane {
  flex: 1 1 0;
  min-width: 0;
  padding: 8px 12px;
  font-size: 14px;
  line-height: 1.6;
  color: var(--ant-color-text, rgb(0 0 0 / 88%));
  resize: none; /* 关键：禁止单个框独立拖拽缩放 */
  background: transparent;
  border: 0;
  outline: none;
}
.eval360-split-pane + .eval360-split-pane {
  border-left: 1px solid var(--ant-color-border, #d9d9d9); /* 中间竖线 */
}
.eval360-split-pane::placeholder {
  color: var(--ant-color-text-placeholder, rgb(0 0 0 / 25%));
}
</style>
