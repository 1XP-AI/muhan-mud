/** Runtime coordinates understood by the terminal-only page. */
export interface GatewayRuntimeEnvironment {
  /** Preferred Go gateway coordinate supplied by the running web server. */
  MUD_GO_GATEWAY_URL?: string | null;
  /** Explicit compatibility fallback for existing gateway deployments. */
  MUD_GATEWAY_URL?: string | null;
}

function configuredValue(value: string | null | undefined): string | null {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
}

/**
 * Resolve the server-side runtime coordinate without baking it into the
 * browser bundle. An invalid preferred value is retained so the terminal can
 * report the configuration error instead of silently connecting elsewhere.
 */
export function resolveGatewayUrl(
  environment: GatewayRuntimeEnvironment,
): string | null {
  return (
    configuredValue(environment.MUD_GO_GATEWAY_URL) ??
    configuredValue(environment.MUD_GATEWAY_URL)
  );
}

export type GatewayUrlValidation =
  | { kind: "valid"; url: URL }
  | { kind: "missing"; url: null }
  | { kind: "invalid"; url: null }
  | { kind: "mixed-content"; url: null };

/**
 * Validate the one WebSocket coordinate used by the terminal.
 *
 * Keeping the page protocol as an argument makes this helper independent of
 * browser globals and keeps HTTPS mixed-content protection in one place.
 */
export function validateGatewayUrl(
  value: string | null | undefined,
  pageProtocol: string,
): GatewayUrlValidation {
  const configured = configuredValue(value);
  if (configured === null) return { kind: "missing", url: null };

  let url: URL;
  try {
    url = new URL(configured);
  } catch {
    return { kind: "invalid", url: null };
  }

  if (url.protocol !== "ws:" && url.protocol !== "wss:") {
    return { kind: "invalid", url: null };
  }

  if (pageProtocol.trim().toLowerCase() === "https:" && url.protocol !== "wss:") {
    return { kind: "mixed-content", url: null };
  }

  return { kind: "valid", url };
}
