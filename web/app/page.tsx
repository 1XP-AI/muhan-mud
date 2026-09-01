import { MudPortal } from "@/components/mud-portal";
import { readPublicConfig } from "@/lib/config";

// Public deployment coordinates are supplied by the running container, not
// baked into the browser bundle while the image is built.
export const dynamic = "force-dynamic";

export default function HomePage() {
  return <MudPortal configResult={readPublicConfig()} />;
}
