<script setup>
import { computed, ref, watch } from "vue";
import { experiencePacks, toneClass } from "../data/opsWorkspace";

const query = ref("");
const selectedPackId = ref(experiencePacks[0]?.id || "");

const filteredPacks = computed(() => {
  const keyword = query.value.trim().toLowerCase();
  if (!keyword) return experiencePacks;
  return experiencePacks.filter((pack) => {
    return [pack.name, pack.summary, pack.version, pack.purpose, ...(pack.resources || [])]
      .join(" ")
      .toLowerCase()
      .includes(keyword);
  });
});

watch(
  filteredPacks,
  (packs) => {
    if (!packs.length) {
      selectedPackId.value = "";
      return;
    }
    if (!packs.some((pack) => pack.id === selectedPackId.value)) {
      selectedPackId.value = packs[0].id;
    }
  },
  { immediate: true }
);

const selectedPack = computed(() => {
  return filteredPacks.value.find((pack) => pack.id === selectedPackId.value) || filteredPacks.value[0] || null;
});
</script>

<template>
  <section class="ops-page">
    <div class="ops-page-inner">
      <header class="ops-page-header">
        <div>
          <h2 class="ops-page-title">经验包库</h2>
          <p class="ops-page-subtitle">把运行成功经验、playbook 与主机画像绑定成可复用的运维资产。</p>
        </div>
      </header>

      <div class="ops-scope-bar">
        <div class="ops-scope-left">
          <label class="ops-search">
            <input v-model="query" type="text" placeholder="搜索经验包、场景、版本、来源" />
          </label>
          <span class="ops-pill is-info">场景: nginx</span>
          <span class="ops-pill">风险: low</span>
          <span class="ops-pill">来源: verified</span>
          <span class="ops-pill">适用 OS: Linux</span>
        </div>
        <div class="ops-actions">
          <button class="ops-button primary">新建经验包</button>
        </div>
      </div>

      <div class="ops-grid ops-grid-packs">
        <article class="ops-card">
          <div class="ops-card-header">
            <div>
              <h3 class="ops-card-title">经验包列表</h3>
              <p class="ops-card-subtitle">默认按最近使用与成功率排序</p>
            </div>
          </div>

          <div class="ops-pack-list">
            <button
              v-for="pack in filteredPacks"
              :key="pack.id"
              type="button"
              class="ops-pack-item"
              :class="{ 'is-active': selectedPack && selectedPack.id === pack.id }"
              @click="selectedPackId = pack.id"
            >
              <div class="ops-pack-mark" :class="toneClass(pack.confidenceTone)"></div>
              <div class="ops-pack-copy">
                <strong>{{ pack.name }}</strong>
                <span>{{ pack.summary }}</span>
                <div class="ops-chip-row">
                  <span class="ops-mini-pill">{{ pack.version }}</span>
                  <span class="ops-pill" :class="toneClass(pack.confidenceTone)">{{ pack.confidence }}</span>
                </div>
              </div>
              <span class="ops-pack-meta">{{ pack.bindings }}</span>
            </button>

            <div v-if="!filteredPacks.length" class="ops-empty">
              没有命中的经验包，试试更宽松的关键词。
            </div>
          </div>
        </article>

        <article class="ops-card ops-sidebar-card" v-if="selectedPack">
          <div class="ops-card-header">
            <div>
              <h3 class="ops-card-title">包详情</h3>
              <p class="ops-card-subtitle">{{ selectedPack.name }} · {{ selectedPack.version }}</p>
            </div>
          </div>

          <div class="ops-badge-row">
            <span class="ops-pill" :class="toneClass(selectedPack.statusTone)">{{ selectedPack.status }}</span>
            <span class="ops-pill">{{ selectedPack.risk }}</span>
            <span class="ops-pill">{{ selectedPack.platform }}</span>
          </div>

          <div class="ops-detail-block">
            <span class="ops-subcard-label">用途</span>
            <p class="ops-detail-copy">{{ selectedPack.purpose }}</p>
          </div>

          <div class="ops-subcard">
            <span class="ops-subcard-label">版本演进</span>
            <div class="ops-version-row">
              <template v-for="(version, index) in selectedPack.versionTrail" :key="version.label">
                <span class="ops-version-dot" :class="toneClass(version.state)"></span>
                <span class="ops-version-label">{{ version.label }}</span>
                <span v-if="index < selectedPack.versionTrail.length - 1" class="ops-version-line"></span>
              </template>
            </div>
            <p class="ops-detail-copy">{{ selectedPack.versionNote }}</p>
          </div>

          <div class="ops-subcard">
            <span class="ops-subcard-label">关联资源</span>
            <ul class="ops-list ops-mono-list">
              <li v-for="resource in selectedPack.resources" :key="resource">{{ resource }}</li>
            </ul>
          </div>

          <div class="ops-card-actions">
            <button class="ops-button primary">加载到主 Agent</button>
            <button class="ops-button ghost">附加到主机组</button>
            <button class="ops-button ghost">创建新版本</button>
          </div>
        </article>
      </div>
    </div>
  </section>
</template>
