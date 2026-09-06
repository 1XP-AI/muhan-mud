import assert from "node:assert/strict";
import { type ChildProcess } from "node:child_process";
import { EventEmitter } from "node:events";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  signalWebStackProcessTree,
  stopWebStackServer,
  waitForWebStackExit,
  waitForWebStackServer,
} from "./web-stack-ui.js";
import { cleanupFailure, runCleanupSteps } from "./lifecycle.js";

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

test("web stack exit wait observes a recorded signal-code race without waiting for a missed event", async () => {
  const child = fakeChild({ exitCode: null, signalCode: "SIGTERM" });
  const timers = {
    setTimeout: () => { throw new Error("an exited child must not receive a timer"); },
    clearTimeout: () => undefined,
  };

  assert.deepEqual(await waitForWebStackExit(child, 100, timers), [null, "SIGTERM"]);
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

test("web stack cleanup accepts a SIGTERM signal-code race from the detached process group", async () => {
  const child = fakeChild({ pid: 4312, exitCode: null, signalCode: null });

  await stopWebStackServer(
    { baseUrl: "http://127.0.0.1:1", child },
    {
      signalProcessTree: () => {
        Object.assign(child, { exitCode: null, signalCode: "SIGTERM" });
        throw Object.assign(new Error("no such process"), { code: "ESRCH" });
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

test("web stack cleanup preserves an unsuccessful exit observed during SIGKILL fallback", async () => {
  const child = fakeChild({ pid: 4312, exitCode: null, signalCode: null });
  let timeoutCallback: (() => void) | undefined;

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
      signalProcessTree: (_child, signal) => {
        if (signal === "SIGKILL") Object.assign(child, { exitCode: 1, signalCode: null });
      },
    },
  );
  timeoutCallback?.();
  await assert.rejects(stopping, /web server exited unexpectedly \(1\)/);
});

test("web stack readiness bounds a hung fetch with an AbortSignal", async () => {
  const child = fakeChild({ exitCode: null, signalCode: null });
  const callbacks: Array<() => void> = [];
  let signal: AbortSignal | undefined;
  let timerCalls = 0;
  const now = [0, 0, 0, 10];
  const ready = waitForWebStackServer(
    { baseUrl: "http://127.0.0.1:1", child },
    {
      startupTimeoutMs: 10,
      requestTimeoutMs: 5,
      now: () => now.shift() ?? 10,
      fetch: async (_url, init) => {
        signal = init?.signal ?? undefined;
        return await new Promise<Response>(() => undefined);
      },
      timers: {
        setTimeout: (callback: () => void, _timeoutMs: number) => {
          timerCalls += 1;
          if (timerCalls === 1) callbacks.push(callback);
          else callback();
          return { id: timerCalls } as unknown as ReturnType<typeof setTimeout>;
        },
        clearTimeout: (_handle: ReturnType<typeof setTimeout>) => undefined,
      },
    },
  );
  callbacks.shift()?.();
  await assert.rejects(ready, /web server readiness request exceeded 5ms/);
  assert.equal(signal?.aborted, true);
});

test("stack teardown runs Gateway, MUD, and evidence after web cleanup fails", async () => {
  const calls: string[] = [];
  const failures = await runCleanupSteps([
    { name: "web", run: async () => { calls.push("web"); throw new Error("web cleanup failed"); } },
    { name: "gateway", run: async () => { calls.push("gateway"); } },
    { name: "mud", run: async () => { calls.push("mud"); } },
    { name: "evidence", run: async () => { calls.push("evidence"); } },
  ]);

  assert.deepEqual(calls, ["web", "gateway", "mud", "evidence"]);
  assert.equal(cleanupFailure(failures)?.message, "stack-e2e web cleanup failed");
});

test("web stack cleanup reports an already-unsuccessful server exit", async () => {
  await assert.rejects(
    () => stopWebStackServer({ baseUrl: "http://127.0.0.1:1", child: fakeChild({ exitCode: 1 }) }),
    /web server exited unexpectedly \(1\)/,
  );
});

test("browser claim acceptance enters the C password through the rendered xterm keyboard path", async () => {
  const webRunner = await readFile(new URL("./web-stack-ui.ts", import.meta.url), "utf8");
  const claimAcceptance = webRunner.slice(webRunner.indexOf("const claimContext"));

  assert.match(webRunner, /async function submitOnboardingXtermInput/);
  assert.match(webRunner, /getByLabel\("캐릭터 온보딩 터미널"\)/);
  assert.match(webRunner, /textarea\.xterm-helper-textarea/);
  assert.match(webRunner, /toBeFocused\(\)/);
  assert.match(webRunner, /page\.keyboard\.type\(value\)/);
  assert.match(webRunner, /page\.keyboard\.press\("Enter"\)/);
  assert.match(claimAcceptance, /submitOnboardingXtermInput\(claimPage, claim\.characterName\)/);
  assert.match(claimAcceptance, /claimTerminal\)\.toContainText\(\/암호를 넣어 주십시요\//);
  assert.match(claimAcceptance, /submitOnboardingXtermInput\(claimPage, claim\.gamePassword\)/);
  assert.match(claimAcceptance, /assertRosterThenAdmission\(claimPage, claim\.characterName\)/);
  assert.doesNotMatch(claimAcceptance, /게임 비밀번호 입력/);
});

test("runner starts a dedicated process group, targets pnpm descendants, and retains cleanup failures", async () => {
  const [webRunner, stackHarness, ciWorkflow] = await Promise.all([
    readFile(new URL("./web-stack-ui.ts", import.meta.url), "utf8"),
    readFile(new URL("./stack-e2e.test.ts", import.meta.url), "utf8"),
    readFile(new URL("../../.github/workflows/ci.yml", import.meta.url), "utf8"),
  ]);

  assert.match(webRunner, /detached: process\.platform !== "win32"/);
  assert.match(webRunner, /pnpm[\s\S]*detached: process\.platform !== "win32"/);
  assert.match(webRunner, /signalProcessTree\(server\.child, "SIGTERM"\)/);
  assert.match(webRunner, /signalProcessTree\(server\.child, "SIGKILL"\)/);
  assert.match(stackHarness, /runCleanupSteps/);
  assert.match(stackHarness, /if \(web\) await stopWebStackServer\(web\)/);
  assert.match(stackHarness, /if \(gateway\) await closeGatewayBounded\(gateway\)/);
  assert.match(stackHarness, /if \(mud\) await stopMudDuringFailure\(mud\)/);
  assert.match(stackHarness, /!scenarioFailed && teardownFailure/);

  const lifecycleInvocation = "pnpm --dir services/gateway exec tsx --test ../../tests/stack-e2e/web-stack-ui.lifecycle.test.ts";
  assert.match(ciWorkflow, /- name: Web stack lifecycle unit contract/);
  assert.ok(ciWorkflow.includes(lifecycleInvocation), "CI must invoke the deterministic lifecycle test directly");
  assert.ok(
    ciWorkflow.indexOf(lifecycleInvocation) < ciWorkflow.indexOf("./scripts/run-stack-e2e.sh"),
    "CI must run the deterministic lifecycle test before the Docker-backed stack acceptance",
  );
});
