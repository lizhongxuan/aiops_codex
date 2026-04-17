import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";

function asArray(value) {
  return Array.isArray(value) ? value : [];
}

function defaultGetItemId(item) {
  return String(item?.id || "");
}

function bottomDistance(el) {
  if (!el) return 0;
  return Math.max(el.scrollHeight - el.scrollTop - el.clientHeight, 0);
}

function isNearBottom(el, threshold = 100) {
  return bottomDistance(el) <= threshold;
}

function resolveUnreadAnchorId(items, lastReadId, getItemId) {
  const list = asArray(items);
  if (!list.length) return "";
  if (!lastReadId) return getItemId(list[list.length - 1]);
  const lastReadIndex = list.findIndex((item) => getItemId(item) === lastReadId);
  if (lastReadIndex >= 0 && lastReadIndex < list.length - 1) {
    return getItemId(list[lastReadIndex + 1]);
  }
  return getItemId(list[list.length - 1]);
}

export function useChatScrollState({
  scrollContainer,
  scrollContent,
  items,
  signature,
  getItemId = defaultGetItemId,
  threshold = 100,
} = {}) {
  const isPinnedToBottom = ref(true);
  const unreadCount = ref(0);
  const unreadAnchorId = ref("");
  const lastReadItemId = ref("");
  let contentResizeObserver = null;
  let resizeSyncHandle = 0;
  let userScrolledAt = 0;

  function scheduleResizeSync(callback) {
    if (typeof window !== "undefined" && typeof window.requestAnimationFrame === "function") {
      return window.requestAnimationFrame(callback);
    }
    return window.setTimeout(callback, 16);
  }

  function cancelResizeSync(handle) {
    if (!handle) return;
    if (typeof window !== "undefined" && typeof window.cancelAnimationFrame === "function") {
      window.cancelAnimationFrame(handle);
      return;
    }
    window.clearTimeout(handle);
  }

  const showUnreadPill = computed(() => unreadCount.value > 0 && !isPinnedToBottom.value);

  function markRead() {
    const list = asArray(items?.value);
    const lastItem = list[list.length - 1];
    lastReadItemId.value = lastItem ? getItemId(lastItem) : "";
    unreadCount.value = 0;
    unreadAnchorId.value = "";
  }

  function scrollToBottom(force = false) {
    const el = scrollContainer?.value;
    if (!el || (!force && !isPinnedToBottom.value)) return;
    el.scrollTop = el.scrollHeight;
    if (force) {
      isPinnedToBottom.value = true;
      markRead();
    }
  }

  function handleScroll(event) {
    const el = event?.target || scrollContainer?.value;
    const pinned = isNearBottom(el, threshold);
    isPinnedToBottom.value = pinned;
    if (!pinned) {
      userScrolledAt = Date.now();
    }
    if (pinned) {
      markRead();
    }
  }

  function jumpToLatest() {
    scrollToBottom(true);
  }

  watch(
    signature,
    async (_value, previousValue) => {
      await nextTick();
      const list = asArray(items?.value);
      if (!list.length) {
        markRead();
        return;
      }
      if (previousValue === undefined) {
        scrollToBottom(true);
        return;
      }
      if (isPinnedToBottom.value) {
        scrollToBottom(true);
        return;
      }
      unreadCount.value += 1;
      if (!unreadAnchorId.value) {
        unreadAnchorId.value = resolveUnreadAnchorId(list, lastReadItemId.value, getItemId);
      }
    },
    { deep: true, immediate: true },
  );

  onMounted(() => {
    nextTick(() => scrollToBottom(true));
    if (typeof ResizeObserver !== "undefined" && scrollContent?.value) {
      contentResizeObserver = new ResizeObserver(() => {
        // Sync on the next frame so streamed updates stay smooth without waiting for a coarse debounce.
        if (resizeSyncHandle) {
          cancelResizeSync(resizeSyncHandle);
        }
        resizeSyncHandle = scheduleResizeSync(() => {
          resizeSyncHandle = 0;
          if (Date.now() - userScrolledAt < 1500 && !isPinnedToBottom.value) return;
          scrollToBottom();
        });
      });
      contentResizeObserver.observe(scrollContent.value);
    }
  });

  onBeforeUnmount(() => {
    if (resizeSyncHandle) {
      cancelResizeSync(resizeSyncHandle);
      resizeSyncHandle = 0;
    }
    if (contentResizeObserver) {
      contentResizeObserver.disconnect();
      contentResizeObserver = null;
    }
  });

  return {
    isPinnedToBottom,
    unreadCount,
    unreadAnchorId,
    showUnreadPill,
    scrollToBottom,
    handleScroll,
    jumpToLatest,
    markRead,
  };
}
