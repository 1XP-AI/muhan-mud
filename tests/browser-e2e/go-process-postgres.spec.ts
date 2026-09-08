import { expect, test, type Page } from "@playwright/test";

const characterName = process.env.MUHAN_BROWSER_CHARACTER_NAME ?? "BrowserAlice";
const gamePassword = process.env.MUHAN_BROWSER_GAME_PASSWORD ?? "pw1234";

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
}

async function reloginAndLook(page: Page): Promise<void> {
  await page.reload();
  await waitForLoginPrompt(page);
  await submitAndWait(page, characterName, "암호");
  await submitAndWait(page, gamePassword, "== 브라우저 광장 ==");
  await submitAndWait(page, "봐", "실제 Go 서버와 PostgreSQL");
  await expect(page.locator(".xterm-screen")).not.toContainText(gamePassword);
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
