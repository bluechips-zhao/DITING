// Run only against an isolated, newly created database and application directory.
import assert from "node:assert/strict";
import crypto from "node:crypto";

const base = process.env.DITING_SMOKE_URL;
const version = process.env.DITING_SMOKE_VERSION;
assert.ok(base && version, "Set DITING_SMOKE_URL and DITING_SMOKE_VERSION explicitly");
assert.ok(["127.0.0.1", "localhost"].includes(new URL(base).hostname), "Smoke test requires loopback");
async function json(route, init) {
  const response = await fetch(base + route, { ...init, signal: AbortSignal.timeout(5000) });
  assert.equal(response.status, 200, `HTTP status: ${route}`);
  return response.json();
}

const deadline = Date.now() + 60000;
while (true) {
  try { await json("/api/health"); break; }
  catch (error) {
    if (Date.now() >= deadline) throw error;
    await new Promise(resolve => setTimeout(resolve, 500));
  }
}
assert.equal((await json("/api/health")).version, version);
assert.equal((await json("/api/auth/status")).initialized, false, "Refusing to initialize an existing installation");
assert.equal((await fetch(base + "/api/tasks")).status, 401);
for (const page of ["/login/", "/setup/", "/dashboard/", "/system/settings/"]) {
  assert.equal((await fetch(base + page)).status, 200, `Embedded page: ${page}`);
}
const initialized = await json("/api/auth/init", {
  method: "POST", headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ password: crypto.randomBytes(24).toString("hex") }),
});
assert.ok(initialized.token);
const headers = { Authorization: `Bearer ${initialized.token}` };
for (const [route, key] of [
  ["/api/tasks", "tasks"], ["/api/assets", "assets"], ["/api/companies", "companies"],
  ["/api/conversations", "conversations"], ["/api/exploration/findings", "findings"],
  ["/api/llm/profiles", "profiles"], ["/api/llm/pool", "chain"],
]) {
  const data = await json(route, { headers });
  const items = data === null || Array.isArray(data) ? data : data?.[key];
  assert.ok(items === null || Array.isArray(items), `Unexpected list shape: ${route}`);
  assert.equal(items?.length ?? 0, 0, `Fresh business list: ${route}`);
}
assert.equal((await json("/api/llm", { headers })).configured, false);
assert.equal((await json("/api/update/status", { headers })).progress.phase, "idle");
assert.equal((await fetch(base + "/api/update/status")).status, 401);
assert.equal((await fetch(base + "/api/llm/profiles/2", { method: "DELETE", headers })).status, 404);
console.log(`Fresh installation passed: ${version}; empty business data; authentication enforced; no LLM calls`);
