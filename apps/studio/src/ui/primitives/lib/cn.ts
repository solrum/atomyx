import clsx, { type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * Tailwind-aware className merger. shadcn convention — lets a
 * component accept `className` and override its internal classes
 * without duplication warnings.
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
