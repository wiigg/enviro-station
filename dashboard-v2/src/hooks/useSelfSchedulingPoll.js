import { useEffect } from "react";

function isPageHidden() {
  if (typeof document === "undefined") {
    return false;
  }

  return document.visibilityState === "hidden";
}

export function useSelfSchedulingPoll(task, intervalMs) {
  useEffect(() => {
    let closed = false;
    let timerId = null;
    let running = false;
    const abortController = new AbortController();

    function clearTimer() {
      if (timerId !== null) {
        window.clearTimeout(timerId);
        timerId = null;
      }
    }

    async function run() {
      if (closed || running || isPageHidden()) {
        return;
      }

      running = true;
      try {
        await task({
          signal: abortController.signal,
          isClosed: () => closed
        });
      } finally {
        running = false;
        if (!closed && !isPageHidden()) {
          timerId = window.setTimeout(run, intervalMs);
        }
      }
    }

    function handleVisibilityChange() {
      if (closed) {
        return;
      }

      if (isPageHidden()) {
        clearTimer();
        return;
      }

      clearTimer();
      run();
    }

    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", handleVisibilityChange);
    }

    if (!isPageHidden()) {
      run();
    }

    return () => {
      closed = true;
      abortController.abort();
      clearTimer();
      if (typeof document !== "undefined") {
        document.removeEventListener("visibilitychange", handleVisibilityChange);
      }
    };
  }, [intervalMs, task]);
}
