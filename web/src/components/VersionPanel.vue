<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from "vue";
import {
  Download,
  RefreshCw,
  LoaderCircle,
  ExternalLink,
} from "lucide-vue-next";
import { api, APIError } from "../api";
import Dialog from "./Dialog.vue";
const props = defineProps<{
  disabled: boolean;
  pending: boolean;
  dirty: boolean;
}>();
const emit = defineEmits<{ busy: [value: boolean]; expired: [] }>();
type Release = {
  current: string;
  latest: string;
  available: boolean;
  can_update: boolean;
  url: string;
  notes: string;
  published_at: string;
  checked_at: string;
};
type Status = {
  phase: string;
  message: string;
  version?: string;
  updated_at?: string;
};
const release = ref<Release | null>(null),
  current = ref(""),
  checking = ref(false),
  updating = ref(false),
  confirming = ref(false),
  error = ref(""),
  status = ref<Status | null>(null);
let timer: ReturnType<typeof setTimeout> | undefined,
  disposed = false,
  startedAt = 0,
  previousTimestamp = "",
  deadline = 0;
const activePhases = [
  "checking",
  "downloading",
  "verifying",
  "installing",
  "restarting",
];
function finish() {
  updating.value = false;
  emit("busy", false);
}
function fail(e: unknown) {
  error.value = e instanceof Error ? e.message : "版本检查失败，请重试";
  if (e instanceof APIError && e.status === 401) emit("expired");
}
function schedule(delay = 2000) {
  clearTimeout(timer);
  if (!disposed) timer = setTimeout(() => void poll(), delay);
}
async function poll() {
  try {
    const result = await api<{ current: string; status: Status }>(
      "/update/status",
    );
    if (disposed) return;
    current.value = result.current;
    const timestamp = result.status.updated_at || "";
    // Ignore a previous update's saved terminal status until this task publishes fresh progress.
    if (startedAt && timestamp === previousTimestamp) {
      if (Date.now() > deadline) {
        error.value =
          "未收到更新任务进度，请检查 SSH 中的 systemctl status meshport-update";
        finish();
      } else schedule();
      return;
    }
    const wasUpdating = updating.value;
    const previousStatus = status.value;
    status.value = result.status;
    if (activePhases.includes(result.status.phase)) {
      if (!deadline) deadline = Date.now() + 360000;
      updating.value = true;
      emit("busy", true);
      if (Date.now() > deadline) {
        error.value = "更新仍未完成，请通过 SSH 检查 meshport-update 服务日志";
        finish();
      } else schedule();
    } else {
      finish();
      startedAt = 0;
      deadline = 0;
      if (
        result.status.phase === "failed" &&
        result.status.updated_at !== previousStatus?.updated_at
      )
        error.value = result.status.message;
      if (wasUpdating && result.status.phase === "done") release.value = null;
      schedule(10000);
    }
  } catch (e) {
    if (disposed) return;
    if (
      updating.value &&
      Date.now() < deadline &&
      !(e instanceof APIError && e.status === 401)
    ) {
      status.value = {
        phase: "restarting",
        message: "后台暂时不可访问，正在等待重新连接…",
      };
      schedule();
    } else {
      fail(e);
      finish();
    }
  }
}
async function check() {
  if (checking.value || updating.value || props.disabled) return;
  checking.value = true;
  error.value = "";
  try {
    const value = await api<Release>("/update");
    if (!disposed) {
      release.value = value;
      current.value = value.current;
    }
  } catch (e) {
    if (!disposed) fail(e);
  } finally {
    checking.value = false;
  }
}
async function start() {
  if (props.disabled || props.pending || props.dirty || updating.value) return;
  error.value = "";
  clearTimeout(timer);
  updating.value = true;
  emit("busy", true);
  previousTimestamp = status.value?.updated_at || "";
  startedAt = Date.now();
  deadline = Date.now() + 360000;
  status.value = { phase: "checking", message: "正在启动更新任务…" };
  try {
    await api("/update", "POST", {});
    confirming.value = false;
    schedule();
  } catch (e) {
    if (e instanceof APIError && e.status === 0) {
      confirming.value = false;
      status.value = {
        phase: "checking",
        message: "启动结果待确认，正在查询更新进度…",
      };
      schedule();
      return;
    }
    fail(e);
    finish();
    startedAt = 0;
    deadline = 0;
  }
}
onMounted(() => void poll());
onBeforeUnmount(() => {
  disposed = true;
  clearTimeout(timer);
  emit("busy", false);
});
</script>

<template>
  <section class="panel version-panel">
    <div class="panel-top">
      <div>
        <h2>版本与更新</h2>
        <p>
          当前版本：<strong class="mono">{{
            current ? "v" + current : "正在读取…"
          }}</strong>
        </p>
      </div>
      <button
        class="outline"
        :disabled="disabled || checking || updating"
        :aria-busy="checking"
        @click="check"
      >
        <RefreshCw :class="{ spin: checking }" />检查最新版本
      </button>
    </div>
    <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
    <div v-if="updating" class="test-progress" role="status" aria-live="polite">
      <LoaderCircle class="spin" /><span>{{ status?.message }}</span>
    </div>
    <p v-else-if="status?.phase === 'done'" role="status">
      {{ status.message }} · {{ status.version ? "v" + status.version : "" }}
    </p>
    <template v-if="release">
      <p role="status">
        {{
          release.available
            ? "发现新版本"
            : "当前已是最新稳定版本（或比最新发行版更新）"
        }}：<strong class="mono">v{{ release.latest }}</strong>
      </p>
      <div class="version-actions">
        <a :href="release.url" target="_blank" rel="noopener noreferrer"
          ><ExternalLink />查看发布说明</a
        ><button
          v-if="release.available"
          class="primary"
          :disabled="
            disabled || updating || !release.can_update || pending || dirty
          "
          @click="confirming = true"
        >
          <Download />一键更新
        </button>
      </div>
      <p v-if="!release.can_update">
        一键更新需要通过安装脚本部署到 Linux systemd。
      </p>
      <p v-if="pending">请先确认 TUN 或等待恢复，再更新。</p>
      <p v-if="dirty">请先保存或放弃未保存的设置，再更新。</p>
    </template>
    <p>
      更新会保留配置、订阅和代理内核，管理后台会短暂重启。命令行也可运行
      <code>sudo meshport update</code>。
    </p>
    <Dialog
      :open="confirming"
      title="更新 Meshport？"
      description="将下载并校验最新稳定版，保留配置和订阅，然后重启管理后台。页面会自动等待重新连接。"
      :busy="updating"
      @close="confirming = false"
    >
      <p>
        当前 v{{ current }} → 最新稳定版 v{{
          release?.latest
        }}。更新时会再次检查最新版本。
      </p>
      <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
      <template #actions
        ><button
          class="outline"
          data-safe-focus
          :disabled="updating"
          @click="confirming = false"
        >
          取消</button
        ><button
          class="primary"
          :disabled="updating || disabled"
          :aria-busy="updating"
          @click="start"
        >
          更新并重启后台
        </button></template
      >
    </Dialog>
  </section>
</template>
