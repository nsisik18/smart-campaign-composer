<script setup lang="ts">
import HealthBadge from './components/HealthBadge.vue'
import CampaignWorkspace from './components/CampaignWorkspace.vue'
</script>

<template>
  <main class="mx-auto max-w-5xl space-y-6 p-6">
    <header class="flex items-center justify-between">
      <h1 class="text-xl font-semibold">Smart Campaign Composer</h1>
      <HealthBadge />
    </header>
    <CampaignWorkspace />
  </main>
</template>
