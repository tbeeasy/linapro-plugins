<script lang="ts">
export const pluginPageMeta = {
  routePath: '/monitor/apilog',
  title: '接口请求日志',
};
</script>

<script setup lang="ts">
import type { ApiLog } from './apilog-client';

import { useAccess } from '@vben/access';
import { Page, useVbenDrawer } from '@vben/common-ui';

import { useVbenVxeGrid } from '#/adapter/vxe-table';

import { apiLogList } from './apilog-client';
import { buildColumns, buildQuerySchema, splitDateRange } from './data';
import ApiLogDetailDrawer from './apilog-detail-drawer.vue';

// 详情含完整请求头/请求体/响应体，单独受 monitor:apilog:query 控制：只有列表权限的角色看不到入口。
const detailAccessCode = 'monitor:apilog:query';
const { hasAccessByCodes } = useAccess();

const [DetailDrawerRef, detailDrawerApi] = useVbenDrawer({
  connectedComponent: ApiLogDetailDrawer,
});

/**
 * 记录是只增不删的审计数据：页面刻意没有任何删除、清空入口。
 * 时间范围必填（表按 request_at 分区），缺省 7 天在 schema 里给出。
 */
const [Grid, gridApi] = useVbenVxeGrid({
  formOptions: {
    schema: buildQuerySchema(),
    commonConfig: {
      labelWidth: 90,
      componentProps: {
        allowClear: true,
      },
    },
    wrapperClass: 'grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4',
  },
  gridOptions: {
    columns: buildColumns(),
    height: 'auto',
    keepSource: true,
    pagerConfig: {},
    // 后端固定按请求时间倒序，不接受排序参数，因此不开启列排序。
    proxyConfig: {
      ajax: {
        query: async ({ page }: any, formValues: Record<string, any> = {}) => {
          const { beginTime, endTime } = splitDateRange(formValues.timeRange);
          const params: Record<string, any> = {
            pageNum: page.currentPage,
            pageSize: page.pageSize,
            beginTime,
            endTime,
          };
          // 只有真正填写的筛选项才进 WHERE：空串会让后端按「精确等于空」过滤。
          for (const key of [
            'routeOwner',
            'path',
            'callerNo',
            'callerName',
            'callerId',
            'requestId',
            'statusType',
          ]) {
            const value = formValues[key];
            if (typeof value === 'string' && value.trim() !== '') {
              params[key] = value.trim();
            } else if (value !== undefined && value !== null && value !== '') {
              params[key] = value;
            }
          }
          return await apiLogList(params as any);
        },
      },
    },
    headerCellConfig: {
      height: 44,
    },
    cellConfig: {
      height: 48,
    },
    rowConfig: {
      keyField: 'id',
    },
    id: 'linapro-monitor-apilog-index',
  },
});

/**
 * 详情需要 requestAt：记录表按 request_at 分区，只传 id 会退化成全分区扫描。
 * 列表行本来就带 requestAt，直接连同行数据交给抽屉。
 */
function handlePreview(row: ApiLog) {
  detailDrawerApi.setData({ record: row });
  detailDrawerApi.open();
}
</script>

<template>
  <Page :auto-content-height="true">
    <Grid table-title="接口请求日志列表">
      <template #action="{ row }">
        <ghost-button
          v-if="hasAccessByCodes([detailAccessCode])"
          data-testid="apilog-detail-open"
          @click.stop="handlePreview(row)"
        >
          详情
        </ghost-button>
      </template>
    </Grid>

    <DetailDrawerRef />
  </Page>
</template>
