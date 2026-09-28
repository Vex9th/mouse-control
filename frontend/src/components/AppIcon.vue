<script setup lang="ts">
defineProps<{ name: 'mouse' | 'refresh' | 'arrow' | 'check' | 'alert' | 'info' | 'link' | 'bolt' | 'tune' | 'wave' | 'close' | 'chevron' | 'lock' }>()
</script>
<template>
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <template v-if="name === 'mouse'"><rect x="6.5" y="2.5" width="11" height="19" rx="5.5"/><path d="M12 6v4m-5.5 2h11"/></template>
    <template v-else-if="name === 'refresh'"><path d="M20 10a8 8 0 0 0-13.8-5L3 8m0-5v5h5M4 14a8 8 0 0 0 13.8 5L21 16m-5 0h5v5"/></template>
    <template v-else-if="name === 'arrow'"><path d="M5 12h14m-5-5 5 5-5 5"/></template>
    <template v-else-if="name === 'check'"><path d="m5 12 4 4L19 6"/></template>
    <template v-else-if="name === 'alert'"><path d="m10.3 3.8-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.7-3.2l-8-14a2 2 0 0 0-3.4 0Z"/><path d="M12 9v5m0 3h.01"/></template>
    <template v-else-if="name === 'info'"><circle cx="12" cy="12" r="9"/><path d="M12 11v6m0-10h.01"/></template>
    <template v-else-if="name === 'link'"><path d="m10 13 4-4m-6 7-1.5 1.5a4 4 0 0 1-5.5-5.8l5-5A4 4 0 0 1 12 7m0 10a4 4 0 0 0 6 0l5-5a4 4 0 0 0-5.5-5.5L16 8" transform="translate(0 0) scale(.9)"/></template>
    <template v-else-if="name === 'bolt'"><path d="m13 2-9 12h7l-1 8 10-13h-7l0-7Z"/></template>
    <template v-else-if="name === 'tune'"><path d="M5 3v9m0 4v5M12 3v3m0 4v11m7-18v12m0 4v2"/><path d="M2 12h6m1-6h6m1 9h6"/></template>
    <template v-else-if="name === 'wave'"><path d="M2 12h4l3-8 6 16 3-8h4"/></template>
    <template v-else-if="name === 'close'"><path d="m6 6 12 12M6 18 18 6"/></template>
    <template v-else-if="name === 'chevron'"><path d="m8 5 7 7-7 7"/></template>
    <template v-else-if="name === 'lock'"><rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V6a4 4 0 0 1 8 0v4m-4 4v3"/></template>
  </svg>
</template>
