"use client";

import type { Session, SupabaseClient } from "@supabase/supabase-js";
import { useEffect, useState } from "react";

export type PresenceStatus = "offline" | "joining" | "online" | "error";

interface PresenceInfo {
  count: number | null;
  status: PresenceStatus;
}

interface LobbyPresencePayload {
  user_id?: string;
  online_at?: string;
}

export function useLobbyPresence(
  supabase: SupabaseClient | null,
  session: Session | null,
): PresenceInfo {
  const [info, setInfo] = useState<PresenceInfo>({
    count: null,
    status: "offline",
  });

  useEffect(() => {
    if (!supabase || !session) {
      setInfo({ count: null, status: "offline" });
      return;
    }

    let cancelled = false;
    const channel = supabase.channel("mud:lobby", {
      config: {
        private: true,
        presence: { key: session.user.id },
      },
    });

    const syncCount = () => {
      const state = channel.presenceState<LobbyPresencePayload>();
      const userIds = new Set<string>();

      for (const entries of Object.values(state)) {
        for (const entry of entries) {
          if (entry.user_id) {
            userIds.add(entry.user_id);
          }
        }
      }

      if (!cancelled) {
        setInfo({ count: userIds.size, status: "online" });
      }
    };

    const join = async () => {
      setInfo({ count: null, status: "joining" });

      try {
        await supabase.realtime.setAuth(session.access_token);
        channel.on("presence", { event: "sync" }, syncCount);
        channel.subscribe(async (status) => {
          if (cancelled) {
            return;
          }

          if (status === "SUBSCRIBED") {
            const result = await channel.track({
              user_id: session.user.id,
              online_at: new Date().toISOString(),
            });

            if (result !== "ok") {
              setInfo({ count: null, status: "error" });
            }
          } else if (
            status === "CHANNEL_ERROR" ||
            status === "TIMED_OUT"
          ) {
            setInfo({ count: null, status: "error" });
          }
        });
      } catch {
        if (!cancelled) {
          setInfo({ count: null, status: "error" });
        }
      }
    };

    void join();

    return () => {
      cancelled = true;
      void supabase.removeChannel(channel);
    };
  }, [session, supabase]);

  return info;
}
