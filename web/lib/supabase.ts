import { createClient, type SupabaseClient } from "@supabase/supabase-js";

import type { PublicConfig } from "@/lib/config";

let browserClient: SupabaseClient | null = null;
let browserClientKey = "";

export function getBrowserSupabase(config: PublicConfig): SupabaseClient {
  const nextKey = `${config.supabaseUrl}:${config.supabasePublishableKey}`;

  if (!browserClient || browserClientKey !== nextKey) {
    browserClient = createClient(
      config.supabaseUrl,
      config.supabasePublishableKey,
      {
        auth: {
          autoRefreshToken: true,
          detectSessionInUrl: true,
          persistSession: true,
        },
      },
    );
    browserClientKey = nextKey;
  }

  return browserClient;
}
