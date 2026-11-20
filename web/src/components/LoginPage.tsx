import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { loadAuthConfig, loadCurrentUser, login, oidcStartURL, register, type OIDCProvider } from "../api";
import { useNotifier } from "../notifications";
import { AuthScreen } from "./AuthScreen";

// LoginPage is a standalone login route for shared links (e.g. file URLs
// pasted into approvals): /login?redirect=/api/files/6 signs the user in and
// sends them back to the link they clicked.
export function LoginPage() {
  const { t } = useTranslation();
  const [passwordEnabled, setPasswordEnabled] = useState(true);
  const [providers, setProviders] = useState<OIDCProvider[]>([]);
  const { notify } = useNotifier();
  const redirect = safeRedirectTarget();

  useEffect(() => {
    let cancelled = false;
    void loadCurrentUser()
      .then((user) => {
        if (!cancelled && user) {
          window.location.assign(redirect);
        }
      })
      .catch(() => undefined);
    void loadAuthConfig()
      .then((authConfig) => {
        if (!cancelled) {
          setPasswordEnabled(authConfig.password_enabled);
          setProviders(authConfig.oidc_providers);
        }
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [redirect]);

  async function loginUser(email: string, password: string) {
    try {
      await login(email, password);
      window.location.assign(redirect);
      return true;
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.loginFailed"), "error");
      return false;
    }
  }

  async function registerUser(email: string, password: string, displayName: string) {
    try {
      await register(email, password, displayName);
      window.location.assign(redirect);
      return true;
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.registrationFailed"), "error");
      return false;
    }
  }

  return (
    <AuthScreen
      onLogin={loginUser}
      onRegister={registerUser}
      onOIDCLogin={(providerName) => window.location.assign(oidcStartURL(providerName, redirect))}
      passwordEnabled={passwordEnabled}
      providers={providers}
    />
  );
}

function safeRedirectTarget(): string {
  const target = new URLSearchParams(window.location.search).get("redirect") ?? "/";
  if (!target.startsWith("/") || target.startsWith("//") || target.includes("\\")) {
    return "/";
  }
  return target;
}
