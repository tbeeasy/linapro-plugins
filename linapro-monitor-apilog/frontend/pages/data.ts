import type { VbenFormSchema } from '#/adapter/form';
import type { VxeGridProps } from '#/adapter/vxe-table';

import { formatTimestamp } from '#/utils/time';

/** 默认查询窗口：最近 7 天。时间范围是必填项（表按 request_at 分区）。 */
export const DEFAULT_QUERY_DAYS = 7;

/** 状态筛选取值。success=2xx，failure=非 2xx。 */
export const STATUS_TYPE_OPTIONS = () => [
  { label: '成功（2xx）', value: 'success' },
  { label: '失败（非 2xx）', value: 'failure' },
];

/** 把日期范围转成后端字段。 */
export function splitDateRange(range: unknown): { beginTime?: string; endTime?: string } {
  if (!Array.isArray(range) || range.length < 2) {
    return {};
  }
  const [beginTime, endTime] = range as [string, string];
  if (!beginTime || !endTime) {
    return {};
  }
  return { beginTime, endTime };
}

/** 默认 7 天范围，格式 YYYY-MM-DD。 */
export function defaultDateRange(): [string, string] {
  const end = new Date();
  const begin = new Date(end.getTime() - DEFAULT_QUERY_DAYS * 24 * 60 * 60 * 1000);
  return [formatDate(begin), formatDate(end)];
}

/** 格式化为 YYYY-MM-DD，避免依赖 dayjs 的隐式行为。 */
function formatDate(value: Date): string {
  const year = value.getFullYear();
  const month = `${value.getMonth() + 1}`.padStart(2, '0');
  const day = `${value.getDate()}`.padStart(2, '0');
  return `${year}-${month}-${day}`;
}

/** 查询表单 schema。 */
export function buildQuerySchema(): VbenFormSchema[] {
  return [
    {
      component: 'RangePicker',
      fieldName: 'timeRange',
      label: '请求时间',
      defaultValue: defaultDateRange(),
      componentProps: {
        valueFormat: 'YYYY-MM-DD',
        allowClear: false,
      },
    },
    {
      component: 'Input',
      fieldName: 'routeOwner',
      label: '归属插件',
    },
    {
      component: 'Input',
      fieldName: 'path',
      label: '请求路径',
    },
    {
      component: 'Input',
      fieldName: 'callerNo',
      label: '工号',
    },
    {
      component: 'Input',
      fieldName: 'callerName',
      label: '姓名',
    },
    {
      component: 'Input',
      fieldName: 'callerId',
      label: '调用方标识',
    },
    {
      component: 'Select',
      fieldName: 'statusType',
      label: '状态码',
      componentProps: {
        options: STATUS_TYPE_OPTIONS(),
        allowClear: true,
      },
    },
    {
      component: 'Input',
      fieldName: 'requestId',
      label: '请求 ID',
    },
  ];
}

/** 调用方展示：优先姓名(工号)，为空时回退 EmpID / app_key。 */
export function formatCaller(row: {
  callerId?: string;
  callerKind?: string;
  callerName?: string;
  callerNo?: string;
}): string {
  const name = (row.callerName ?? '').trim();
  const no = (row.callerNo ?? '').trim();
  if (name && no) {
    return `${name}(${no})`;
  }
  if (name) {
    return name;
  }
  if (no) {
    return no;
  }
  return (row.callerId ?? '').trim();
}

/** HTTP 方法标签颜色。 */
export function getMethodTagColor(method: string): string {
  const map: Record<string, string> = {
    GET: 'green',
    POST: 'blue',
    PUT: 'orange',
    DELETE: 'red',
    PATCH: 'cyan',
  };
  return map[method?.toUpperCase()] || 'default';
}

/** 状态码标签颜色。 */
export function getStatusTagColor(statusCode: number): string {
  if (statusCode >= 200 && statusCode < 300) {
    return 'green';
  }
  if (statusCode >= 500) {
    return 'red';
  }
  if (statusCode >= 400) {
    return 'orange';
  }
  return 'default';
}

/** 表格列定义。 */
export function buildColumns(): VxeGridProps['columns'] {
  return [
    {
      field: 'requestAt',
      title: '请求时间',
      formatter: ({ cellValue }) => formatTimestamp(cellValue),
      minWidth: 180,
    },
    {
      field: 'method',
      title: '请求方法',
      minWidth: 90,
    },
    {
      field: 'path',
      title: '请求路径',
      minWidth: 260,
      showOverflow: 'tooltip',
    },
    {
      field: 'routeOwner',
      title: '归属插件',
      minWidth: 170,
    },
    {
      field: 'caller',
      title: '调用方',
      minWidth: 170,
      slots: {
        default: ({ row }) => formatCaller(row),
      },
    },
    {
      field: 'statusCode',
      title: '状态码',
      minWidth: 100,
    },
    {
      field: 'costMs',
      title: '耗时',
      formatter: ({ cellValue }) => `${cellValue} ms`,
      minWidth: 110,
    },
    {
      field: 'clientIp',
      title: '客户端 IP',
      minWidth: 130,
    },
    {
      field: 'requestId',
      title: '请求 ID',
      minWidth: 240,
      showOverflow: 'tooltip',
    },
    {
      field: 'action',
      fixed: 'right',
      slots: { default: 'action' },
      title: '操作',
      resizable: false,
      width: 'auto',
    },
  ];
}
