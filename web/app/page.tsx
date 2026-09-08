import { ClassicTerminal } from "@/components/classic-terminal";

// Public deployment coordinates are supplied by the running container, not
// baked into the browser bundle while the image is built.
export const dynamic = "force-dynamic";

export default function HomePage() {
  return <ClassicTerminal url={process.env.MUD_GO_GATEWAY_URL ?? null} />;
}
