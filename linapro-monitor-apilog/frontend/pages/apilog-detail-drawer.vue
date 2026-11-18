<script setup lang="ts">
import type { ApiLog, ApiLogDetail } from './apilog-client';

import { computed, shallowRef } from 'vue';

import { useVbenDrawer } from '@vben/common-ui';

import { Descriptions, DescriptionsItem, Spin, Tag } from 'ant-design-vue';

import JsonPreview from '#/components/json-preview/index.vue';
import { formatTimestamp } from '#/utils/time';

import { apiLogDetail } from './apilog-client';
import { formatCaller, getMethodTagColor, getStatusTagColor } from './data';

const [BasicDrawer, drawerApi] = useVbenDrawer({
  onOpenChange: handleOpenChange,
  onClosed() {
    currentLog.value = null;
  },
});

const currentLog = shallowRef<ApiLogDetail | null>(null);
const loading = shallowRef(false);
const loadError = shallowRef('');

const callerText = computed(() => (currentLog.value ? formatCaller(currentLog.value) : ''));

/**
 * 打开抽屉时用行数据渲染概要，同时按 id + requestAt 拉取完整详情。
 * requestAt 必须一起回传：记录表按 request_at 分区，只给 id 会扫描全部分区。
 */
async function handleOpenChange(open: boolean) {
  if (!open) {
    return;
  }
  const { record } = drawerApi.getData() as { record: ApiLog };
  if (!record) {
    return;
  }
  currentLog.value = { ...record, reqBody: '', reqHeaders: '', respBody: '' };
  loading.value = true;
  loadError.value = '';
  try {
    const detail = await apiLogDetail(record.id, record.requestAt);
    currentLog.value = detail;
  } catch (error: any) {
    loadError.value = error?.message ?? '加载日志详情失败';
  } finally {
    loading.value = false;
  }
}

/** 只解析对象/数组形态的 JSON，其余按纯文本展示。 */
function parseJson(str: string): any {
  if (!str) {
    return null;
  }
  try {
    const obj = JSON.parse(str);
    if (typeof obj === 'object' && obj !== null) {
      return obj;
    }
    return null;
  } catch {
    return null;
  }
}
</script>

<template>
  <BasicDrawer
    :footer="false"
    class="w-[720px]"
    title="接口请求日志详情"
  >
    <Spin :spinning="loading">
      <div v-if="loadError" class="mb-3 text-red-600" data-testid="apilog-detail-error">
        {{ loadError }}
      </div>
      <Descriptions v-if="currentLog" size="small" bordered :column="1">
        <DescriptionsItem
          label="日志编号"
          :label-style="{ minWidth: '140px' }"
        >
          {{ currentLog.id }}
        </DescriptionsItem>
        <DescriptionsItem label="请求时间">
          {{ formatTimestamp(currentLog.requestAt) }}
        </DescriptionsItem>
        <DescriptionsItem label="归属插件">
          {{ currentLog.routeOwner }}
        </DescriptionsItem>
        <DescriptionsItem label="请求方法">
          <Tag :color="getMethodTagColor(currentLog.method)">
            {{ currentLog.method }}
          </Tag>
        </DescriptionsItem>
        <DescriptionsItem label="请求路径">
          {{ currentLog.path }}
        </DescriptionsItem>
        <DescriptionsItem label="路由模式">
          {{ currentLog.routePath }}
        </DescriptionsItem>
        <DescriptionsItem
          v-if="currentLog.query"
          label="查询串"
        >
          {{ currentLog.query }}
        </DescriptionsItem>
        <DescriptionsItem label="调用方">
          {{ callerText }}
        </DescriptionsItem>
        <DescriptionsItem label="调用方类型">
          {{ currentLog.callerKind }}
        </DescriptionsItem>
        <DescriptionsItem label="租户">
          {{ currentLog.callerTenant }}
        </DescriptionsItem>
        <DescriptionsItem label="状态码">
          <Tag :color="getStatusTagColor(currentLog.statusCode)">
            {{ currentLog.statusCode }}
          </Tag>
        </DescriptionsItem>
        <DescriptionsItem label="耗时">
          {{ currentLog.costMs }} ms
        </DescriptionsItem>
        <DescriptionsItem label="客户端 IP">
          {{ currentLog.clientIp }}
        </DescriptionsItem>
        <DescriptionsItem label="User-Agent">
          {{ currentLog.userAgent }}
        </DescriptionsItem>
        <DescriptionsItem label="请求 ID">
          {{ currentLog.requestId }}
        </DescriptionsItem>
        <DescriptionsItem
          v-if="currentLog.errorMsg"
          label="错误信息"
        >
          <span class="font-semibold text-red-600">{{ currentLog.errorMsg }}</span>
        </DescriptionsItem>
        <DescriptionsItem
          v-if="currentLog.reqHeaders"
          label="请求头"
        >
          <div class="max-h-[300px] overflow-y-auto" data-testid="apilog-detail-req-headers">
            <JsonPreview
              v-if="parseJson(currentLog.reqHeaders)"
              class="break-normal"
              :data="parseJson(currentLog.reqHeaders)"
            />
            <span v-else>{{ currentLog.reqHeaders }}</span>
          </div>
        </DescriptionsItem>
        <DescriptionsItem
          v-if="currentLog.reqBody"
          label="请求体"
        >
          <div class="max-h-[300px] overflow-y-auto" data-testid="apilog-detail-req-body">
            <JsonPreview
              v-if="parseJson(currentLog.reqBody)"
              class="break-normal"
              :data="parseJson(currentLog.reqBody)"
            />
            <span v-else>{{ currentLog.reqBody }}</span>
          </div>
        </DescriptionsItem>
        <DescriptionsItem
          v-if="currentLog.respBody"
          label="响应体"
        >
          <div class="max-h-[300px] overflow-y-auto" data-testid="apilog-detail-resp-body">
            <JsonPreview
              v-if="parseJson(currentLog.respBody)"
              class="break-normal"
              :data="parseJson(currentLog.respBody)"
            />
            <span v-else>{{ currentLog.respBody }}</span>
          </div>
        </DescriptionsItem>
      </Descriptions>
    </Spin>
  </BasicDrawer>
</template>
