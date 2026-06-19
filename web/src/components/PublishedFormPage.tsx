import { useEffect, useState } from "react";
import { Button, Text } from "@fluentui/react-components";
import { useTranslation } from "react-i18next";
import {
  listOIDCProviders,
  loadCurrentUser,
  loadMetadata,
  loadPublishedForm,
  login,
  oidcStartURL,
  register,
  type AuthUser,
  type FormDefinition,
  type OIDCProvider,
  type TableMetadata
} from "../api";
import { useFormRunner } from "../hooks/useFormRunner";
import { AuthDialog } from "./AuthDialog";
import { FormRunner } from "./FormRunner";

type PublishedFormPageProps = {
  token: string;
};

export function PublishedFormPage({ token }: PublishedFormPageProps) {
  const { t } = useTranslation();
  const [authEmail, setAuthEmail] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(null);
  const [authReady, setAuthReady] = useState(false);
  const [authDialogOpen, setAuthDialogOpen] = useState(false);
  const [oidcProviders, setOIDCProviders] = useState<OIDCProvider[]>([]);
  const [form, setForm] = useState<FormDefinition | null>(null);
  const [tables, setTables] = useState<TableMetadata[]>([]);
  const [status, setStatus] = useState(t("status.loadingForm"));
  const formRunner = useFormRunner({
    databaseName: form?.database_name ?? "",
    script: form?.script ?? "",
    onStatus: setStatus,
    onRowCreated: (_table, row) => setStatus(t("status.publishedFormSubmitted", { id: row.record_id }))
  });

  useEffect(() => {
    let cancelled = false;
    void loadCurrentUser()
      .then((user) => {
        if (cancelled) {
          return;
        }
          setCurrentUser(user);
        if (!user) {
          setAuthDialogOpen(true);
          setStatus(t("status.loginToOpenForm"));
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setStatus(error instanceof Error ? error.message : t("status.currentUserLoadFailed"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setAuthReady(true);
        }
      });
    void listOIDCProviders()
      .then((providers) => {
        if (!cancelled) {
          setOIDCProviders(providers);
        }
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    if (!authReady || !currentUser) {
      return () => {
        cancelled = true;
      };
    }
    void loadPublishedForm(token)
      .then(async (loadedForm) => {
        const catalog = await loadMetadata();
        const database = catalog.databases.find((item) => item.name === loadedForm.database_name);
        if (!database) {
          throw new Error(t("form.databaseMetadataMissing", { database: loadedForm.database_name }));
        }
        return { loadedForm, tables: database?.tables ?? [] };
      })
      .then(({ loadedForm, tables: loadedTables }) => {
        if (cancelled) {
          return;
        }
        setForm(loadedForm);
        setTables(loadedTables);
        setStatus(t("status.openedForm", { name: loadedForm.name }));
      })
      .catch((error) => {
        if (!cancelled) {
          setForm(null);
          setTables([]);
          setStatus(error instanceof Error ? error.message : t("status.publishedFormLoadFailed"));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [authReady, currentUser?.id, token]);

  async function registerUser() {
    try {
      const user = await register(authEmail, authPassword);
      setCurrentUser(user);
      setAuthDialogOpen(false);
      setStatus(t("status.signedInAs", { email: user.email }));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : t("status.registrationFailed"));
    }
  }

  async function loginUser() {
    try {
      const user = await login(authEmail, authPassword);
      setCurrentUser(user);
      setAuthDialogOpen(false);
      setStatus(t("status.signedInAs", { email: user.email }));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : t("status.loginFailed"));
    }
  }

  function loginWithOIDC(providerName: string) {
    window.location.assign(oidcStartURL(providerName));
  }

  return (
    <div className="published-form-shell">
      <main className="published-form-main">
        <div className="section-header">
          <div>
            <Text weight="semibold">{form?.name ?? t("form.publishedForm")}</Text>
            <Text size={200}>{currentUser ? currentUser.email : t("status.loginRequired")}</Text>
          </div>
          {!currentUser && (
            <Button appearance="primary" onClick={() => setAuthDialogOpen(true)}>
              {t("common.login")}
            </Button>
          )}
        </div>
        {form ? (
          <FormRunner
            className="form-preview published-form-card"
            databaseName={form.database_name}
            renderedForm={formRunner.rendered}
            result={formRunner.result}
            tables={tables}
            values={formRunner.values}
            onAction={formRunner.execute}
            onSubmit={formRunner.submit}
            onValueChange={formRunner.updateValue}
          />
        ) : (
          <div className="empty-state">
            <Text>{status}</Text>
          </div>
        )}
      </main>
      <footer className="statusbar">{status}</footer>
      <AuthDialog
        email={authEmail}
        onEmailChange={setAuthEmail}
        onLogin={loginUser}
        onOIDCLogin={loginWithOIDC}
        onOpenChange={setAuthDialogOpen}
        onPasswordChange={setAuthPassword}
        onRegister={registerUser}
        open={authDialogOpen}
        password={authPassword}
        providers={oidcProviders}
      />
    </div>
  );
}
