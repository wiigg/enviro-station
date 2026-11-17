import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useReadingsData } from "./useReadingsData";

const START = 1_800_000_000_000;
const MINUTE = 60_000;

function sample(timestamp) {
  return {
    timestamp, device_id: "station", temperature: 20, pressure: 100_000, humidity: 45,
    oxidised: 1, reduced: 1, nh3: 1, pm1: 1, pm2: 1, pm10: 1
  };
}

function response(readings) {
  return { ok: true, text: async () => JSON.stringify({ readings }) };
}

describe("readings history hydration", () => {
  let stream;
  let fetchMock;
  let visibility;

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(START);
    visibility = "visible";
    vi.spyOn(document, "visibilityState", "get").mockImplementation(() => visibility);
    vi.stubGlobal("EventSource", class extends EventTarget {
      constructor() {
        super();
        stream = this;
      }
      close() {}
    });
    fetchMock = vi.fn(async () => response([]));
    vi.stubGlobal("fetch", fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  function durableRequests() {
    return fetchMock.mock.calls.filter(([url]) => new URL(url).searchParams.has("from"));
  }

  async function openDashboard() {
    const hook = renderHook(() => useReadingsData("http://localhost:8080"));
    await act(async () => {});
    return hook.result;
  }

  async function select(result, id) {
    await act(async () => result.current.setWindowId(id));
  }

  async function advance(minutes) {
    await act(async () => vi.advanceTimersByTimeAsync(minutes * MINUTE));
  }

  async function changeVisibility(value) {
    await act(async () => {
      visibility = value;
      document.dispatchEvent(new Event("visibilitychange"));
    });
  }

  it("keeps Live in memory and reconciles the selected window on its original batch deadline", async () => {
    const result = await openDashboard();
    expect(fetchMock.mock.calls.map(([url]) => new URL(url).searchParams.get("source")))
      .toEqual(["live"]);
    await select(result, "1h");
    expect(durableRequests()).toHaveLength(1);

    await advance(10);
    await select(result, "live");
    await select(result, "1h");
    expect(durableRequests()).toHaveLength(1);

    await advance(19);
    act(() => stream.dispatchEvent(new MessageEvent("reading", {
      data: JSON.stringify(sample(Date.now()))
    })));
    expect(result.current.readings.at(-1)?.timestamp).toBe(Date.now());
    await advance(1);
    expect(durableRequests()).toHaveLength(2);

    await select(result, "live");
    await advance(31);
    expect(durableRequests()).toHaveLength(2);
  });

  it("defers hidden history reads and refreshes on return only when due", async () => {
    const result = await openDashboard();
    await changeVisibility("hidden");
    await select(result, "7d");
    await advance(31);
    expect(durableRequests()).toHaveLength(0);

    await changeVisibility("visible");
    expect(durableRequests()).toHaveLength(1);
    await changeVisibility("hidden");
    await advance(10);
    await changeVisibility("visible");
    expect(durableRequests()).toHaveLength(1);
    await changeVisibility("hidden");
    await advance(25);
    expect(durableRequests()).toHaveLength(1);
    await changeVisibility("visible");
    expect(durableRequests()).toHaveLength(2);
  });

  it("keeps SSE samples received while awaiting an older history snapshot", async () => {
    const result = await openDashboard();
    let finishOverlay;
    const overlay = new Promise(resolve => { finishOverlay = resolve; });
    fetchMock
      .mockResolvedValueOnce(response([sample(START - 20 * MINUTE)]))
      .mockReturnValueOnce(overlay);
    await select(result, "1h");
    act(() => stream.dispatchEvent(new MessageEvent("reading", {
      data: JSON.stringify(sample(START))
    })));
    await act(async () => finishOverlay(response([sample(START - MINUTE)])));

    expect(result.current.readings.map(reading => reading.timestamp))
      .toEqual([START - 20 * MINUTE, START - MINUTE, START]);
  });

  it("retries a failed history load while visible and then resumes the batch cadence", async () => {
    const result = await openDashboard();
    fetchMock.mockRejectedValueOnce(new Error("History unavailable"));
    await select(result, "24h");
    expect(result.current.lastError).toBe("History unavailable");
    expect(durableRequests()).toHaveLength(1);

    await advance(1);
    expect(result.current.lastError).toBe("");
    expect(durableRequests()).toHaveLength(2);
    await advance(29);
    expect(durableRequests()).toHaveLength(2);
  });

  it("ignores an abandoned range response when switching windows", async () => {
    const result = await openDashboard();
    let finishRange;
    fetchMock.mockReturnValueOnce(new Promise(resolve => { finishRange = resolve; }));
    await select(result, "24h");
    const [, { signal }] = durableRequests()[0];
    await select(result, "live");
    expect(signal.aborted).toBe(true);
    await act(async () => finishRange(response([sample(START)])));
    expect(result.current.readings).toEqual([]);

    await select(result, "24h");
    expect(durableRequests()).toHaveLength(2);
  });
});
