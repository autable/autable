import {
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Input,
  Label
} from "@fluentui/react-components";
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
  return (
    <Dialog open={open} onOpenChange={(_, data) => onOpenChange(data.open)}>
      <DialogSurface>
        <DialogBody>
          <DialogTitle>Login</DialogTitle>
          <DialogContent>
            <div className="auth-modal">
              <Label htmlFor="auth-email">Email</Label>
              <Input id="auth-email" type="email" value={email} onChange={(_, data) => onEmailChange(data.value)} />
              <Label htmlFor="auth-password">Password</Label>
              <Input
                id="auth-password"
                type="password"
                value={password}
                onChange={(_, data) => onPasswordChange(data.value)}
              />
              {providers.length > 0 && (
                <div className="oidc-actions">
                  {providers.map((provider) => (
                    <Button key={provider.name} onClick={() => onOIDCLogin(provider.name)}>
                      Continue with {provider.name}
                    </Button>
                  ))}
                </div>
              )}
            </div>
          </DialogContent>
          <DialogActions>
            <Button
              onClick={async () => {
                await onLogin();
                onOpenChange(false);
              }}
            >
              Login
            </Button>
            <Button
              appearance="primary"
              onClick={async () => {
                await onRegister();
                onOpenChange(false);
              }}
            >
              Register
            </Button>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}
