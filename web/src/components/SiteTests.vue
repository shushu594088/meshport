<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount } from "vue";
import {
  RefreshCw,
  LoaderCircle,
  CheckCircle2,
  XCircle,
  CircleHelp,
  Globe,
  Square,
} from "lucide-vue-next";
import { api, APIError } from "../api";
import {
  useDiagnostics,
  diagnosticLabels,
} from "../composables/useDiagnostics";
const props = defineProps<{
  running: boolean;
  disabled: boolean;
  revision: number;
  mode: string;
}>();
const emit = defineEmits<{ busy: [value: boolean]; expired: [] }>();
const sites = ref<{ id: string; name: string }[]>([]),
  error = ref(""),
  loading = ref(true);
const {
  results,
  pending,
  running: testing,
  completed,
  total,
  notice,
  run,
  cancel,
  reset,
} = useDiagnostics(() => emit("expired"));
watch(testing, (value) => emit("busy", value));
watch(() => props.revision, reset);
const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
function date(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "short",
    timeStyle: "medium",
  }).format(new Date(value));
}
async function load() {
  loading.value = true;
  error.value = "";
  try {
    sites.value = await api("/tests/sites");
  } catch (e) {
    error.value = e instanceof Error ? e.message : "加载失败，请重试";
    if (e instanceof APIError && e.status === 401) emit("expired");
  } finally {
    loading.value = false;
  }
}
onMounted(load);
onBeforeUnmount(() => emit("busy", false));
</script>

<template>
  <section class="panel">
    <div class="panel-top">
      <div>
        <h2>网站连通性与解锁测试</h2>
        <p>
          从服务器的当前代理规则发起测试。规则模式可能直连部分网站，各网站出口可能不同。
        </p>
      </div>
      <button
        class="primary"
        :disabled="disabled || testing || !running || !sites.length"
        @click="
          run(
            'site',
            sites.map((site) => site.id),
          )
        "
      >
        <RefreshCw />测试全部
      </button>
    </div>
    <p class="test-explanation">
      “支持”表示检测接口或页面给出支持信号；“可访问”仅表示网页可连通。账号、会员与实际播放结果仍以网站为准。当前模式：{{
        { rule: "规则", global: "全局", direct: "直连" }[mode] || mode
      }}。
    </p>
    <div v-if="!running" class="empty">
      <Globe />
      <h3>内核已停止</h3>
      <p>请在概览中启动代理，再进行网站测试。</p>
      <a href="#overview">前往概览</a>
    </div>
    <div v-else-if="error" class="alert danger" role="alert">
      <span>{{ error }}</span
      ><button class="outline" @click="load">重新加载</button>
    </div>
    <p v-else-if="loading" role="status">
      <LoaderCircle class="spin" />正在加载测试网站…
    </p>
    <template v-else>
      <div class="test-progress" role="status" aria-live="polite">
        <template v-if="testing"
          ><LoaderCircle class="spin" /><span
            >已完成 {{ completed }} / {{ total }} 项</span
          ><progress
            :value="completed"
            :max="total"
            aria-label="网站测试进度"
          /><button class="outline" @click="cancel">
            <Square />取消测试
          </button></template
        >
        <span v-else>{{ notice || "尚未测试，点击“测试全部”开始。" }}</span>
      </div>
      <div class="site-grid">
        <article
          v-for="site in sites"
          :key="site.id"
          class="site-card"
          :aria-busy="pending.has(site.id)"
        >
          <div class="site-heading">
            <h3>{{ site.name }}</h3>
            <button
              class="outline icon-button"
              :aria-label="'测试 ' + site.name"
              :disabled="disabled || testing || !running"
              @click="run('site', [site.id])"
            >
              <RefreshCw :class="{ spin: pending.has(site.id) }" />
            </button>
          </div>
          <div class="test-badges">
            <span
              class="test-badge"
              :class="
                pending.has(site.id)
                  ? 'waiting'
                  : results[site.id]?.status || 'waiting'
              "
            >
              <LoaderCircle
                v-if="pending.has(site.id)"
                class="spin"
              /><component
                v-else
                :is="
                  results[site.id]?.status === 'supported'
                    ? CheckCircle2
                    : results[site.id]?.status === 'unsupported'
                      ? XCircle
                      : results[site.id]?.status === 'reachable'
                        ? Globe
                        : CircleHelp
                "
              />
              {{
                pending.has(site.id)
                  ? "测试中"
                  : results[site.id]
                    ? diagnosticLabels[results[site.id].status]
                    : "未测试"
              }} </span
            ><span
              v-if="!pending.has(site.id) && results[site.id]?.region"
              class="test-region mono"
              >{{ results[site.id].region }}</span
            >
          </div>
          <p class="test-detail">
            {{
              pending.has(site.id)
                ? "正在通过服务器代理检查…"
                : results[site.id]?.detail || "等待测试"
            }}
          </p>
          <footer>
            <span class="mono">{{
              results[site.id]?.delay ? results[site.id].delay + " ms" : "—"
            }}</span
            ><time
              v-if="results[site.id]"
              :datetime="results[site.id].checked_at"
              >{{ date(results[site.id].checked_at) }}</time
            ><span v-else>尚无结果</span>
          </footer>
        </article>
      </div>
      <p class="timezone">
        测试时间：{{ timeZone }} · 切换节点、订阅或模式后需重新测试。
      </p>
    </template>
  </section>
</template>
