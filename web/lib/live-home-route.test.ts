import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const webRoot = join(dirname(fileURLToPath(import.meta.url)), "..");

test("the live home route mounts ClassicTerminal and never AuthGate", () => {
  const page = readFileSync(join(webRoot, "app/page.tsx"), "utf8");
  assert.match(page, /ClassicTerminal/);
  assert.doesNotMatch(page, /AuthGate|MudPortal|auth-gate|mud-portal/);
});

test("MudPortal does not render the Supabase web login card", () => {
  const portal = readFileSync(join(webRoot, "components/mud-portal.tsx"), "utf8");
  assert.match(portal, /ClassicTerminal/);
  assert.doesNotMatch(portal, /AuthGate|auth-gate/);
});
