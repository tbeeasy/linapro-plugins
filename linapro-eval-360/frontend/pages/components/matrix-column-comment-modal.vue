<script setup lang="ts">
// 评语配置弹窗：同一弹窗服务两个子项——「提示语」与「预设填空」。
// 组件只做「单值 ↔ 文本」的转换，不感知具体是哪张题目卡片的哪一列：
// 打开时按 mode 把传入的值铺开，确定时 emit 回该列的草稿。
import { computed, ref, watch } from 'vue';

import { Checkbox, Input, Modal, Textarea } from 'ant-design-vue';

import { $t } from '#/locales';

const props = defineProps<{
  open: boolean;
  // placeholder=编辑提示语；presets=编辑预设填空候选。
  mode: 'placeholder' | 'presets';
  // 当前列文案：仅用于弹窗标题，便于辨认正在配置哪一列。
  columnLabel: string;
  // mode=placeholder 时为提示语文案；mode=presets 时为候选列表。
  placeholder: string;
  presets: string[];
  allowCustom: number;
}>();

const emit = defineEmits<{
  'update:open': [value: boolean];
  // 提交结果：placeholder 模式只用 placeholder；presets 模式只用 presets/allowCustom。
  submit: [payload: { placeholder: string; presets: string[]; allowCustom: number }];
}>();

// 提示语输入（单行）。
const placeholderText = ref('');
// 预设候选输入（多行，一行一个）与「允许自定义」。
const presetsText = ref('');
const allowCustom = ref(false);

const title = computed(() => {
  const suffix = props.mode === 'placeholder'
    ? $t('plugin.linapro-eval-360.page.questionnaire.commentPlaceholder')
    : $t('plugin.linapro-eval-360.page.questionnaire.commentPresets');
  return `${suffix} · ${props.columnLabel}`;
});

watch(
  () => props.open,
  (open) => {
    if (!open) return;
    placeholderText.value = props.placeholder ?? '';
    presetsText.value = (props.presets ?? []).join('\n');
    allowCustom.value = props.allowCustom === 1;
  },
);

function submit() {
  if (props.mode === 'placeholder') {
    emit('submit', {
      placeholder: placeholderText.value.trim(),
      presets: props.presets ?? [],
      allowCustom: props.allowCustom,
    });
    emit('update:open', false);
    return;
  }
  // 预设按行拆分：去首尾空白、丢弃空行、保序去重（与后端落库口径一致）。
  const presets = [...new Set(
    presetsText.value
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line.length > 0),
  )];
  emit('submit', {
    placeholder: props.placeholder ?? '',
    presets,
    // 没有预设时「允许自定义」无意义，一并归零，避免留下不可达的配置。
    allowCustom: presets.length > 0 && allowCustom.value ? 1 : 0,
  });
  emit('update:open', false);
}

// 清空：把当前模式的值置空，便于取消某项配置（取消单项 = 子项不再标蓝）。
function clearCurrent() {
  if (props.mode === 'placeholder') {
    placeholderText.value = '';
    return;
  }
  presetsText.value = '';
  allowCustom.value = false;
}
</script>

<template>
  <Modal
    :open="open"
    :title="title"
    :width="mode === 'presets' ? 560 : 480"
    centered
    destroy-on-close
    data-testid="eval360-matrix-column-comment-modal"
    @ok="submit"
    @cancel="emit('update:open', false)"
  >
    <!-- 提示语：评语输入框的占位文案，与预设填空互不冲突、可同时配置。 -->
    <template v-if="mode === 'placeholder'">
      <Input
        v-model:value="placeholderText"
        :maxlength="255"
        show-count
        :placeholder="$t('plugin.linapro-eval-360.page.questionnaire.commentPlaceholderHint')"
        data-testid="eval360-matrix-comment-placeholder"
      />
      <p class="mt-2 text-xs text-gray-400">
        {{ $t('plugin.linapro-eval-360.page.questionnaire.commentPlaceholderTip') }}
      </p>
    </template>

    <!-- 预设填空：一行一个候选；勾选「允许自定义」后员工端可自由输入。 -->
    <template v-else>
      <Textarea
        v-model:value="presetsText"
        :rows="6"
        :placeholder="$t('plugin.linapro-eval-360.page.questionnaire.commentPresetsHint')"
        data-testid="eval360-matrix-comment-presets"
      />
      <div class="mt-3">
        <Checkbox
          v-model:checked="allowCustom"
          data-testid="eval360-matrix-comment-allow-custom"
        >
          {{ $t('plugin.linapro-eval-360.page.questionnaire.commentAllowCustom') }}
        </Checkbox>
      </div>
      <p class="mt-2 text-xs text-gray-400">
        {{ allowCustom
          ? $t('plugin.linapro-eval-360.page.questionnaire.commentAllowCustomTip')
          : $t('plugin.linapro-eval-360.page.questionnaire.commentPresetOnlyTip') }}
      </p>
    </template>

    <template #footer>
      <a-button data-testid="eval360-matrix-comment-clear" @click="clearCurrent">
        {{ $t('plugin.linapro-eval-360.page.questionnaire.clearCommentConfig') }}
      </a-button>
      <a-button @click="emit('update:open', false)">
        {{ $t('plugin.linapro-eval-360.page.common.cancel') }}
      </a-button>
      <a-button type="primary" data-testid="eval360-matrix-comment-save" @click="submit">
        {{ $t('plugin.linapro-eval-360.page.common.save') }}
      </a-button>
    </template>
  </Modal>
</template>
