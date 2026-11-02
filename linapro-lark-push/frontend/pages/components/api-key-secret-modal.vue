<script setup lang="ts">
import { ref } from "vue";

import { useVbenModal } from "@vben/common-ui";

import { Alert, Button, Input, message } from "ant-design-vue";

import { getApiKeySecret } from "../api-key-client";

const appKey = ref("");
const secret = ref("");

const [Modal, modalApi] = useVbenModal({
  class: "w-[560px] max-w-[calc(100vw-32px)]",
  showCancelButton: false,
  confirmText: "关闭",
  onConfirm: () => modalApi.close(),
  onOpenChange: async (isOpen: boolean) => {
    if (!isOpen) {
      secret.value = "";
      return;
    }
    // 新增成功时直接带着 secret 打开；从列表打开时按 id 查询（需要「查看密钥」权限）。
    const data = modalApi.getData<{ appKey: string; id?: number; secret?: string }>();
    appKey.value = data?.appKey ?? "";
    secret.value = data?.secret ?? "";
    if (secret.value || !data?.id) return;

    modalApi.setState({ loading: true });
    try {
      secret.value = await getApiKeySecret(data.id);
    } finally {
      modalApi.setState({ loading: false });
    }
  },
});

async function handleCopy() {
  try {
    await navigator.clipboard.writeText(secret.value);
    message.success("已复制");
  } catch {
    message.error("复制失败，请手动选择复制");
  }
}
</script>

<template>
  <Modal title="签名密钥">
    <div class="space-y-4">
      <Alert
        message="secret 只用于调用方本地计算 HMAC-SHA256 签名，不要放进请求或日志；请通过安全渠道交给调用方。"
        show-icon
        type="warning"
      />
      <div class="text-sm">
        app_key：<span class="font-mono" data-testid="lark-push-secret-app-key">{{
          appKey
        }}</span>
      </div>
      <div class="flex w-full gap-2">
        <Input
          class="font-mono"
          data-testid="lark-push-secret-value"
          readonly
          :value="secret"
        />
        <Button
          data-testid="lark-push-secret-copy"
          :disabled="!secret"
          @click="handleCopy"
        >
          复制
        </Button>
      </div>
    </div>
  </Modal>
</template>
