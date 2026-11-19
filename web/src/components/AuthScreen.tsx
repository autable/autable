import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { OIDCProvider } from "../api";
import { LogoMark } from "../shell/Logo";
import { LanguageButton } from "../shell/Sidebar";
import { Button, Callout, Field, Input, Tabs, TabsList, TabsTrigger, ThemeToggle } from "../ui";

type AuthMode = "login" | "register";

type AuthScreenProps = {
  onLogin: (email: string, password: string) => Promise<boolean>;
  onRegister: (email: string, password: string, displayName: string) => Promise<boolean>;
  onOIDCLogin: (providerName: string) => void;
  passwordEnabled: boolean;
  providers: OIDCProvider[];
};

/**
 * The signed-out entry: a single centered card, like Bindy's login page.
 * Password sign-in and registration share one form; configured OIDC
 * providers are offered below it. Nothing redirects on its own, so a broken
 * identity provider still leaves a page that explains itself.
 */
export function AuthScreen({ onLogin, onRegister, onOIDCLogin, passwordEnabled, providers }: AuthScreenProps) {
  const { t } = useTranslation();
  const [mode, setMode] = useState<AuthMode>("login");
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const hasOIDCProviders = providers.length > 0;
  const isRegister = mode === "register";
  const canSubmit = email.trim().length > 0 && password.length > 0 && (!isRegister || displayName.trim().length > 0);

  return (
    <div className="flex min-h-full flex-col overflow-y-auto">
      <div className="flex justify-end gap-0.5 p-3">
        <LanguageButton />
        <ThemeToggle />
      </div>
      <div className="flex flex-1 items-center justify-center px-5 pb-20">
        <form
          aria-label={t("auth.form")}
          className="w-full max-w-[22rem] rounded-container border border-border bg-elevated px-7 pb-7 pt-8"
          onSubmit={async (event) => {
            event.preventDefault();
            if (!passwordEnabled || !canSubmit || busy) {
              return;
            }
            setBusy(true);
            try {
              await (isRegister ? onRegister(email, password, displayName) : onLogin(email, password));
            } finally {
              setBusy(false);
            }
          }}
        >
          <LogoMark className="size-9" />
          <h1 className="mt-5 text-20 font-medium tracking-tight text-fg">
            {isRegister ? t("auth.registerTitleFull") : t("auth.signInTitle")}
          </h1>
          <p className="mt-1.5 text-13 leading-relaxed text-muted">
            {isRegister ? t("auth.registerDescription") : t("auth.signInDescription")}
          </p>
          {passwordEnabled && (
            <>
              <Tabs value={mode} onValueChange={(value) => setMode(value as AuthMode)} className="mt-6">
                <TabsList aria-label={t("auth.mode")} className="grid w-full grid-cols-2">
                  <TabsTrigger value="login">{t("common.login")}</TabsTrigger>
                  <TabsTrigger value="register">{t("common.register")}</TabsTrigger>
                </TabsList>
              </Tabs>
              <div className="mt-5 flex flex-col gap-4">
                <Field label={t("auth.email")}>
                  {(field) => (
                    <Input
                      {...field}
                      type="email"
                      autoComplete="email"
                      required
                      value={email}
                      onChange={(event) => setEmail(event.target.value)}
                    />
                  )}
                </Field>
                {isRegister && (
                  <Field label={t("auth.displayName")}>
                    {(field) => (
                      <Input
                        {...field}
                        type="text"
                        autoComplete="name"
                        required
                        value={displayName}
                        onChange={(event) => setDisplayName(event.target.value)}
                      />
                    )}
                  </Field>
                )}
                <Field label={t("auth.password")}>
                  {(field) => (
                    <Input
                      {...field}
                      type="password"
                      autoComplete={isRegister ? "new-password" : "current-password"}
                      required
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                    />
                  )}
                </Field>
              </div>
              <Button type="submit" variant="cta" size="lg" className="mt-6 w-full" disabled={!canSubmit} loading={busy}>
                {isRegister ? t("common.register") : t("common.login")}
              </Button>
            </>
          )}
          {passwordEnabled && hasOIDCProviders && (
            <div className="my-5 flex items-center gap-3 text-12 text-subtle" role="separator">
              <span className="h-px flex-1 bg-border" />
              {t("auth.or")}
              <span className="h-px flex-1 bg-border" />
            </div>
          )}
          {hasOIDCProviders && (
            <div className={passwordEnabled ? "flex flex-col gap-2" : "mt-7 flex flex-col gap-2"}>
              {providers.map((provider) => (
                <Button key={provider.name} type="button" size="lg" className="w-full" onClick={() => onOIDCLogin(provider.name)}>
                  {t("auth.continueWith", { provider: provider.name })}
                </Button>
              ))}
            </div>
          )}
          {!passwordEnabled && !hasOIDCProviders && (
            <Callout tone="warning" className="mt-6">
              {t("auth.noMethods")}
            </Callout>
          )}
        </form>
      </div>
    </div>
  );
}
