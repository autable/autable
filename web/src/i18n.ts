import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import enUS from "./locales/en-US.json";
import zhCN from "./locales/zh-CN.json";

export const appLanguages = ["en-US", "zh-CN"] as const;
export type AppLanguage = (typeof appLanguages)[number];

const resources = {
  "en-US": { translation: enUS },
  "zh-CN": { translation: zhCN }
};

function initialLanguage(): AppLanguage {
  const savedLanguage = window.localStorage.getItem("autable.language");
  if (isAppLanguage(savedLanguage)) {
    return savedLanguage;
  }
  const browserLanguage = window.navigator.language;
  return browserLanguage.startsWith("zh") ? "zh-CN" : "en-US";
}

export function isAppLanguage(value: unknown): value is AppLanguage {
  return value === "en-US" || value === "zh-CN";
}

export function normalizeLanguage(value: string | undefined): AppLanguage {
  if (isAppLanguage(value)) {
    return value;
  }
  return value?.startsWith("zh") ? "zh-CN" : "en-US";
}

void i18n.use(initReactI18next).init({
  resources,
  lng: initialLanguage(),
  fallbackLng: "en-US",
  interpolation: {
    escapeValue: false
  },
  react: {
    useSuspense: false
  }
});

i18n.on("languageChanged", (language) => {
  window.localStorage.setItem("autable.language", normalizeLanguage(language));
});

export default i18n;
