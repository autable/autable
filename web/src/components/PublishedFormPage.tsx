import { type FormEvent, useEffect, useMemo, useState } from "react";
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
  createRow,
  type AuthUser,
  type FormDefinition,
  type OIDCProvider,
  type TableMetadata
} from "../api";
import { renderFormScript, type FormElement } from "../formRuntime";
import { AuthDialog } from "./AuthDialog";
import { FormPreviewFields } from "./FormPreviewFields";

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
  const [formValues, setFormValues] = useState<Record<string, string>>({});
  const [tables, setTables] = useState<TableMetadata[]>([]);
  const [status, setStatus] = useState(t("status.loadingForm"));
  const renderedForm = useMemo(() => renderFormScript(form?.script ?? ""), [form?.script]);

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
        setFormValues({});
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

  async function submitForm(submitElement?: Extract<FormElement, { kind: "submit" }>, event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    if (!form) {
      return;
    }
    if (!submitElement && !renderedForm.elements.some((element) => element.kind === "submit")) {
      return;
    }
    if (!renderedForm.table) {
      setStatus(t("status.publishedFormDefinitionRequired"));
      return;
    }
    const values = Object.fromEntries(
      renderedForm.elements.flatMap((element) => {
        if (element.kind === "input" || element.kind === "relation") {
          return [[element.field, formValues[element.field] ?? ""]];
        }
        if (element.kind === "select") {
          return [[element.field, formValues[element.field] ?? element.options[0] ?? ""]];
        }
        return [];
      })
    );
    try {
      const saved = await createRow(form.database_name, renderedForm.table, values);
      setFormValues({});
      setStatus(t("status.publishedFormSubmitted", { id: saved.record_id }));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : t("status.publishedFormSubmitFailed"));
    }
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
          <form className="form-preview published-form-card" onSubmit={(event) => void submitForm(undefined, event)}>
            {renderedForm.error && <Text className="form-error">{renderedForm.error}</Text>}
            <FormPreviewFields
              databaseName={form.database_name}
              elements={renderedForm.elements}
              formValues={formValues}
              onFormValueChange={(name, value) => setFormValues((current) => ({ ...current, [name]: value }))}
              onSubmit={submitForm}
              tables={tables}
            />
          </form>
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
