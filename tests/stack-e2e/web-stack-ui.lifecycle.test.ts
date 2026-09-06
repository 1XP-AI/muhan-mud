import assert from "node:assert/strict";
import { type ChildProcess } from "node:child_process";
import { EventEmitter } from "node:events";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  signalWebStackProcessTree,
  stopWebStackServer,
  waitForWebStackExit,
} from "./web-stack-ui.js";

function fakeChild(properties: Partial<ChildProcess> = {}): ChildProcess {
  return Object.assign(new EventEmitter(), properties) as ChildProcess;
}

test("web stack termination signals the detached POSIX process group", () => {
  const calls: Array<{ pid: number; signal: NodeJS.Signals }> = [];
  const child = fakeChild({ pid: 4312 });

  signalWebStackProcessTree(
    child,
    "SIGTERM",
    "linux",
    ((pid: number, signal: NodeJS.Signals) => {
      calls.push({ pid, signal });
      return true;
    }) as typeof process.kill,
  );

  assert.deepEqual(calls, [{ pid: -4312, signal: "SIGTERM" }]);
});

test("web stack exit wait clears its bounded-wait timer after a normal exit", async () => {
  const child = fakeChild();
  const timer = { id: "web-exit" } as unknown as ReturnType<typeof setTimeout>;
  const cleared: Array<ReturnType<typeof setTimeout>> = [];
  const timers = {
    setTimeout: (_callback: () => void, _timeoutMs: number) => timer,
    clearTimeout: (handle: ReturnType<typeof setTimeout>) => { cleared.push(handle); },
  };

  const exited = waitForWebStackExit(child, 100, timers);
  child.emit("exit", 0, null);
  assert.deepEqual(await exited, [0, null]);
  assert.deepEqual(cleared, [timer]);
});

test("web stack exit wait removes its exit listener when its timeout wins", async () => {
  const child = fakeChild();
  let timeoutCallback: (() => void) | undefined;
  const timer = { id: "web-exit-timeout" } as unknown as ReturnType<typeof setTimeout>;
  const cleared: Array<ReturnType<typeof setTimeout>> = [];
  const timers = {
    setTimeout: (callback: () => void, _timeoutMs: number) => {
      timeoutCallback = callback;
      return timer;
    },
    clearTimeout: (handle: ReturnType<typeof setTimeout>) => { cleared.push(handle); },
  };

  const exited = waitForWebStackExit(child, 100, timers);
  assert.equal(child.listenerCount("exit"), 1);
  timeoutCallback?.();
  await assert.rejects(exited, /web server did not exit within 100ms/);
  assert.equal(child.listenerCount("exit"), 0);
  assert.deepEqual(cleared, [timer]);
});

test("web stack cleanup clears timed-out SIGTERM listeners before SIGKILL and after fallback exit", async () => {
  const child = fakeChild({ pid: 4312, exitCode: null });
  const callbacks: Array<() => void> = [];
  const signals: NodeJS.Signals[] = [];
  const timers = {
    setTimeout: (callback: () => void, _timeoutMs: number) => {
      callbacks.push(callback);
      return { id: callbacks.length } as unknown as ReturnType<typeof setTimeout>;
    },
    clearTimeout: (_handle: ReturnType<typeof setTimeout>) => undefined,
  };
  const stopping = stopWebStackServer(
    { baseUrl: "http://127.0.0.1:1", child },
    {
      graceTimeoutMs: 1,
      killTimeoutMs: 1,
      timers,
      signalProcessTree: (_child, signal) => {
        signals.push(signal);
        if (signal === "SIGKILL") assert.equal(child.listenerCount("exit"), 0);
      },
    },
  );

  assert.equal(child.listenerCount("exit"), 1);
  callbacks.shift()?.();
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(signals, ["SIGTERM", "SIGKILL"]);
  assert.equal(child.listenerCount("exit"), 1);
  child.emit("exit", 0, "SIGKILL");
  await stopping;
  assert.equal(child.listenerCount("exit"), 0);
});

test("web stack cleanup accepts a normal exit racing process-group SIGTERM", async () => {
  const child = fakeChild({ pid: 4312, exitCode: null });

  await stopWebStackServer(
    { baseUrl: "http://127.0.0.1:1", child },
    {
      signalProcessTree: () => {
        Object.assign(child, { exitCode: 0 });
        const error = Object.assign(new Error("no such process"), { code: "ESRCH" });
        throw error;
      },
    },
  );
});

test("web stack cleanup accepts a normal exit racing fallback process-group SIGKILL", async () => {
  const child = fakeChild({ pid: 4312, exitCode: null });
  let timeoutCallback: (() => void) | undefined;
  let signals = 0;

  const stopping = stopWebStackServer(
    { baseUrl: "http://127.0.0.1:1", child },
    {
      graceTimeoutMs: 1,
      timers: {
        setTimeout: (callback: () => void, _timeoutMs: number) => {
          timeoutCallback = callback;
          return { id: "timeout" } as unknown as ReturnType<typeof setTimeout>;
        },
        clearTimeout: (_handle: ReturnType<typeof setTimeout>) => undefined,
      },
      signalProcessTree: () => {
        signals += 1;
        if (signals === 2) {
          Object.assign(child, { exitCode: 0 });
          throw Object.assign(new Error("no such process"), { code: "ESRCH" });
        }
      },
    },
  );
  timeoutCallback?.();
  await stopping;
  assert.equal(signals, 2);
  assert.equal(child.listenerCount("exit"), 0);
});

test("web stack cleanup reports an already-unsuccessful server exit", async () => {
  await assert.rejects(
    () => stopWebStackServer({ baseUrl: "http://127.0.0.1:1", child: fakeChild({ exitCode: 1 }) }),
    /web server exited unsuccessfully/,
  );
});

test("runner starts a dedicated process group and does not swallow web cleanup failures", async () => {
  const [webRunner, stackHarness] = await Promise.all([
    readFile(new URL("./web-stack-ui.ts", import.meta.url), "utf8"),
    readFile(new URL("./stack-e2e.test.ts", import.meta.url), "utf8"),
  ]);

  assert.match(webRunner, /detached: process\.platform !== "win32"/);
  assert.match(webRunner, /signalProcessTree\(server\.child, "SIGTERM"\)/);
  assert.match(webRunner, /signalProcessTree\(server\.child, "SIGKILL"\)/);
  assert.match(stackHarness, /if \(web\) await stopWebStackServer\(web\)/);
  assert.doesNotMatch(stackHarness, /stopWebStackServer\(web\)\.catch\(/);
});
