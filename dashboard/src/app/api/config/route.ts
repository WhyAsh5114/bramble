import { deviceLabels } from "@/lib/config";

// Read at request time, not build time — see lib/config.ts's doc comment.
export async function GET() {
  return Response.json({ deviceLabels: deviceLabels() });
}
