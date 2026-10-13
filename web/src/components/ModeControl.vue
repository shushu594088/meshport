<script setup lang="ts">
defineProps<{ modelValue: string; disabled?: boolean }>();
defineEmits<{ "update:modelValue": [value: string] }>();
const modes = [
  ["rule", "规则"],
  ["global", "全局"],
  ["direct", "直连"],
];
</script>
<template>
  <fieldset class="mode-control">
    <legend>代理模式</legend>
    <label
      v-for="[value, label] in modes"
      :key="value"
      :class="{ selected: modelValue === value }"
      ><input
        type="radio"
        name="mode"
        :value="value"
        :checked="modelValue === value"
        :disabled="disabled"
        @change="$emit('update:modelValue', value)"
      />{{ label }}</label
    >
  </fieldset>
</template>
