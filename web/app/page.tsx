import { ClassicTerminal } from "@/components/classic-terminal";
import { resolveGatewayUrl } from "@/lib/gateway-url";

// Public deployment coordinates are supplied by the running container, not
// baked into the browser bundle while the image is built.
export const dynamic = "force-dynamic";

export default function HomePage() {
  return (
    <ClassicTerminal
      url={resolveGatewayUrl({
        MUD_GO_GATEWAY_URL: process.env.MUD_GO_GATEWAY_URL,
        MUD_GATEWAY_URL: process.env.MUD_GATEWAY_URL,
      })}
    />
  );
}
