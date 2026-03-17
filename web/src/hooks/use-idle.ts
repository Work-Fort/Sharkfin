import type { SharkfinClient } from '@workfort/sharkfin-client';

/**
 * Track user activity and set presence state.
 * Sets 'active' on connect and activity, 'idle' after 5 minutes of inactivity.
 * Returns a cleanup function — caller is responsible for invoking it on unmount.
 */
export function useIdleDetection(client: SharkfinClient): () => void {
  const IDLE_TIMEOUT = 5 * 60 * 1000;
  let timer: ReturnType<typeof setTimeout>;

  function setActive() {
    clearTimeout(timer);
    client.setState('active').catch(() => {});
    timer = setTimeout(() => {
      client.setState('idle').catch(() => {});
    }, IDLE_TIMEOUT);
  }

  setActive();

  const events = ['mousemove', 'keydown', 'click', 'scroll'];
  const throttledSetActive = throttle(setActive, 30_000);
  events.forEach((e) => document.addEventListener(e, throttledSetActive, { passive: true }));

  function onVisibilityChange() {
    if (document.hidden) {
      client.setState('idle').catch(() => {});
    } else {
      setActive();
    }
  }
  document.addEventListener('visibilitychange', onVisibilityChange);

  return () => {
    clearTimeout(timer);
    events.forEach((e) => document.removeEventListener(e, throttledSetActive));
    document.removeEventListener('visibilitychange', onVisibilityChange);
  };
}

function throttle(fn: () => void, ms: number): () => void {
  let last = 0;
  return () => {
    const now = Date.now();
    if (now - last >= ms) {
      last = now;
      fn();
    }
  };
}
