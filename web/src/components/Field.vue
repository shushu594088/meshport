<script setup lang="ts">
defineProps<{
  id: string;
  label: string;
  modelValue: string | number;
  type?: string;
  error?: string;
  hint?: string;
  disabled?: boolean;
  autocomplete?: string;
}>();
defineEmits<{ "update:modelValue": [value: string] }>();
</script>
<template>
  <div class="field">
    <label :for="id">{{ label }}</label
    ><input
      :id="id"
      :type="type || 'text'"
      :value="modelValue"
      :disabled="disabled"
      :autocomplete="autocomplete || 'off'"
      :aria-invalid="!!error"
      :aria-describedby="id + '-help'"
      @input="
        $emit('update:modelValue', ($event.target as HTMLInputElement).value)
      "
    /><small :id="id + '-help'" :class="{ invalid: error }">{{
      error || hint || "\u00a0"
    }}</small>
  </div>
</template>
