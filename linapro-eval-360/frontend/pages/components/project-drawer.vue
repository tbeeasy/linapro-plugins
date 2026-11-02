<script setup lang="ts">
import { ref } from 'vue';

import { useVbenDrawer } from '@vben/common-ui';

import { message } from 'ant-design-vue';

import { useVbenForm } from '#/adapter/form';
import { $t } from '#/locales';
import { projectCreate, projectDetail, projectUpdate } from '../eval-client';

const emit = defineEmits<{ reload: [] }>();

const isUpdate = ref(false);
const projectId = ref<number>(0);
const title = ref('');

// msToPicker 把 Unix 毫秒时间戳转为 DatePicker 承载的 YYYY-MM-DD 字符串（null 透传 null）。
// 日期取本地时区当天（date-only，用户不关心时分秒）。
function msToPicker(ms: null | number | undefined): null | string {
  if (ms == null) return null;
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return null;
  const y = d.getFullYear();
  const m = `${d.getMonth() + 1}`.padStart(2, '0');
  const day = `${d.getDate()}`.padStart(2, '0');
  return `${y}-${m}-${day}`;
}

// pickerToMs 把 DatePicker 产出的 YYYY-MM-DD 字符串转为 Unix 毫秒时间戳（空值透传 null）。
// startAt 取所选日期当天 00:00:00，endAt 取当天 23:59:59（避免截止边界误判把当天误作已截止）。
function pickerToMs(v: null | string | undefined, endOfDay = false): null | number {
  if (!v) return null;
  const [y, m, d] = v.split('-').map(Number);
  if (!y || !m || !d) return null;
  const date = new Date(y, m - 1, d, endOfDay ? 23 : 0, endOfDay ? 59 : 0, endOfDay ? 59 : 0);
  const ms = date.getTime();
  return Number.isNaN(ms) ? null : ms;
}

const [Form, formApi] = useVbenForm({
  commonConfig: {
    labelWidth: 100,
    componentProps: { class: 'w-full' },
  },
  schema: [
    {
      fieldName: 'name',
      label: $t('plugin.linapro-eval-360.page.project.fields.name'),
      component: 'Input',
      rules: 'required',
    },
    {
      fieldName: 'description',
      label: $t('plugin.linapro-eval-360.page.project.fields.description'),
      component: 'Textarea',
      componentProps: { rows: 4, placeholder: $t('plugin.linapro-eval-360.page.project.fields.descriptionPlaceholder') },
    },
    {
      fieldName: 'startAt',
      label: $t('plugin.linapro-eval-360.page.project.fields.startAt'),
      component: 'DatePicker',
      componentProps: {
        showTime: false,
        valueFormat: 'YYYY-MM-DD',
        style: { width: '100%' },
        placeholder: $t('plugin.linapro-eval-360.page.project.fields.datePlaceholder'),
      },
    },
    {
      fieldName: 'endAt',
      label: $t('plugin.linapro-eval-360.page.project.fields.endAt'),
      component: 'DatePicker',
      componentProps: {
        showTime: false,
        valueFormat: 'YYYY-MM-DD',
        style: { width: '100%' },
        placeholder: $t('plugin.linapro-eval-360.page.project.fields.datePlaceholder'),
      },
    },
  ],
  showDefaultActions: false,
});

const [Drawer, drawerApi] = useVbenDrawer({
  async onOpenChange(open) {
    if (!open) return;
    drawerApi.setState({ loading: true });
    const data = drawerApi.getData<{ id?: number; update: boolean }>();
    isUpdate.value = !!data?.update;
    title.value = isUpdate.value
      ? $t('plugin.linapro-eval-360.page.project.editTitle')
      : $t('plugin.linapro-eval-360.page.project.createTitle');
    if (isUpdate.value && data?.id) {
      projectId.value = data.id;
      const res = await projectDetail(data.id);
      // 后端时间字段为 Unix 毫秒时间戳，DatePicker 用 ISO 字符串承载，此处做投影转换。
      await formApi.setValues({
        ...res.detail,
        startAt: msToPicker(res.detail.startAt),
        endAt: msToPicker(res.detail.endAt),
      });
    } else {
      projectId.value = 0;
      await formApi.resetForm();
    }
    drawerApi.setState({ loading: false });
  },
  async onConfirm() {
    try {
      drawerApi.lock(true);
      const { valid } = await formApi.validate();
      if (!valid) return;
      const values = await formApi.getValues();
      // DatePicker 产出 YYYY-MM-DD 字符串，回传后端前转为 Unix 毫秒时间戳。
      // startAt 取当天 00:00:00，endAt 取当天 23:59:59（截止边界：当天内仍可提交）。
      const payload = {
        ...values,
        startAt: pickerToMs(values.startAt, false),
        endAt: pickerToMs(values.endAt, true),
      };
      if (isUpdate.value) {
        await projectUpdate(projectId.value, payload);
        message.success($t('plugin.linapro-eval-360.page.common.updateSuccess'));
      } else {
        await projectCreate(payload);
        message.success($t('plugin.linapro-eval-360.page.common.createSuccess'));
      }
      emit('reload');
      drawerApi.close();
    } catch (error) {
      const msg = error instanceof Error ? error.message : '';
      if (msg) message.error(msg);
    } finally {
      drawerApi.lock(false);
    }
  },
  onClosed() {
    formApi.resetForm();
  },
});
</script>

<template>
  <Drawer :title="title" class="w-[600px]">
    <Form />
  </Drawer>
</template>
