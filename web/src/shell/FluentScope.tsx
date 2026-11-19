import { FluentProvider, webDarkTheme, webLightTheme, type Theme } from "@fluentui/react-components";
import type { ReactNode } from "react";
import { useTheme } from "../ui";

// Screens not yet moved to the design system (workflow editor, permission
// panel, runners, Excel import dialog) still use Fluent UI. They render inside
// this scope only, with Fluent's light or dark theme following the app theme
// and the app's font, so they sit in the new shell without a second look.
const fontFamilyBase = 'var(--font-sans)';
const lightTheme: Theme = { ...webLightTheme, fontFamilyBase };
const darkTheme: Theme = { ...webDarkTheme, fontFamilyBase };

export function FluentScope({ children, className }: { children: ReactNode; className?: string }) {
  const { resolved } = useTheme();
  return (
    <FluentProvider theme={resolved === "dark" ? darkTheme : lightTheme} className={className} style={{ background: "transparent" }}>
      {children}
    </FluentProvider>
  );
}
