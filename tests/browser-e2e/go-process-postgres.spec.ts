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
  await submitAndWait(page, "정보", "[엔터]를 누르세요. 그만보시려면 [.]을 치세요: ");
  await submitAndWait(page, "", "주문: 없음.");
  await expect(page.locator(".xterm-screen")).toContainText("당신은 현재 달성한 임무가 없습니다.");
  await submitAndWait(page, "정보", "[엔터]를 누르세요. 그만보시려면 [.]을 치세요: ");
  await submitAndWait(page, ".", "중단되었습니다.");
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
