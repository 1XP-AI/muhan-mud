import { expect, test, type Page } from "@playwright/test";

const characterName = process.env.MUHAN_BROWSER_CHARACTER_NAME ?? "BrowserAlice";
const gamePassword = process.env.MUHAN_BROWSER_GAME_PASSWORD ?? "pw1234";
const existingCharacterName = process.env.MUHAN_BROWSER_EXISTING_CHARACTER_NAME ?? "BrowserOld";
const existingGamePassword = process.env.MUHAN_BROWSER_EXISTING_GAME_PASSWORD ?? "existingpw";
const infoPrompt = "[엔터]를 누르세요. 그만보시려면 [.]을 치세요: ";

async function submitLine(page: Page, value: string): Promise<void> {
  const input = page.locator("textarea.xterm-helper-textarea");
  await expect(input).toBeFocused();
  if (value !== "") {
    if (/^[\u0000-\u007f]*$/.test(value)) {
      await page.keyboard.type(value);
    } else {
      // xterm's composition helper reads the textarea value after
      // compositionend. Dispatch the same ordered events as a committed
      // Korean IME composition; keyboard.type/insertText alone does not
      // update TerminalLine in Chromium for these characters.
      await input.evaluate((element, text) => {
        element.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true, data: "" }));
        element.value = text;
        element.dispatchEvent(new CompositionEvent("compositionupdate", { bubbles: true, data: text }));
        element.dispatchEvent(new InputEvent("input", {
          bubbles: true,
          data: text,
          inputType: "insertCompositionText",
          isComposing: true,
        }));
        element.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: text }));
      }, value);
      // CompositionHelper finalizes on a zero-delay task after the browser
      // has committed the textarea value.
      await page.waitForTimeout(10);
    }
  }
  await page.keyboard.press("Enter");
}

async function submitAndWait(page: Page, value: string, expected: string): Promise<void> {
  await submitLine(page, value);
  await expect(page.locator(".xterm-screen")).toContainText(expected);
}

async function submitAndWaitForNewOccurrence(
  page: Page,
  value: string,
  expected: string,
): Promise<void> {
  const screen = page.locator(".xterm-screen");
  const before = (await screen.textContent()) ?? "";
  const previousCount = before.split(expected).length - 1;
  await submitLine(page, value);
  await expect.poll(async () => {
    const current = (await screen.textContent()) ?? "";
    return current.split(expected).length - 1;
  }).toBeGreaterThan(previousCount);
}

async function submitInfoAndWait(page: Page): Promise<void> {
  const screen = page.locator(".xterm-screen");
  const before = (await screen.textContent()) ?? "";
  const promptCount = before.split(infoPrompt).length - 1;
  await submitLine(page, "정보");
  await expect.poll(async () => {
    const current = (await screen.textContent()) ?? "";
    return current.split(infoPrompt).length - 1;
  }).toBeGreaterThan(promptCount);
}

async function waitForLoginPrompt(page: Page): Promise<void> {
  await expect(page.locator(".xterm-screen")).toContainText("당신의 이름은 무엇입니까?");
  await expect(page.locator("textarea.xterm-helper-textarea")).toBeFocused();
}

async function createCharacterAndEnterWorld(page: Page): Promise<void> {
  await waitForLoginPrompt(page);
  await submitAndWait(page, characterName, "새 캐릭터를 만드시겠습니까");
  await submitAndWait(page, "예", "[엔터]");
  await submitAndWait(page, "", "당신은 남자");
  await submitAndWait(page, "남", "직업");
  await submitAndWait(page, "4", "능력치");
  await submitAndWait(page, "12 10 12 10 10", "무기");
  await submitAndWait(page, "1", "성향");
  await submitAndWait(page, "선", "종족");
  await submitAndWait(page, "7", "새 암호");
  // With a real world connector the transport replaces the registration
  // acknowledgement with the first durable room scene in the same response.
  await submitAndWait(page, gamePassword, "== 브라우저 광장 ==");
  await expect(page.locator(".xterm-screen")).toContainText("== 브라우저 광장 ==");
  await expect(page.locator(".xterm-screen")).toContainText("실제 Go 서버와 PostgreSQL");
  await expect(page.locator(".xterm-screen")).not.toContainText(gamePassword);
  await submitAndWait(page, "줄임말 테스트 시간", "줄임말이 설정되었습니다.");
  await submitAndWait(page, "테스트", "현재 시간");
  await submitAndWaitForNewOccurrence(page, "!", "현재 시간");
  await submitAndWait(page, "환영", "이게임은 아직도 제작중입니다.");
  await submitAndWait(page, "설정 색", "색        :  사용 ");
  await submitAndWait(page, "열어 __missing_door__", "그런 출구는 없습니다.");
  await submitAndWait(page, "따 __missing_door__", "도둑만 자물쇠를 딸 수 있습니다.");
  await submitAndWait(page, "도움말 정보", "'정보'는");
  await submitAndWait(page, "표현 웹 테스트", "님이 웹 테스트");
  await submitAndWait(page, "외쳐 웹 테스트", "예. 좋습니다.");
  await submitAndWait(page, "검색", "아무것도 찾지 못했습니다.");
  await submitAndWait(page, "엿봐 Bob", "직업으로는");
  await submitAndWait(page, "보아 gob 2", "당신은 Goblin를 봅니다.");
  await submitInfoAndWait(page);
  await submitAndWait(page, ".", "중단되었습니다.");
  await submitInfoAndWait(page);
  await submitAndWait(page, "", "주문: 없음.");
  await expect(page.locator(".xterm-screen")).toContainText("당신은 현재 달성한 임무가 없습니다.");
}

async function reloginAndLook(page: Page): Promise<void> {
  await page.reload();
  await waitForLoginPrompt(page);
  await submitAndWait(page, characterName, "암호");
  await submitAndWait(page, gamePassword, "== 브라우저 광장 ==");
  await submitAndWait(page, "봐", "실제 Go 서버와 PostgreSQL");
  await expect(page.locator(".xterm-screen")).not.toContainText(gamePassword);
}

async function loginExistingCharacter(page: Page): Promise<void> {
  await page.goto("/");
  await waitForLoginPrompt(page);
  await submitAndWait(page, existingCharacterName, "암호");
  await submitAndWait(page, existingGamePassword, "== 브라우저 광장 ==");
  await expect(page.locator(".xterm-screen")).toContainText("실제 Go 서버와 PostgreSQL");
  await expect(page.locator(".xterm-screen")).not.toContainText(existingGamePassword);
}

async function beginExistingLogin(page: Page): Promise<void> {
  await page.goto("/");
  await waitForLoginPrompt(page);
  await submitAndWait(page, existingCharacterName, "암호");
}

test("real Go process and PostgreSQL survive browser signup, world admission, and relogin", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });

  await page.goto("/");
  await expect(page).toHaveTitle("무한대전 · Web MUD");
  await expect(page.getByLabel("무한대전 게임 터미널")).toHaveCount(1);
  await createCharacterAndEnterWorld(page);
  await reloginAndLook(page);

  expect(errors, `browser errors: ${errors.join(" | ")}`).toEqual([]);
});

test("pre-seeded canonical character admits once and rejects a duplicate session", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });

  await loginExistingCharacter(page);

  const duplicate = await page.context().newPage();
  duplicate.on("pageerror", (error) => errors.push(error.message));
  duplicate.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  try {
    await beginExistingLogin(duplicate);
    await submitAndWait(
      duplicate,
      existingGamePassword,
      "게임 입장을 완료하지 못했습니다. 잠시 후 다시 접속해 주세요.",
    );
    await submitAndWait(page, "설정 색", "색        :  사용 ");
  } finally {
    await duplicate.close();
  }

  expect(errors, `browser errors: ${errors.join(" | ")}`).toEqual([]);
});

test.describe("mobile terminal input", () => {
  test.use({
    deviceScaleFactor: 3,
    hasTouch: true,
    isMobile: true,
    viewport: { width: 390, height: 844 },
  });

  test("keeps the xterm focused and usable after a mobile viewport resize", async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("console", (message) => {
      if (message.type() === "error") errors.push(message.text());
    });

    await page.goto("/");
    await waitForLoginPrompt(page);
    const input = page.locator("textarea.xterm-helper-textarea");
    const terminal = page.getByLabel("무한대전 게임 터미널");
    await expect(terminal).toBeVisible();
    const viewport = page.viewportSize();
    const bounds = await terminal.boundingBox();
    expect(viewport?.width).toBe(390);
    expect(viewport?.height).toBe(844);
    expect(bounds?.height ?? 0).toBeGreaterThan(0);
    expect(bounds?.height ?? 0).toBeLessThanOrEqual(viewport?.height ?? 0);

    await page.evaluate(() => window.dispatchEvent(new Event("resize")));
    await expect(input).toBeFocused();
    await submitAndWait(page, existingCharacterName, "암호");
    await submitAndWait(page, existingGamePassword, "== 브라우저 광장 ==");
    await submitAndWait(page, "봐", "실제 Go 서버와 PostgreSQL");
    await expect(input).toBeFocused();
    await expect(page.locator("body")).not.toContainText(existingGamePassword);

    expect(errors, `browser errors: ${errors.join(" | ")}`).toEqual([]);
  });
});
