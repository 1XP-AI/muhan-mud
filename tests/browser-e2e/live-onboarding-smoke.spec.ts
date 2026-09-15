import { expect, test, type Page } from "@playwright/test";

import { readLiveOnboardingSmokeConfig } from "../../web/lib/live-onboarding-smoke";

const liveSmoke = readLiveOnboardingSmokeConfig();

async function signInToPrecreatedEmptyFixture(
  page: Page,
  email: string,
  webPassword: string,
): Promise<void> {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /글자로 열린 세계/ })).toBeVisible();
  await page.getByLabel("웹 계정 이메일").fill(email);
  await page.getByLabel("비밀번호").fill(webPassword);
  // This is the existing-account sign-in path only. The harness never clicks
  // the sign-up tab or sends a sign-up request.
  await page.getByRole("button", { name: "성문 열기" }).click();
  await expect(page.getByText("이 계정에 연결된 캐릭터가 없습니다")).toBeVisible();
}

async function submitOnboardingInput(page: Page, value: string): Promise<void> {
  const command = page.getByLabel("온보딩 입력");
  await expect(command).toBeEnabled();
  await command.fill(value);
  await page.getByRole("button", { name: "보내기" }).click();
  await expect(command).toHaveValue("");
}

async function assertActiveRosterAndGatewayAdmission(
  page: Page,
  characterName: string,
): Promise<void> {
  await expect(
    page.getByRole("heading", { name: "입장할 캐릭터를 고르세요" }),
  ).toBeVisible();
  await expect(page.getByText(characterName, { exact: true })).toBeVisible();
  await expect(page.getByText(/ACTIVE/)).toBeVisible();

  const character = page.locator('input[name="mud-character"]');
  await expect(character).toHaveCount(1);
  await character.check();
  await page.getByRole("button", { name: "게임 입장" }).click();

  await expect(page.locator(".selected-character-bar strong")).toHaveText(
    characterName,
  );
  await expect(page.getByText("무한대전 세계와 연결됐습니다.")).toBeVisible();
}

test.describe("operator-approved live onboarding smoke", () => {
  // Do this at declaration time so an unmet guard skips every test before a
  // browser/page fixture is created. No default invocation reaches a network.
  test.skip(!liveSmoke.shouldRun, liveSmoke.skipReason);

  test("provision completes the named fixture's saved, finalized, and committed wizard lifecycle", async ({ page }) => {
    const fixture = liveSmoke.config!.provision;
    await signInToPrecreatedEmptyFixture(page, fixture.email, fixture.webPassword);

    await page.getByRole("button", { name: "새 캐릭터 만들기" }).click();
    await expect(page.getByRole("heading", { name: "새 캐릭터 만들기" })).toBeVisible();
    // This matches the real C/Gateway lifecycle: C requires legacy name
    // confirmation and an explicit blank [enter] before it reserves the
    // fixture. The operator-provided wizard inputs cause C SAVED; Gateway
    // finalizes durable ownership and sends C COMMIT then ACTIVATED before it
    // emits `provisioned`.
    await submitOnboardingInput(page, fixture.characterName);
    await submitOnboardingInput(page, "예");
    await submitOnboardingInput(page, "");
    await submitOnboardingInput(page, fixture.gender);
    await submitOnboardingInput(page, fixture.characterClass);
    await submitOnboardingInput(page, fixture.stats);
    await submitOnboardingInput(page, fixture.weapon);
    await submitOnboardingInput(page, fixture.alignment);
    await submitOnboardingInput(page, fixture.race);

    const gamePassword = page.getByLabel("게임 비밀번호 입력");
    await expect(gamePassword).toBeVisible();
    await gamePassword.fill(fixture.gamePassword);
    await page.getByRole("button", { name: "보내기" }).click();
    await expect(page.getByText("새 캐릭터가 활성화됐습니다.")).toBeVisible();

    // Return to the roster only after the provisioned control has confirmed
    // the full C SAVED -> Gateway finalize -> C COMMIT -> C ACTIVATED
    // transaction.
    await page.getByRole("button", { name: "취소하고 캐릭터 선택" }).click();
    await assertActiveRosterAndGatewayAdmission(page, fixture.characterName);
  });

  test("claim consumes only the named pre-created legacy fixture", async ({ page }) => {
    const fixture = liveSmoke.config!.claim;
    await signInToPrecreatedEmptyFixture(page, fixture.email, fixture.webPassword);

    await page.getByRole("button", { name: "기존 캐릭터 연결" }).click();
    await expect(page.getByRole("heading", { name: "기존 캐릭터 연결" })).toBeVisible();
    await submitOnboardingInput(page, fixture.characterName);

    const legacyPassword = page.getByLabel("게임 비밀번호 입력");
    await expect(legacyPassword).toBeVisible();
    await legacyPassword.fill(fixture.legacyPassword);
    await page.getByRole("button", { name: "보내기" }).click();

    await assertActiveRosterAndGatewayAdmission(page, fixture.characterName);
  });
});
