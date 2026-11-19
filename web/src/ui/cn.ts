import { clsx, type ClassValue } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

// tailwind-merge only knows Tailwind's default scales. Without these, the
// custom font sizes (`text-14`) read as text colors and conflict with
// `text-muted`, so one of the two is silently dropped; the custom radii and
// shadow would not merge with their built-in counterparts either. Keep these
// lists in sync with the @theme block in styles.css.
export const fontSizes = ["11", "12", "13", "14", "15", "18", "20", "24"];

const twMerge = extendTailwindMerge({
  extend: {
    theme: {
      text: fontSizes,
      radius: ["inner", "container"],
      shadow: ["float"]
    }
  }
});

/** Merge class names; later Tailwind classes override earlier ones for the same property. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
