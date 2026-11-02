<script setup lang="ts">
// 问卷预览主体：以被评估人视角只读渲染问卷结构（维度→题目→作答控件）。
// 所有控件均 disabled，仅供查看不可作答；预览弹窗与评测问卷页内嵌预览共用本组件。
import { computed } from "vue";

import { Empty, Radio, RadioGroup, Textarea } from "ant-design-vue";

import { $t } from "#/locales";
import type { EvaluateeRow, QuestionnaireDetail } from "../eval-client";
import MatrixTable from "./matrix-table.vue";

const props = defineProps<{
  detail: QuestionnaireDetail;
  // 被评估人清单（来自评价关系）：矩阵题的「行」由此动态渲染。
  evaluatees: EvaluateeRow[];
}>();

// 全问卷题目总数：为空时展示占位。
const questionCount = computed(() =>
  props.detail.dimensions.reduce((sum, d) => sum + d.questions.length, 0),
);

// 题目序号跨维度连续：维度分组只是展示结构，题号须按整份问卷依次递增，
// 不能每个维度都从 1 开始重数。此处按题目 id 预计算全局序号。
const questionNo = computed<Record<number, number>>(() => {
  const map: Record<number, number> = {};
  let n = 0;
  for (const d of props.detail.dimensions) {
    for (const q of d.questions) {
      n += 1;
      map[q.id] = n;
    }
  }
  return map;
});

// 矩阵题的行：一行一个被评估人（来自评价关系）。无关系时为空数组，
// 由 MatrixTable 渲染占位行。
const matrixRows = computed(() => props.evaluatees.map((e) => e.evaluateeName));
</script>

<template>
  <div class="flex flex-col gap-4" data-testid="eval360-questionnaire-preview-body">
    <!-- 头部：问卷标题 + 描述 + 只读提示。 -->
    <div class="flex flex-col gap-1 border-b border-border pb-3">
      <h2 class="text-lg font-semibold">{{ detail.title }}</h2>
      <p v-if="detail.description" class="text-sm text-gray-500">
        {{ detail.description }}
      </p>
      <p class="text-xs text-gray-400">
        {{ $t("plugin.linapro-eval-360.page.questionnaire.previewHint") }}
      </p>
    </div>

    <Empty
      v-if="questionCount === 0"
      :description="$t('plugin.linapro-eval-360.page.questionnaire.previewEmpty')"
    />

    <!-- 维度分组渲染。 -->
    <template v-else>
      <section
        v-for="dim in detail.dimensions"
        :key="dim.id"
        class="flex flex-col gap-3"
      >
        <h3 class="text-base font-semibold text-primary">{{ dim.name }}</h3>
        <div
          v-for="q in dim.questions"
          :key="q.id"
          class="flex flex-col gap-2 rounded-lg border border-border p-3"
          data-testid="eval360-preview-question"
        >
          <!-- 题干：序号 + 标题（富文本） + 必填星标（不显示题型）。 -->
          <div class="flex items-start gap-2">
            <span class="text-sm font-medium">{{ questionNo[q.id] }}.</span>
            <span class="eval360-rich-inline flex-1 text-sm font-semibold">
              <span v-html="q.title"></span>
              <span v-if="q.required === 1" class="text-red-500">*</span>
            </span>
          </div>

          <!-- 题目描述（富文本，可空）。 -->
          <div
            v-if="q.description"
            class="eval360-rich-block ml-6 text-sm text-gray-500"
            v-html="q.description"
          ></div>

          <!-- 单选题：只读单选组，allowText 选项附只读补充框。 -->
          <RadioGroup v-if="q.type === 'single_choice'" disabled class="ml-6">
            <div class="flex flex-col gap-2">
              <div
                v-for="opt in q.options"
                :key="opt.id"
                class="flex items-center gap-2"
              >
                <Radio :value="opt.id">
                  {{ opt.label }}
                </Radio>
                <span
                  v-if="opt.allowText === 1"
                  class="text-xs text-gray-400"
                >
                  （{{ $t("plugin.linapro-eval-360.page.questionnaire.allowText") }}）
                </span>
              </div>
            </div>
          </RadioGroup>

          <!-- 短文本 / 长文本：只读文本框，行数区分。 -->
          <Textarea
            v-else-if="q.type === 'text_short' || q.type === 'text_long'"
            disabled
            class="ml-6"
            :rows="q.type === 'text_long' ? 4 : 2"
            :placeholder="$t('plugin.linapro-eval-360.page.questionnaire.previewTextPlaceholder')"
          />

          <!-- 矩阵题：行=被评估人（来自评价关系），列=评价项（两行表头：文案 + 分值）。
               未导入关系时行渲染为占位行。 -->
          <div v-else-if="q.type === 'matrix'" class="ml-6">
            <MatrixTable
              :columns="q.matrixColumns"
              :rows="matrixRows"
              :empty-hint="$t('plugin.linapro-eval-360.page.questionnaire.matrixNoEvaluatee')"
            />
          </div>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
/* 标题为富文本：段落内联渲染，星标紧随其后。 */
.eval360-rich-inline :deep(p) {
  display: inline;
  margin: 0;
}
/* 描述为富文本块：恢复段落的正常块级边距。 */
.eval360-rich-block :deep(p) {
  margin: 0 0 0.25em;
}
.eval360-rich-block :deep(p:last-child) {
  margin-bottom: 0;
}
</style>
