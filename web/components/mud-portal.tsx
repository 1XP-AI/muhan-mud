"use client";

import type { Session } from "@supabase/supabase-js";
import dynamic from "next/dynamic";
import { useCallback, useEffect, useMemo, useState } from "react";

import { AuthGate } from "@/components/auth-gate";
import { CharacterRoster } from "@/components/character-roster";
import type { OnboardingMode } from "@/lib/onboarding-contract";
import type { GatewayStatus } from "@/components/mud-terminal";
import type { ConfigResult } from "@/lib/config";
import {
  useCharacterRoster,
} from "@/lib/character-roster";
import { shouldOpenGatewaySocket } from "@/lib/gateway-contract";
import {
  completeOnboardingHandoff,
  resolvePlayAdmission,
  type OnboardingCompletion,
  type PlayAdmissionHandoff,
} from "@/lib/play-admission";
import { getBrowserSupabase } from "@/lib/supabase";
import { useLobbyPresence } from "@/lib/use-lobby-presence";

const OnboardingTerminal = dynamic(
  () =>
    import("@/components/onboarding-terminal").then(
      (module) => module.OnboardingTerminal,
    ),
  {
    ssr: false,
    loading: () => <div className="terminal-loading">온보딩 터미널을 준비하는 중…</div>,
  },
);

const MudTerminal = dynamic(
  () => import("@/components/mud-terminal").then((module) => module.MudTerminal),
  {
    ssr: false,
    loading: () => <div className="terminal-loading">터미널을 준비하는 중…</div>,
  },
);

const initialGatewayStatus: GatewayStatus = {
  state: "idle",
  detail: "아직 통로를 열지 않았습니다.",
  attempt: 0,
};

const statusLabel: Record<GatewayStatus["state"], string> = {
  idle: "대기",
  connecting: "통로 개방",
  authenticating: "입장권 확인",
  ready: "성문 개방",
  provisioned: "캐릭터 활성",
  retrying: "재접속",
  closed: "닫힘",
  error: "점검 필요",
};

interface MudPortalProps {
  configResult: ConfigResult;
}

export function MudPortal({ configResult }: MudPortalProps) {
  const supabase = useMemo(
    () =>
      configResult.config ? getBrowserSupabase(configResult.config) : null,
    [configResult.config],
  );
  const [session, setSession] = useState<Session | null>(null);
  const [authLoading, setAuthLoading] = useState(Boolean(supabase));
  const [gatewayStatus, setGatewayStatus] = useState(initialGatewayStatus);
  const [activeCharacterId, setActiveCharacterId] = useState<string | null>(null);
  const [activeOwnerId, setActiveOwnerId] = useState<string | null>(null);
  const [onboardingFlow, setOnboardingFlow] = useState<{
    mode: OnboardingMode;
    correlationId: string;
  } | null>(null);
  const [onboardingHandoff, setOnboardingHandoff] =
    useState<PlayAdmissionHandoff | null>(null);
  const presence = useLobbyPresence(supabase, session);
  const roster = useCharacterRoster(supabase, session?.user.id ?? null);

  useEffect(() => {
    if (!supabase) {
      setAuthLoading(false);
      return;
    }

    let active = true;
    void supabase.auth.getSession().then(({ data }) => {
      if (active) {
        setSession(data.session);
        setAuthLoading(false);
      }
    });
    const { data } = supabase.auth.onAuthStateChange((_event, nextSession) => {
      if (active) {
        setSession(nextSession);
        setAuthLoading(false);
      }
    });

    return () => {
      active = false;
      data.subscription.unsubscribe();
    };
  }, [supabase]);

  useEffect(() => {
    setActiveCharacterId(null);
    setActiveOwnerId(null);
    setGatewayStatus(initialGatewayStatus);
    setOnboardingFlow(null);
    setOnboardingHandoff(null);
  }, [session?.user.id]);

  useEffect(() => {
    const handoff = resolvePlayAdmission(
      session?.user.id ?? "",
      roster.status,
      roster.characters,
      onboardingHandoff,
    );
    if (!handoff) return;

    setActiveCharacterId(handoff.characterId);
    setActiveOwnerId(handoff.ownerUserId);
    setOnboardingHandoff(null);
  }, [onboardingHandoff, roster.characters, roster.status, session?.user.id]);

  const onGatewayStatus = useCallback((status: GatewayStatus) => {
    setGatewayStatus(status);
  }, []);

  const terminateGateway = useCallback(() => {
    setActiveCharacterId(null);
    setActiveOwnerId(null);
    setGatewayStatus(initialGatewayStatus);
    roster.retry();
  }, [roster.retry]);

  const cancelOnboarding = useCallback(() => {
    setOnboardingFlow(null);
    setGatewayStatus(initialGatewayStatus);
    roster.retry();
  }, [roster.retry]);

  const completedOnboarding = useCallback((
    completion: OnboardingCompletion,
    characterId: string,
  ) => {
    const ownerUserId = session?.user.id;
    if (!ownerUserId) return;

    setOnboardingHandoff((existing) =>
      completeOnboardingHandoff(ownerUserId, characterId, completion, existing),
    );
    setOnboardingFlow(null);
    roster.retry();
  }, [roster.retry, session?.user.id]);

  const provisionedOnboarding = useCallback((characterId: string) => {
    completedOnboarding("provisioned", characterId);
  }, [completedOnboarding]);

  const terminateOnboarding = useCallback(() => {
    setOnboardingFlow(null);
    setActiveCharacterId(null);
    setActiveOwnerId(null);
    setGatewayStatus(initialGatewayStatus);
    roster.retry();
  }, [roster.retry]);

  const claimOnboarding = useCallback((characterId: string) => {
    completedOnboarding("claimed", characterId);
  }, [completedOnboarding]);

  if (!configResult.config || !supabase) {
    return (
      <main className="setup-screen">
        <p className="eyebrow">CONFIGURATION REQUIRED</p>
        <h1>성문 좌표가 비어 있습니다.</h1>
        <p>
          웹 컨테이너에 아래 공개 환경변수를 설정하세요. Kubernetes에서는
          Helm chart가 Secret과 도메인 값으로 주입합니다.
        </p>
        <ul>
          {configResult.missing.map((name) => (
            <li key={name}>
              <code>{name}</code>
            </li>
          ))}
        </ul>
        {configResult.error ? (
          <p className="form-notice error" role="alert">
            {configResult.error}
          </p>
        ) : null}
        <p>
          로컬 실행 예시는 <code>web/.env.example</code>에 있습니다.
        </p>
      </main>
    );
  }

  if (authLoading) {
    return (
      <main className="boot-screen" aria-live="polite">
        <span className="boot-cursor" aria-hidden="true" />
        <p>무한대전 입장 기록을 확인하는 중…</p>
      </main>
    );
  }

  if (!session) {
    return <AuthGate supabase={supabase} />;
  }

  const identityReady = Boolean(session.user.id);
  const passageReady = !["idle", "connecting", "error", "closed"].includes(
    gatewayStatus.state,
  );
  const worldReady = gatewayStatus.state === "ready" || gatewayStatus.state === "provisioned";
  const terminalAllowed = shouldOpenGatewaySocket(
    roster.status,
    activeOwnerId === session.user.id ? activeCharacterId : null,
    roster.characters,
  );
  const activeCharacter = terminalAllowed
    ? roster.characters.find((character) => character.id === activeCharacterId)
    : undefined;
  const onboardingEnabled = configResult.config.onboardingEnabled;

  return (
    <main className="game-shell">
      <header className="game-header">
        <div className="wordmark">
          <span className="wordmark-seal" aria-hidden="true">
            無限
          </span>
          <div>
            <p>MUHAN NETWORK</p>
            <h1>무한대전</h1>
          </div>
        </div>
        <div className="account-strip">
          <span title={session.user.email}>{session.user.email}</span>
          <button onClick={() => void supabase.auth.signOut()} type="button">
            나가기
          </button>
        </div>
      </header>

      <div className="gate-layout">
        <aside className="world-gauge" aria-label="접속 상태">
          <div className="gauge-heading">
            <span>성문 상태</span>
            <strong data-state={gatewayStatus.state}>
              {statusLabel[gatewayStatus.state]}
            </strong>
          </div>

          <ol className="gate-stages">
            <li data-complete={identityReady}>
              <span>신원</span>
              <b>웹 계정 확인</b>
            </li>
            <li data-complete={passageReady}>
              <span>통로</span>
              <b>WSS 인증</b>
            </li>
            <li data-complete={worldReady}>
              <span>세계</span>
              <b>C MUD 연결</b>
            </li>
          </ol>

          <div className="presence-meter">
            <span>문 앞 접속자</span>
            <strong>{presence.count ?? "—"}</strong>
            <small data-status={presence.status}>
              {presence.status === "online"
                ? "Presence 연결"
                : presence.status === "error"
                  ? "Presence 점검 필요"
                  : "집계 중"}
            </small>
          </div>

          <p className="gateway-detail" aria-live="polite">
            {gatewayStatus.detail}
          </p>
        </aside>

        <section className="terminal-panel" aria-label="게임 화면">
          <div className="terminal-chrome">
            <span>{onboardingFlow ? "ONBOARDING / MUHAN-01" : "WORLD / MUHAN-01"}</span>
            <span className="secure-indicator">
              <i aria-hidden="true" /> AUTH + WSS
            </span>
          </div>
          {activeCharacter ? (
            <div className="terminal-content">
              <div className="selected-character-bar">
                <span>
                  선택됨 <strong>{activeCharacter.legacy_name}</strong>
                </span>
                <button
                  onClick={() => {
                    setActiveCharacterId(null);
                    setActiveOwnerId(null);
                  }}
                  type="button"
                >
                  캐릭터 변경
                </button>
              </div>
              <MudTerminal
                accessToken={session.access_token}
                characterId={activeCharacter.id}
                gatewayUrl={configResult.config.gatewayUrl}
                onStatus={onGatewayStatus}
                onTerminated={terminateGateway}
              />
            </div>
          ) : onboardingFlow ? (
            <OnboardingTerminal
              accessToken={session.access_token}
              correlationId={onboardingFlow.correlationId}
              gatewayUrl={configResult.config.gatewayUrl}
              mode={onboardingFlow.mode}
              onCancel={cancelOnboarding}
              onClaimed={claimOnboarding}
              onProvisioned={provisionedOnboarding}
              onStatus={onGatewayStatus}
              onTerminated={terminateOnboarding}
            />
          ) : (
            <CharacterRoster
              characters={roster.characters}
              error={roster.error}
              onEnter={() => {
                if (roster.selectedId) {
                  setActiveCharacterId(roster.selectedId);
                  setActiveOwnerId(session.user.id);
                }
              }}
              onRetry={roster.retry}
              onSelect={roster.selectCharacter}
              selectedId={roster.selectedId}
              status={roster.status}
              onboardingEnabled={onboardingEnabled}
              onStartOnboarding={(mode) => {
                if (!onboardingEnabled || roster.status !== "empty") return;
                // One correlation identifies a user-started flow and is reused
                // only by the bounded reconnects inside this terminal.
                setOnboardingFlow({ mode, correlationId: crypto.randomUUID() });
              }}
            />
          )}
        </section>
      </div>
    </main>
  );
}
