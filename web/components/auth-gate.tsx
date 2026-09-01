"use client";

import type { SupabaseClient } from "@supabase/supabase-js";
import { FormEvent, useState } from "react";

interface AuthGateProps {
  supabase: SupabaseClient;
}

type AuthMode = "sign-in" | "sign-up";

export function AuthGate({ supabase }: AuthGateProps) {
  const [mode, setMode] = useState<AuthMode>("sign-in");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const switchMode = (nextMode: AuthMode) => {
    setMode(nextMode);
    setMessage(null);
    setError(null);
  };

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setPending(true);
    setMessage(null);
    setError(null);

    try {
      if (mode === "sign-in") {
        const result = await supabase.auth.signInWithPassword({
          email: email.trim(),
          password,
        });

        if (result.error) {
          throw result.error;
        }
      } else {
        const result = await supabase.auth.signUp({
          email: email.trim(),
          password,
        });

        if (result.error) {
          throw result.error;
        }

        if (!result.data.session) {
          setMessage("확인 메일을 보냈습니다. 메일의 링크를 연 뒤 입장해 주세요.");
        }
      }
    } catch (caught) {
      setError(
        caught instanceof Error
          ? caught.message
          : "계정 정보를 확인하지 못했습니다.",
      );
    } finally {
      setPending(false);
    }
  };

  return (
    <main className="auth-layout">
      <section className="auth-story" aria-labelledby="auth-title">
        <p className="eyebrow">MUHAN NETWORK · 1996 / 2026</p>
        <h1 id="auth-title">
          글자로 열린 세계,
          <br />다시 성문 앞에서.
        </h1>
        <p className="auth-lead">
          무한대전의 방, 전투, 사람들은 그대로입니다. 브라우저 접속과 계정
          관문만 새로 세웠습니다.
        </p>
        <dl className="auth-facts">
          <div>
            <dt>표현</dt>
            <dd>UTF-8 터미널</dd>
          </div>
          <div>
            <dt>세계</dt>
            <dd>단일 영속 월드</dd>
          </div>
          <div>
            <dt>접속</dt>
            <dd>암호화 WebSocket</dd>
          </div>
        </dl>
      </section>

      <section className="auth-panel" aria-label="웹 계정 인증">
        <div className="gate-mark" aria-hidden="true">
          <span />
          <b>無限</b>
          <span />
        </div>
        <div className="auth-mode" role="tablist" aria-label="인증 방식">
          <button
            aria-selected={mode === "sign-in"}
            className={mode === "sign-in" ? "active" : undefined}
            onClick={() => switchMode("sign-in")}
            role="tab"
            type="button"
          >
            입장
          </button>
          <button
            aria-selected={mode === "sign-up"}
            className={mode === "sign-up" ? "active" : undefined}
            onClick={() => switchMode("sign-up")}
            role="tab"
            type="button"
          >
            계정 만들기
          </button>
        </div>

        <form onSubmit={submit}>
          <label>
            웹 계정 이메일
            <input
              autoComplete="email"
              inputMode="email"
              onChange={(event) => setEmail(event.target.value)}
              placeholder="player@example.com"
              required
              type="email"
              value={email}
            />
          </label>
          <label>
            비밀번호
            <input
              autoComplete={
                mode === "sign-in" ? "current-password" : "new-password"
              }
              minLength={8}
              onChange={(event) => setPassword(event.target.value)}
              required
              type="password"
              value={password}
            />
          </label>

          {error ? (
            <p className="form-notice error" role="alert">
              {error}
            </p>
          ) : null}
          {message ? (
            <p className="form-notice" role="status">
              {message}
            </p>
          ) : null}

          <button className="primary-action" disabled={pending} type="submit">
            {pending
              ? "관문 확인 중…"
              : mode === "sign-in"
                ? "성문 열기"
                : "입장권 만들기"}
          </button>
        </form>

        <p className="legacy-note">
          웹 계정 인증 뒤 기존 MUD 캐릭터 이름과 비밀번호를 한 번 더 입력합니다.
        </p>
      </section>
    </main>
  );
}
