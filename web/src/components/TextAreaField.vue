<script setup lang="ts">
defineProps<{
  id: string;
  label: string;
  modelValue: string;
  error?: string;
  hint?: string;
  disabled?: boolean;
  rows?: number;
}>();
defineEmits<{ "update:modelValue": [value: string] }>();
</script>
<template>
  <div class="field">
    <label :for="id">{{ label }}</label
    ><textarea
      class="resize-none"
      :id="id"
      :rows="rows || 4"
      :value="modelValue"
      :disabled="disabled"
      :aria-invalid="!!error"
      :aria-describedby="id + '-help'"
      @input="
        $emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)
      "
    /><small :id="id + '-help'" :class="{ invalid: error }">{{
      error || hint || "\u00a0"
    }}</small>
  </div>
</template>
