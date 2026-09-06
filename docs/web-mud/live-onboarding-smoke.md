# Live onboarding browser smoke

This is a test-only, operator-run Playwright harness for the identity-migration onboarding path. It has no local `webServer`, creates no browser by default, and makes no network or external-service call unless every guard below is explicitly provided.

## Required approval and fixtures

Do not use a personal account, an arbitrary character name, or an arbitrary legacy character. Before running, an operator must prepare two single-use fixtures:

- A pre-created web account with an empty roster and one approved, globally unique provision character name.
- A different pre-created web account with an empty roster, plus one imported-and-unclaimed legacy character and its known legacy password.

The C/Gateway provision path is not a one-name check. It preserves the legacy name confirmation and `[enter]` gate before reserving the supplied name, accepts the existing wizard's gender, class, stats, weapon, alignment, race, and game-password inputs, waits for C to emit `SAVED`, lets Gateway finalize ownership, and then lets Gateway send C `COMMIT` followed by `ACTIVATED`. The browser test sends the name, confirmation, and an empty line in that order; it then waits for the resulting provisioned control, returns to the active roster, and opens a normal authenticated game session as admission evidence.

The legacy game password is currently stored in the legacy player file and must differ from the web-login password.

The harness requires the following environment variables. `MUHAN_LIVE_ONBOARDING_SMOKE_DURABLE_DATA_APPROVAL` is deliberately separate from the enable flag: a run can create durable provisioning/claim data only after the operator has supplied that approval value in an approved maintenance window.

```text
MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED=true
MUHAN_LIVE_ONBOARDING_SMOKE_DURABLE_DATA_APPROVAL=approved-durable-onboarding-data
MUHAN_LIVE_ONBOARDING_SMOKE_GLOBAL_UNIQUENESS_APPROVAL=confirmed-global-uniqueness
MUHAN_LIVE_ONBOARDING_SMOKE_BASE_URL=https://approved-muhan-host.example
MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_EMAIL=<pre-created-empty-roster-account>
MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEB_PASSWORD=<that-account-password>
MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CHARACTER_NAME=<approved-single-use-name>
MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GENDER=<approved-existing-wizard-answer>
MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CLASS=<approved-existing-wizard-answer>
MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_STATS=<approved-existing-wizard-answer>
MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEAPON=<approved-existing-wizard-answer>
MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_ALIGNMENT=<approved-existing-wizard-answer>
MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_RACE=<approved-existing-wizard-answer>
MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GAME_PASSWORD=<approved-new-game-password-different-from-web-login-password>
MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_EMAIL=<pre-created-empty-roster-account>
MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_WEB_PASSWORD=<that-account-password>
MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_CHARACTER_NAME=<pre-created-unclaimed-legacy-name>
MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_LEGACY_PASSWORD=<that-character-password>
```

Run only after the above preparation and approval:

```sh
pnpm exec playwright test --config=tests/browser-e2e/live-onboarding-smoke.config.ts --project=chromium
```

The provision case signs in only to the named existing account and sends only the supplied character name, the fixed legacy confirmation, its empty `[enter]`, and the supplied existing-wizard answers; it never uses the sign-up UI, generates a name, or invents an answer. It proves the `SAVED` → finalize → `COMMIT` → `ACTIVATED` path with the browser's provisioned control, then proves the named active roster entry and a normal authenticated game admission. The claim case signs in only to its named existing account and consumes only the supplied legacy-name/password fixture; it proves the same resulting active-roster and admission evidence.

## Safe default and operator checks

With no variables, `web/lib/live-onboarding-smoke.ts` marks the suite skipped before browser creation. Enabling the flag without every named fixture, using a non-HTTPS target, omitting either approval, reusing provision/claim web accounts or character names, or supplying blank, control-character, placeholder, malformed-email, or unsafe-character-name values also skips it; configuration tests cover those cases. Fixture values are never included in the guard's skip reason.

Before a real run, obtain the designated operator's approval for the fixture accounts/names, every wizard input and password, and durable-data impact; confirm the deployed onboarding feature is enabled; confirm that both account emails and both character names are globally unique with an authorized live query; then set `MUHAN_LIVE_ONBOARDING_SMOKE_GLOBAL_UNIQUENESS_APPROVAL` only after that check. The harness intentionally does not make a live uniqueness query itself, so this remains an explicit operator precondition. Arrange post-run ownership/fixture cleanup, and do not put fixture values in shell history, CI logs, Playwright traces, screenshots, or test reports; this config disables trace, screenshot, video, and retained test output.
