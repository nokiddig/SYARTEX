import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

// Helper for the onInteractOutside close check of Sheet / Dialog (drawers and dialogs).
//
// Background: Radix popups inside a drawer (Select dropdown, DropdownMenu, Popover, etc.) portal outside the drawer.
// When a popup is open and the user clicks the overlay / outside the drawer to dismiss it, that one pointerdown is handled by both
// the Select and the Sheet DismissableLayer. Select closes first (a discrete event that React flushes synchronously), so by the time
// the Sheet's handler runs, the popup's data-state has already flipped to closed. Detecting whether a popup is open 'right now' is
// therefore unreliable (verified in practice).
//
// Correct approach: Radix's pointerdown listener runs in the bubble phase. We record in the capture phase (earlier than it)
// whether a popup was open at that moment, and onInteractOutside reads that recorded value to decide whether to allow the close.
function isRadixOverlayOpenNow(): boolean {
  if (typeof document === "undefined") return false;
  return !!document.querySelector(
    [
      "[data-slot='select-trigger'][data-state='open']",
      "[data-slot='select-content'][data-state='open']",
      "[role='listbox'][data-state='open']",
      "[data-radix-popper-content-wrapper]",
      "[aria-expanded='true'][data-state='open']",
    ].join(","),
  );
}

let overlayOpenAtLastPointerDown = false;
if (typeof document !== "undefined") {
  document.addEventListener(
    "pointerdown",
    () => {
      overlayOpenAtLastPointerDown = isRadixOverlayOpenNow();
    },
    true, // capture: record before the Radix bubble-phase pointerdown handlers run
  );
}

// radixOverlayWasOpenAtPointerDown returns whether a Radix popup was open when the most recent pointerdown occurred.
// The drawer/dialog uses this: when a popup is open and the overlay is clicked, only the popup closes, not the drawer itself.
export function radixOverlayWasOpenAtPointerDown(): boolean {
  return overlayOpenAtLastPointerDown;
}

// copyText writes text to the clipboard; returns whether it succeeded.
// Background: navigator.clipboard is only available in secure contexts (HTTPS / localhost). Accessed via IP + HTTP it is undefined,
// in which case we fall back to execCommand("copy").
export async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Fall back to the fallback approach
    }
  }
  try {
    const textarea = document.createElement("textarea");
    textarea.value = text;
    textarea.style.position = "fixed";
    textarea.style.left = "-9999px";
    textarea.style.top = "0";
    document.body.appendChild(textarea);
    textarea.focus();
    textarea.select();
    const ok = document.execCommand("copy");
    document.body.removeChild(textarea);
    return ok;
  } catch {
    return false;
  }
}

export const getInitials = (str: string): string => {
  if (typeof str !== "string" || !str.trim()) return "?";

  return (
    str
      .trim()
      .split(/\s+/)
      .filter(Boolean)
      .map((word) => word[0])
      .join("")
      .toUpperCase() || "?"
  );
};

export function formatCurrency(
  amount: number,
  opts?: {
    currency?: string;
    locale?: string;
    minimumFractionDigits?: number;
    maximumFractionDigits?: number;
    noDecimals?: boolean;
  },
) {
  const { currency = "USD", locale = "en-US", minimumFractionDigits, maximumFractionDigits, noDecimals } = opts ?? {};

  const formatOptions: Intl.NumberFormatOptions = {
    style: "currency",
    currency,
    minimumFractionDigits: noDecimals ? 0 : minimumFractionDigits,
    maximumFractionDigits: noDecimals ? 0 : maximumFractionDigits,
  };

  return new Intl.NumberFormat(locale, formatOptions).format(amount);
}
