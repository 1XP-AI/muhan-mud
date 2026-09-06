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
  assert.match(webRunner, /signalWebStackProcessTree\(server\.child, "SIGTERM"\)/);
  assert.match(webRunner, /signalWebStackProcessTree\(server\.child, "SIGKILL"\)/);
  assert.match(stackHarness, /if \(web\) await stopWebStackServer\(web\)/);
  assert.doesNotMatch(stackHarness, /stopWebStackServer\(web\)\.catch\(/);
});
