<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount } from "vue";
import { X } from "lucide-vue-next";
const props = defineProps<{ open: boolean; title: string; busy?: boolean }>();
const emit = defineEmits<{ close: [] }>();
const dialog = ref<HTMLDialogElement>();
let trigger: HTMLElement | null = null;
watch(
  () => props.open,
  async (open) => {
    await nextTick();
    if (open) {
      trigger = document.activeElement as HTMLElement;
      dialog.value?.showModal();
      dialog.value?.querySelector<HTMLElement>("[data-safe-focus]")?.focus();
    } else {
      dialog.value?.close();
      trigger?.focus();
    }
  },
);
onBeforeUnmount(() => dialog.value?.close());
</script>
<template>
  <dialog
    ref="dialog"
    aria-labelledby="dialog-title"
    @cancel.prevent="!busy && emit('close')"
  >
    <div class="dialog-head">
      <h2 id="dialog-title">{{ title }}</h2>
      <button
        type="button"
        class="icon-button"
        aria-label="关闭对话框"
        :disabled="busy"
        @click="emit('close')"
      >
        <X />
      </button>
    </div>
    <slot />
  </dialog>
</template>
