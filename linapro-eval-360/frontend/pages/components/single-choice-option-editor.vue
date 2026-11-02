<script setup lang="ts">
// 单选选项编辑器：编辑态逐行录入「文案 + 分值 + 允许评语」，可增删行；只读态展示选项清单。
// 组件直接按引用改传入的 options 数组（不 emit 回写）：题目草稿是原地变更模型，
// 父级靠同一对象引用做脏检查（snapshot 比对），复制副本再回传会打断该机制。
import { Checkbox, Input, InputNumber, Radio } from 'ant-design-vue';

import { $t } from '#/locales';
import type { QuestionOptionDTO } from '../eval-client';

defineProps<{
  options: QuestionOptionDTO[];
  // 只读态：以被评估人视角展示选项（禁用态单选钮），不可编辑。
  readonly?: boolean;
}>();

function addOption(options: QuestionOptionDTO[]) {
  options.push({
    id: 0,
    label: '',
    score: '0',
    allowText: 0,
    sort: options.length,
  });
}

function removeOption(options: QuestionOptionDTO[], idx: number) {
  options.splice(idx, 1);
}
</script>

<template>
  <!-- 只读态：每个选项前置禁用态单选钮。 -->
  <div v-if="readonly" class="flex flex-col gap-1">
    <span class="text-xs text-gray-400">
      {{ $t('plugin.linapro-eval-360.page.questionnaire.fields.options') }}
    </span>
    <div v-for="(o, i) in options" :key="i" class="flex items-center gap-2">
      <Radio disabled />
      <span class="text-sm">
        {{ o.label }}
        <span class="text-xs text-gray-400">（{{ o.score }} 分）</span>
      </span>
    </div>
  </div>

  <!-- 编辑态：逐行录入。 -->
  <div v-else class="flex flex-col gap-2">
    <div
      v-for="(o, i) in options"
      :key="i"
      class="flex items-center gap-2"
    >
      <Input
        v-model:value="o.label"
        class="flex-1"
        :placeholder="$t('plugin.linapro-eval-360.page.questionnaire.optionLabel')"
      />
      <InputNumber
        v-model:value="o.score"
        :placeholder="$t('plugin.linapro-eval-360.page.questionnaire.optionScore')"
        style="width: 100px"
      />
      <Checkbox
        :checked="o.allowText === 1"
        @change="(e: any) => (o.allowText = e.target.checked ? 1 : 0)"
      >
        {{ $t('plugin.linapro-eval-360.page.questionnaire.allowText') }}
      </Checkbox>
      <a-button type="link" danger size="small" @click="removeOption(options, i)">
        ×
      </a-button>
    </div>
    <a-button
      type="dashed"
      size="small"
      data-testid="eval360-question-add-option"
      @click="addOption(options)"
    >
      {{ $t('plugin.linapro-eval-360.page.questionnaire.addOption') }}
    </a-button>
  </div>
</template>
