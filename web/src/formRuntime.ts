export type FormElement =
  | {
      kind: "input";
      name: string;
      label: string;
      inputType: "text" | "email" | "search" | "tel" | "url" | "password";
      required: boolean;
    }
  | { kind: "select"; name: string; label: string; options: string[] }
  | { kind: "submit"; label: string };

export function previewFormElements(): FormElement[] {
  return [
    { kind: "input", name: "email", label: "Email", inputType: "email", required: true },
    { kind: "select", name: "status", label: "Status", options: ["Active", "Review", "Archived"] },
    { kind: "submit", label: "Create record" }
  ];
}
