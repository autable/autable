import { type FormEvent, useEffect, useMemo, useState } from "react";
import { Button, Input, Select, Text } from "@fluentui/react-components";
import {
  listOIDCProviders,
  loadCurrentUser,
  loadPublishedForm,
  login,
  oidcStartURL,
  register,
  submitPublishedForm,
  type AuthUser,
  type FormDefinition,
  type OIDCProvider
} from "../api";
import { renderFormScript, type FormElement } from "../formRuntime";
import { AuthDialog } from "./AuthDialog";

type PublishedFormPageProps = {
  token: string;
};

export function PublishedFormPage({ token }: PublishedFormPageProps) {
  const [authEmail, setAuthEmail] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(null);
  const [authReady, setAuthReady] = useState(false);
  const [authDialogOpen, setAuthDialogOpen] = useState(false);
  const [oidcProviders, setOIDCProviders] = useState<OIDCProvider[]>([]);
  const [form, setForm] = useState<FormDefinition | null>(null);
  const [formValues, setFormValues] = useState<Record<string, string>>({});
  const [status, setStatus] = useState("Loading form");
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
          setStatus("Login to open this form");
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setStatus(error instanceof Error ? error.message : "Current user load failed");
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
      .then((loadedForm) => {
        if (cancelled) {
          return;
        }
        setForm(loadedForm);
        setFormValues({});
        setStatus(`Opened ${loadedForm.name}`);
      })
      .catch((error) => {
        if (!cancelled) {
          setForm(null);
          setStatus(error instanceof Error ? error.message : "Published form load failed");
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
      setStatus(`Signed in as ${user.email}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Registration failed");
    }
  }

  async function loginUser() {
    try {
      const user = await login(authEmail, authPassword);
      setCurrentUser(user);
      setAuthDialogOpen(false);
      setStatus(`Signed in as ${user.email}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Login failed");
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
    if (!renderedForm.table || !renderedForm.fields || Object.keys(renderedForm.fields).length === 0) {
      setStatus("Published form definition is required");
      return;
    }
    const values = Object.fromEntries(
      renderedForm.elements.flatMap((element) => {
        if (element.kind === "input") {
          return [[element.name, formValues[element.name] ?? ""]];
        }
        if (element.kind === "select") {
          return [[element.name, formValues[element.name] ?? element.options[0] ?? ""]];
        }
        return [];
      })
    );
    try {
      const saved = await submitPublishedForm(token, values);
      setFormValues({});
      setStatus(`Form submitted as record ${saved.record_id}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Published form submit failed");
    }
  }

  return (
    <div className="published-form-shell">
      <main className="published-form-main">
        <div className="section-header">
          <div>
            <Text weight="semibold">{form?.name ?? "Published form"}</Text>
            <Text size={200}>{currentUser ? currentUser.email : "Login required"}</Text>
          </div>
          {!currentUser && (
            <Button appearance="primary" onClick={() => setAuthDialogOpen(true)}>
              Login
            </Button>
          )}
        </div>
        {form ? (
          <form className="form-preview published-form-card" onSubmit={(event) => void submitForm(undefined, event)}>
            {renderedForm.error && <Text className="form-error">{renderedForm.error}</Text>}
            {renderedForm.elements.map((element) => {
              if (element.kind === "input") {
                return (
                  <label key={element.name} className="field-stack">
                    <span>{element.label}</span>
                    <Input
                      type={element.inputType}
                      value={formValues[element.name] ?? ""}
                      onChange={(_, data) => setFormValues((current) => ({ ...current, [element.name]: data.value }))}
                    />
                  </label>
                );
              }
              if (element.kind === "select") {
                return (
                  <label key={element.name} className="field-stack">
                    <span>{element.label}</span>
                    <Select
                      value={formValues[element.name] ?? element.options[0] ?? ""}
                      onChange={(_, data) => setFormValues((current) => ({ ...current, [element.name]: data.value }))}
                    >
                      {element.options.map((option) => (
                        <option key={option}>{option}</option>
                      ))}
                    </Select>
                  </label>
                );
              }
              if (element.kind === "html") {
                return <div key={element.html} className="form-html" dangerouslySetInnerHTML={{ __html: element.html }} />;
              }
              return (
                <Button key={element.label} type="button" appearance="primary" onClick={() => void submitForm(element)}>
                  {element.label}
                </Button>
              );
            })}
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
