import { cn } from "@/lib/utils";

// Deliberately plain text, not a badge/pill — the word itself is the signal
// (Vercel design guidance: pair color with a non-color cue, and reserve
// pills for actions, not metadata).
export function StatusText({
  tone,
  children,
}: {
  tone: "positive" | "negative" | "neutral";
  children: React.ReactNode;
}) {
  return (
    <span
      className={cn(
        "text-sm",
        tone === "positive" && "text-foreground",
        tone === "negative" && "text-destructive",
        tone === "neutral" && "text-muted-foreground",
      )}
    >
      {children}
    </span>
  );
}
