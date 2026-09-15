export interface PublicConfig {
  supabaseUrl: string;
  supabasePublishableKey: string;
  gatewayUrl: string;
  onboardingEnabled: boolean;
}

export interface ConfigResult {
  config: PublicConfig | null;
  missing: string[];
  error: string | null;
}

export interface PublicConfigEnvironment {
  NODE_ENV?: string;
  SUPABASE_PUBLIC_URL?: string;
  SUPABASE_PUBLISHABLE_KEY?: string;
  MUD_GATEWAY_URL?: string;
  NEXT_PUBLIC_SUPABASE_URL?: string;
  NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY?: string;
  NEXT_PUBLIC_MUD_GATEWAY_URL?: string;
  MUD_ONBOARDING_ENABLED?: string;
  NEXT_PUBLIC_MUD_ONBOARDING_ENABLED?: string;
}

/**
 * Read browser-safe coordinates on the Next.js server at request time.
 * NEXT_PUBLIC_* remains a local-development fallback, but Kubernetes uses the
 * non-prefixed variables so one immutable image can be promoted between hosts.
 */
export function readPublicConfig(
  environment: PublicConfigEnvironment = process.env,
): ConfigResult {
  const values = {
    SUPABASE_PUBLIC_URL:
      environment.SUPABASE_PUBLIC_URL ??
      environment.NEXT_PUBLIC_SUPABASE_URL,
    SUPABASE_PUBLISHABLE_KEY:
      environment.SUPABASE_PUBLISHABLE_KEY ??
      environment.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY,
    MUD_GATEWAY_URL:
      environment.MUD_GATEWAY_URL ??
      environment.NEXT_PUBLIC_MUD_GATEWAY_URL,
  };
  const missing = Object.entries(values)
    .filter(([, value]) => !value)
    .map(([name]) => name);

  if (missing.length > 0) {
    return { config: null, missing, error: null };
  }

  try {
    const onboardingValue =
      environment.MUD_ONBOARDING_ENABLED ??
      environment.NEXT_PUBLIC_MUD_ONBOARDING_ENABLED;
    if (
      onboardingValue !== undefined &&
      onboardingValue !== "true" &&
      onboardingValue !== "false"
    ) {
      throw new Error(
        "MUD_ONBOARDING_ENABLED는 true 또는 false여야 합니다.",
      );
    }

    const supabaseUrl = new URL(values.SUPABASE_PUBLIC_URL!);
    const gatewayUrl = new URL(values.MUD_GATEWAY_URL!);
    const production = environment.NODE_ENV === "production";

    if (
      (production && supabaseUrl.protocol !== "https:") ||
      (!production && !["http:", "https:"].includes(supabaseUrl.protocol))
    ) {
      throw new Error(
        production
          ? "운영 Supabase URL은 https:// 주소여야 합니다."
          : "Supabase URL은 http:// 또는 https:// 주소여야 합니다.",
      );
    }

    if (
      (production && gatewayUrl.protocol !== "wss:") ||
      (!production && !["ws:", "wss:"].includes(gatewayUrl.protocol))
    ) {
      throw new Error(
        production
          ? "운영 MUD Gateway URL은 wss:// 주소여야 합니다."
          : "MUD Gateway URL은 ws:// 또는 wss:// 주소여야 합니다.",
      );
    }

    return {
      config: {
        supabaseUrl: supabaseUrl.toString().replace(/\/$/, ""),
        supabasePublishableKey: values.SUPABASE_PUBLISHABLE_KEY!,
        gatewayUrl: gatewayUrl.toString(),
        onboardingEnabled: onboardingValue === "true",
      },
      missing: [],
      error: null,
    };
  } catch (error) {
    return {
      config: null,
      missing: [],
      error: error instanceof Error ? error.message : "환경설정을 읽지 못했습니다.",
    };
  }
}
