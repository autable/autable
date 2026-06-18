import {
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Divider,
  Field,
  Input
} from "@fluentui/react-components";
import { useTranslation } from "react-i18next";
import type { OIDCProvider } from "../api";

type AuthDialogProps = {
  email: string;
  onEmailChange: (value: string) => void;
  onLogin: () => Promise<void>;
  onOIDCLogin: (providerName: string) => void;
  onOpenChange: (open: boolean) => void;
  onPasswordChange: (value: string) => void;
  onRegister: () => Promise<void>;
  open: boolean;
  password: string;
  providers: OIDCProvider[];
};

export function AuthDialog({
  email,
  onEmailChange,
  onLogin,
  onOIDCLogin,
  onOpenChange,
  onPasswordChange,
  onRegister,
  open,
  password,
  providers
}: AuthDialogProps) {
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={(_, data) => onOpenChange(data.open)}>
      <DialogSurface>
        <form
          onSubmit={async (event) => {
            event.preventDefault();
            await onLogin();
            onOpenChange(false);
          }}
        >
          <DialogBody>
            <DialogTitle>{t("auth.loginTitle")}</DialogTitle>
            <DialogContent>
              <div className="auth-modal">
                <Field label={t("auth.email")}>
                  <Input
                    type="email"
                    autoComplete="email"
                    value={email}
                    onChange={(_, data) => onEmailChange(data.value)}
                  />
                </Field>
                <Field label={t("auth.password")}>
                  <Input
                    type="password"
                    autoComplete="current-password"
                    value={password}
                    onChange={(_, data) => onPasswordChange(data.value)}
                  />
                </Field>
                {providers.length > 0 && (
                  <>
                    <Divider>{t("auth.or")}</Divider>
                    <div className="oidc-actions">
                      {providers.map((provider) => (
                        <Button key={provider.name} onClick={() => onOIDCLogin(provider.name)}>
                          {t("auth.continueWith", { provider: provider.name })}
                        </Button>
                      ))}
                    </div>
                  </>
                )}
              </div>
            </DialogContent>
            <DialogActions>
              <Button
                type="button"
                onClick={async () => {
                  await onRegister();
                  onOpenChange(false);
                }}
              >
                {t("common.register")}
              </Button>
              <Button type="submit" appearance="primary">
                {t("common.login")}
              </Button>
            </DialogActions>
          </DialogBody>
        </form>
      </DialogSurface>
    </Dialog>
  );
}
