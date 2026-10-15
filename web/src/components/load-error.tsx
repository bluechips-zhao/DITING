"use client";

import { Button } from "@/components/ui/button";

export function LoadError({ message, retry, busy = false }: {
  message: string;
  retry: () => void;
  busy?: boolean;
}) {
  return (
    <div role="alert" className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
      <span className="min-w-0 break-words">{message}</span>
      <Button variant="outline" size="sm" onClick={retry} disabled={busy}>
        {busy ? "正在重试…" : "重试"}
      </Button>
    </div>
  );
}
