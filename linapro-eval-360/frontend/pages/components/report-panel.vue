<script setup lang="ts">
import { ref } from 'vue';

import { Alert, message } from 'ant-design-vue';

import { $t } from '#/locales';
import { downloadBlob } from '#/utils/download';
import { exportByAnswer, exportByScore } from '../eval-client';

const props = defineProps<{ questionnaireId: number }>();

const exportingAnswer = ref(false);
const exportingScore = ref(false);

// 两个独立按钮，各自直出单个 Excel，不打包 ZIP（见 design D7 / tasks 7.5）。
async function handleByAnswer() {
  exportingAnswer.value = true;
  try {
    const blob = await exportByAnswer(props.questionnaireId);
    downloadBlob(blob, '按答案导出.xlsx');
  } catch {
    // 错误码中文提示由请求拦截器统一处理。
  } finally {
    exportingAnswer.value = false;
  }
}

async function handleByScore() {
  exportingScore.value = true;
  try {
    const blob = await exportByScore(props.questionnaireId);
    downloadBlob(blob, '按得分导出.xlsx');
  } catch {
    // 同上。
  } finally {
    exportingScore.value = false;
  }
}
</script>

<template>
  <div class="flex flex-col gap-3" data-testid="eval360-report-panel">
    <div class="text-sm text-gray-500">
      {{ $t('plugin.linapro-eval-360.page.report.hint') }}
    </div>
    <div class="flex gap-3">
      <a-button
        type="primary"
        data-testid="eval360-export-by-answer"
        :loading="exportingAnswer"
        @click="handleByAnswer"
      >
        {{ $t('plugin.linapro-eval-360.page.report.byAnswer') }}
      </a-button>
      <a-button
        type="primary"
        data-testid="eval360-export-by-score"
        :loading="exportingScore"
        @click="handleByScore"
      >
        {{ $t('plugin.linapro-eval-360.page.report.byScore') }}
      </a-button>
    </div>
    <Alert
      type="info"
      show-icon
      :message="$t('plugin.linapro-eval-360.page.report.byAnswerDesc')"
    />
    <Alert
      type="info"
      show-icon
      :message="$t('plugin.linapro-eval-360.page.report.byScoreDesc')"
    />
  </div>
</template>
