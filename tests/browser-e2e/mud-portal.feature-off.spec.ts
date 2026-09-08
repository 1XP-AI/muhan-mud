import { expect, test } from "@playwright/test";

test("terminal-only entry has no web account surface", async ({ page }) => {
  const boundaryRequests: string[] = [];
  page.on("request", (request) => {
    const url = request.url();
    if (url.includes("/auth/v1/") || url.includes("/rest/v1/")) boundaryRequests.push(url);
  });

  await page.goto("/");

  await expect(page).toHaveTitle("무한대전 · Web MUD");
  const terminal = page.getByLabel("무한대전 게임 터미널");
  await expect(terminal).toHaveCount(1);
  await expect(page.getByRole("textbox", { name: "Terminal input" })).toBeFocused();
  await expect(page.locator(".xterm-screen")).toContainText("무한대전 · 터미널 접속");
  await expect(page.locator(".xterm-screen")).toContainText("게임 서버 주소가 설정되지 않았습니다.");

  await expect(page.locator("main input[type=email], main input[type=password], main button")).toHaveCount(0);
  await expect(page.getByRole("heading")).toHaveCount(0);
  expect(boundaryRequests).toEqual([]);
});
