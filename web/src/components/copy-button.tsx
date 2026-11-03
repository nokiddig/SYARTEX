"use client";

import * as React from "react";

import { CheckIcon, CopyIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { cn, copyText } from "@/lib/utils";

type CopyButtonProps = {
  // Text to copy; the button is disabled when empty.
  text: string | null | undefined;
  // Toast message shown on successful copy; defaults to the localized "Copied".
  successMessage?: string;
  label?: React.ReactNode;
  size?: React.ComponentProps<typeof Button>["size"];
  variant?: React.ComponentProps<typeof Button>["variant"];
  className?: string;
};

// Unified "copy to clipboard" button: built-in success/failure feedback, and
// falls back automatically on non-secure HTTP contexts (see copyText).
export function CopyButton({
  text,
  successMessage,
  label,
  size = "sm",
  variant = "outline",
  className,
}: CopyButtonProps) {
  const t = useTranslations("copyButton");
  const resolvedSuccessMessage = successMessage ?? t("copied");
  const resolvedLabel = label ?? t("copy");
  const [copied, setCopied] = React.useState(false);
  const timer = React.useRef<ReturnType<typeof setTimeout> | null>(null);

  React.useEffect(() => {
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, []);

  async function handleCopy() {
    if (!text) return;
    const ok = await copyText(text);
    if (ok) {
      setCopied(true);
      toast.success(resolvedSuccessMessage);
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error(t("copyFailed"));
    }
  }

  return (
    <Button
      type="button"
      size={size}
      variant={variant}
      className={cn(className)}
      disabled={!text}
      onClick={handleCopy}
    >
      {copied ? <CheckIcon /> : <CopyIcon />}
      {resolvedLabel}
    </Button>
  );
}
