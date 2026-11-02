<script lang="ts">
// 360 测评管理端入口页。plugin.yaml 菜单 path=/eval-360/projects 指向本页。
// pluginPageMeta.routePath 必须与菜单 path 完全一致，由 page-registry 匹配渲染。
export const pluginPageMeta = {
  routePath: '/eval-360/projects',
  title: '360 测评',
};
</script>

<script setup lang="ts">
import { ref } from 'vue';

import { Page } from '@vben/common-ui';

import ProjectDetail from './components/project-detail.vue';
import ProjectList from './components/project-list.vue';
import QuestionnaireDetail from './components/questionnaire-detail.vue';

// 单菜单单挂载点内切换「项目列表 / 项目详情 / 问卷详情」三视图。
// 视图状态改由组件内部 ref 承载，不写入 URL：
//   1. URL 恒为菜单路径 /eval-360/projects，路径不变→多标签页天然只有一个，
//      无需 query.pageKey，也不用改 meta.fullPathKey（那依赖挂载时机，不稳）；
//   2. 项目/问卷 id 只在调接口时传（本就在请求体内），不暴露在地址栏；
//   3. 代价：刷新/浏览器后退不保留子视图（明确不需要收藏/深链），可接受。
const projectId = ref<null | number>(null);
const questionnaireId = ref<null | number>(null);
// 从项目详情卡片进入问卷时携带目标 tab（评测问卷/评价关系），默认落在评价关系。
const initialTab = ref<'questionnaire' | 'relation' | 'report'>('relation');

function showProjectList() {
  projectId.value = null;
  questionnaireId.value = null;
}

function showProjectDetail(id: number) {
  projectId.value = id;
  questionnaireId.value = null;
}

function showQuestionnaireDetail(
  pid: number,
  qid: number,
  tab: 'questionnaire' | 'relation' | 'report' = 'relation',
) {
  projectId.value = pid;
  questionnaireId.value = qid;
  initialTab.value = tab;
}
</script>

<template>
  <Page :auto-content-height="true">
    <ProjectList v-if="projectId === null" @open-project="showProjectDetail" />
    <ProjectDetail
      v-else-if="questionnaireId === null"
      :project-id="projectId"
      @back="showProjectList"
      @open-questionnaire="(qid: number, tab?: 'questionnaire' | 'relation') => showQuestionnaireDetail(projectId!, qid, tab)"
    />
    <QuestionnaireDetail
      v-else
      :project-id="projectId"
      :questionnaire-id="questionnaireId"
      :initial-tab="initialTab"
      @list="showProjectList"
      @project="(pid: number) => showProjectDetail(pid)"
      @open="(pid: number, qid: number, tab?: 'questionnaire' | 'relation' | 'report') => showQuestionnaireDetail(pid, qid, tab)"
    />
  </Page>
</template>
