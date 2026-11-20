import { createContext, forwardRef, useContext, type AnchorHTMLAttributes, type ReactNode } from "react";
import { buildWorkspacePath, type WorkspaceRoute } from "../appState";

type Navigate = (route: WorkspaceRoute | null) => void;

const NavigationContext = createContext<Navigate | null>(null);

// The app keeps its own history-based routing (appState.ts); pages and the
// sidebar reach it through this context instead of threading callbacks.
export function NavigationProvider({ navigate, children }: { navigate: Navigate; children: ReactNode }) {
  return <NavigationContext.Provider value={navigate}>{children}</NavigationContext.Provider>;
}

export function useNavigate(): Navigate {
  const navigate = useContext(NavigationContext);
  if (!navigate) throw new Error("useNavigate must be used inside NavigationProvider");
  return navigate;
}

type RouteLinkProps = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href"> & {
  route: WorkspaceRoute | null;
};

/**
 * A real link to a workspace route: plain clicks navigate in place, while
 * modified clicks (new tab, new window) keep the browser's default.
 */
export const RouteLink = forwardRef<HTMLAnchorElement, RouteLinkProps>(function RouteLink({ route, onClick, ...props }, ref) {
  const navigate = useNavigate();
  return (
    <a
      ref={ref}
      href={buildWorkspacePath(route)}
      onClick={(event) => {
        onClick?.(event);
        if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
          return;
        }
        event.preventDefault();
        navigate(route);
      }}
      {...props}
    />
  );
});
