<script setup lang="ts">
// 问卷预览弹窗：只读渲染主体委托给 QuestionnairePreviewBody（与评测问卷页内嵌预览共用）。
import { Modal } from "ant-design-vue";

import { $t } from "#/locales";
import type { EvaluateeRow, QuestionnaireDetail } from "../eval-client";
import QuestionnairePreviewBody from "./questionnaire-preview-body.vue";

defineProps<{
  open: boolean;
  detail: QuestionnaireDetail | null;
  // 被评估人清单（来自评价关系）：矩阵题的「行」由此动态渲染。
  evaluatees: EvaluateeRow[];
}>();
const emit = defineEmits<{ "update:open": [value: boolean] }>();

function close() {
  emit("update:open", false);
}
</script>

<template>
  <Modal
    :open="open"
    :title="$t('plugin.linapro-eval-360.page.questionnaire.previewTitle')"
    :width="820"
    :footer="null"
    data-testid="eval360-questionnaire-preview"
    @cancel="close"
  >
    <div v-if="detail" class="max-h-[70vh] overflow-y-auto pr-1">
      <QuestionnairePreviewBody :detail="detail" :evaluatees="evaluatees" />
    </div>
  </Modal>
</template>
