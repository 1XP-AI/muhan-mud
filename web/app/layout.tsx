import type { Metadata } from "next";
import type { ReactNode } from "react";

import "@xterm/xterm/css/xterm.css";
import "./globals.css";

export const metadata: Metadata = {
  title: "무한대전 · Web MUD",
  description: "브라우저에서 다시 열리는 무한대전 텍스트 세계",
};

export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="ko">
      <body>{children}</body>
    </html>
  );
}
