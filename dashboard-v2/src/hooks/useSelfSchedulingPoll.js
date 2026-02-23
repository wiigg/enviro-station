import { useEffect } from "react";

export function useSelfSchedulingPoll(task, intervalMs) {
  useEffect(() => {
    let closed = false;
    let timerId = null;
    const abortController = new AbortController();

    async function run() {
      try {
        await task({
          signal: abortController.signal,
          isClosed: () => closed
        });
      } finally {
        if (!closed) {
          timerId = window.setTimeout(run, intervalMs);
        }
      }
    }

    run();

    return () => {
      closed = true;
      abortController.abort();
      if (timerId) {
        window.clearTimeout(timerId);
      }
    };
  }, [intervalMs, task]);
}
