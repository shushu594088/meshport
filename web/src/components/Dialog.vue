<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount, useId } from "vue";
import { X } from "lucide-vue-next";
const props = defineProps<{
  open: boolean;
  title: string;
  busy?: boolean;
  description?: string;
}>();
const titleId = useId(),
  descriptionId = useId();
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
    :aria-labelledby="titleId"
    :aria-describedby="description ? descriptionId : undefined"
    @cancel.prevent="!busy && emit('close')"
  >
    <div class="dialog-head">
      <h2 :id="titleId">{{ title }}</h2>
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
    <p v-if="description" :id="descriptionId">{{ description }}</p>
    <slot />
    <div v-if="$slots.actions" class="dialog-actions">
      <slot name="actions" />
    </div>
  </dialog>
</template>
