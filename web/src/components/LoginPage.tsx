import { useEffect, useState } from "react";
import { Text } from "@fluentui/react-components";
import { useTranslation } from "react-i18next";
import {
  loadAuthConfig,
  loadCurrentUser,
  login,
  oidcStartURL,
  register,
  type OIDCProvider
} from "../api";
import { useNotifier } from "../notifications";
import { AuthDialog } from "./AuthDialog";

// LoginPage is a standalone login route for shared links (e.g. file URLs
// pasted into approvals): /login?redirect=/api/files/6 signs the user in and
// sends them back to the link they clicked.
export function LoginPage() {
  const { t } = useTranslation();
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [passwordEnabled, setPasswordEnabled] = useState(true);
  const [providers, setProviders] = useState<OIDCProvider[]>([]);
  const { Toaster, notify } = useNotifier();
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

  async function loginUser() {
    try {
      await login(email, password);
      window.location.assign(redirect);
      return true;
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.loginFailed"), "error");
      return false;
    }
  }

  async function registerUser() {
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
    <div className="published-form-shell">
      <main className="published-form-main">
        <div className="section-header">
          <Text weight="semibold">{t("status.loginRequired")}</Text>
        </div>
        <Toaster />
        <AuthDialog
          displayName={displayName}
          email={email}
          onDisplayNameChange={setDisplayName}
          onEmailChange={setEmail}
          onLogin={loginUser}
          onOIDCLogin={(providerName) => window.location.assign(oidcStartURL(providerName, redirect))}
          onOpenChange={() => undefined}
          onPasswordChange={setPassword}
          onRegister={registerUser}
          open
          password={password}
          passwordEnabled={passwordEnabled}
          providers={providers}
        />
      </main>
    </div>
  );
}

function safeRedirectTarget(): string {
  const target = new URLSearchParams(window.location.search).get("redirect") ?? "/";
  if (!target.startsWith("/") || target.startsWith("//") || target.includes("\\")) {
    return "/";
  }
  return target;
}
