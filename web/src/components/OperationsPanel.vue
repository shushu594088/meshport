<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from "vue";
import { api, APIError } from "../api";
import Dialog from "./Dialog.vue";
import Field from "./Field.vue";
const props = defineProps<{
  mode: "connections" | "logs";
  running: boolean;
  disabled: boolean;
  pending: boolean;
  logLevel: string;
  execute: (task: () => Promise<unknown>, text: string) => Promise<boolean>;
}>();
const emit = defineEmits<{ expired: [] }>();
type Connection = {
  id: string;
  metadata: {
    host: string;
    destinationIP: string;
    destinationPort: string;
    sourceIP: string;
    network: string;
    process: string;
  };
  chains: string[];
  upload: number;
  download: number;
  rule: string;
};
type Snapshot = {
  connections: Connection[];
  uploadTotal: number;
  downloadTotal: number;
  memory: number;
};
type Log = { type: string; payload: string };
const snapshot = ref<Snapshot | null>(null),
  logs = ref<Log[]>([]),
  error = ref(""),
  loading = ref(false),
  paused = ref(false),
  query = ref(""),
  page = ref(1),
  checked = ref(""),
  target = ref<Connection | null>(null),
  confirming = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined,
  controller: AbortController | undefined,
  disposed = false,
  sequence = 0;
function bytes(n: number) {
  if (!n) return "0 B";
  const unit = Math.min(3, Math.floor(Math.log(n) / Math.log(1024)));
  return (
    (n / 1024 ** unit).toFixed(unit ? 1 : 0) +
    " " +
    ["B", "KiB", "MiB", "GiB"][unit]
  );
}
const filtered = computed(() => {
  const q = query.value.toLocaleLowerCase("zh-CN");
  return (snapshot.value?.connections || []).filter((v) =>
    [
      v.metadata.host,
      v.metadata.destinationIP,
      v.metadata.sourceIP,
      v.metadata.process,
      ...(v.chains || []),
    ]
      .join(" ")
      .toLocaleLowerCase("zh-CN")
      .includes(q),
  );
});
const pages = computed(() =>
  Math.max(1, Math.ceil(filtered.value.length / 20)),
);
const visible = computed(() =>
  filtered.value.slice((page.value - 1) * 20, page.value * 20),
);
watch(query, () => (page.value = 1));
watch(pages, (n) => (page.value = Math.min(page.value, n)));
async function poll() {
  clearTimeout(timer);
  if (disposed || !props.running || paused.value || loading.value) return;
  controller = new AbortController();
  const id = ++sequence;
  loading.value = true;
  try {
    if (props.mode === "connections") {
      const value = await api<Snapshot>(
        "/connections",
        "GET",
        undefined,
        controller.signal,
      );
      if (id === sequence) snapshot.value = value;
    } else {
      const value = await api<Log[]>(
        "/logs",
        "GET",
        undefined,
        controller.signal,
      );
      if (id === sequence) logs.value = [...logs.value, ...value].slice(-300);
    }
    if (id === sequence) {
      error.value = "";
      checked.value = new Date().toLocaleTimeString("zh-CN");
    }
  } catch (e) {
    if (!controller.signal.aborted && id === sequence) {
      error.value = e instanceof Error ? e.message : "读取失败，请重试";
      if (e instanceof APIError && e.status === 401) {
        paused.value = true;
        emit("expired");
      }
    }
  } finally {
    if (id === sequence) loading.value = false;
    if (!disposed && props.running && !paused.value)
      timer = setTimeout(() => void poll(), props.mode === "logs" ? 300 : 3000);
  }
}
function stop() {
  clearTimeout(timer);
  ++sequence;
  controller?.abort();
  loading.value = false;
}
watch(
  () => [props.running, paused.value],
  () => {
    stop();
    if (props.running && !paused.value) void poll();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  disposed = true;
  stop();
});
function ask(connection: Connection | null) {
  target.value = connection;
  confirming.value = true;
}
async function close() {
  if (
    await props.execute(
      () =>
        api("/core/action", "POST", {
          action: "connections-close",
          id: target.value?.id || "",
        }),
      target.value ? "连接已关闭" : "所有连接已关闭",
    )
  ) {
    confirming.value = false;
    stop();
    void poll();
  }
}
</script>
<template>
  <section class="operations">
    <div class="section-head">
      <div>
        <h2>{{ mode === "connections" ? "实时连接" : "内核实时日志" }}</h2>
        <p>
          {{
            mode === "connections"
              ? "每 3 秒刷新。关闭连接后，应用可能自动重连。"
              : "仅收集打开本页期间的日志，最多保留最近 300 条；离开页面后清空。"
          }}
        </p>
      </div>
      <div class="test-actions">
        <button class="outline" :disabled="!running" @click="paused = !paused">
          {{ paused ? "继续刷新" : "暂停刷新" }}</button
        ><button class="outline" :disabled="loading || !running" @click="poll">
          重试读取</button
        ><button
          v-if="mode === 'connections'"
          class="danger-button"
          :disabled="
            disabled || pending || !running || !snapshot?.connections?.length
          "
          @click="ask(null)"
        >
          关闭全部连接</button
        ><button
          v-else
          class="outline"
          :disabled="!logs.length"
          @click="logs = []"
        >
          清空本页日志
        </button>
      </div>
    </div>
    <div v-if="!running" class="empty panel">
      <h3>内核已停止</h3>
      <p>
        请在概览启动代理，再读取{{ mode === "connections" ? "连接" : "日志" }}。
      </p>
    </div>
    <template v-else>
      <div v-if="error" class="alert danger" role="alert">
        {{ error }}；已读取的数据保留。
      </div>
      <p class="live-status" role="status">
        {{ paused ? "刷新已暂停" : loading ? "正在读取…" : "自动刷新中"
        }}{{ checked ? " · 最近读取 " + checked : "" }}
      </p>
      <template v-if="mode === 'connections'">
        <div class="network-totals panel" v-if="snapshot">
          <span
            >累计上传 <strong>{{ bytes(snapshot.uploadTotal) }}</strong></span
          ><span
            >累计下载 <strong>{{ bytes(snapshot.downloadTotal) }}</strong></span
          ><span
            >内核内存 <strong>{{ bytes(snapshot.memory) }}</strong></span
          ><span
            >当前连接
            <strong>{{ snapshot.connections?.length || 0 }}</strong></span
          >
        </div>
        <Field
          id="connectionSearch"
          v-model="query"
          label="筛选连接"
          type="search"
          hint="按目标域名、IP、进程或节点筛选，数据仅保留在本页。"
        />
        <div v-if="!visible.length" class="empty panel">
          <h3>
            {{
              query
                ? "没有匹配的连接"
                : snapshot
                  ? "暂无活动连接"
                  : "等待读取连接"
            }}
          </h3>
          <p>
            {{
              query
                ? "调整关键词，或清空筛选。"
                : "有流量经过代理时会出现在这里。"
            }}
          </p>
          <button v-if="query" class="outline" @click="query = ''">
            清空筛选
          </button>
        </div>
        <div class="connection-list">
          <article
            v-for="connection in visible"
            :key="connection.id"
            class="panel connection-card"
          >
            <div>
              <h3>
                {{
                  connection.metadata.host ||
                  connection.metadata.destinationIP ||
                  "未知目标"
                }}<span class="mono"
                  >:{{ connection.metadata.destinationPort }}</span
                >
              </h3>
              <p>
                {{ connection.metadata.network }} ·
                {{ connection.metadata.sourceIP }} ·
                {{ connection.metadata.process || "未提供进程" }}
              </p>
              <p>
                {{ (connection.chains || []).join(" → ") }} ·
                {{ connection.rule }}
              </p>
              <small
                >上传 {{ bytes(connection.upload) }} · 下载
                {{ bytes(connection.download) }}</small
              >
            </div>
            <button
              class="outline"
              :disabled="disabled || pending"
              :aria-label="'关闭连接 ' + connection.id"
              @click="ask(connection)"
            >
              关闭连接
            </button>
          </article>
        </div>
        <footer v-if="filtered.length" class="pagination">
          <span
            >共 {{ filtered.length }} 项 · 第 {{ page }} / {{ pages }} 页</span
          >
          <div>
            <button class="outline" :disabled="page <= 1" @click="page--">
              上一页</button
            ><button class="outline" :disabled="page >= pages" @click="page++">
              下一页
            </button>
          </div>
        </footer>
      </template>
      <template v-else
        ><div v-if="!logLevel || logLevel === 'silent'" class="alert info">
          当前日志等级为静默。可在设置中改为“信息”或“调试”并保存。
        </div>
        <div class="panel log-feed" tabindex="0" aria-label="最近内核日志">
          <p v-if="!logs.length">暂无日志，等待新的内核事件。</p>
          <p v-for="(entry, index) in logs" :key="index" class="mono">
            <strong>{{ entry.type }}</strong> {{ entry.payload }}
          </p>
        </div></template
      >
    </template>
  </section>
  <Dialog
    :open="confirming"
    title="关闭代理连接？"
    :busy="disabled"
    :description="
      target
        ? '将关闭 ' +
          (target.metadata.host || target.metadata.destinationIP) +
          ' 的这一条连接，应用可能自动重连。'
        : '将关闭全部活动代理连接，正在进行的请求可能中断。'
    "
    @close="confirming = false"
    ><template #actions
      ><button
        class="outline"
        data-safe-focus
        :disabled="disabled"
        @click="confirming = false"
      >
        取消</button
      ><button class="danger-button" :disabled="disabled" @click="close">
        确认关闭
      </button></template
    ></Dialog
  >
</template>
