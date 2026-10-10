"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { Globe } from "lucide-react";
import { loadMessages, localeCookie, localeNames, locales, supportedLocale, translator, type Locale, type Messages } from "@/lib/i18n";

type I18nValue = { locale: Locale; t: ReturnType<typeof translator>; changeLanguage: (locale: Locale) => Promise<void>; pending: boolean; error: boolean };
const I18nContext = createContext<I18nValue | null>(null);

export function I18nProvider({ locale: initialLocale, messages: initialMessages, children }: { locale: Locale; messages: Messages; children: ReactNode }) {
  const router = useRouter();
  const [language, setLanguage] = useState({ locale: initialLocale, messages: initialMessages });
  const [pending, setPending] = useState(false);
  const [error, setError] = useState(false);
  const changing = useRef(false);
  const t = useMemo(() => translator(language.messages), [language.messages]);
  useEffect(() => { document.documentElement.lang = language.locale; }, [language.locale]);
  const changeLanguage = useCallback(async (locale: Locale) => {
    if (changing.current || locale === language.locale) return;
    changing.current = true; setPending(true); setError(false);
    try {
      const messages = await loadMessages(locale);
      document.cookie = `${localeCookie}=${locale}; Path=/; Max-Age=31536000; SameSite=Lax${location.protocol === "https:" ? "; Secure" : ""}`;
      setLanguage({ locale, messages });
      router.refresh();
    } catch { setError(true); }
    finally { changing.current = false; setPending(false); }
  }, [language.locale, router]);
  const value = useMemo(() => ({ locale: language.locale, t, changeLanguage, pending, error }), [language.locale, t, changeLanguage, pending, error]);
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}
export function useI18n() {
  const value = useContext(I18nContext);
  if (!value) throw new Error("I18nProvider is required");
  return value;
}
export function useT() { return useI18n().t; }
/** Translate interface copy only. User messages, file bytes and editable values stay untouched. */
export function T({ children }: { children: string }) { const t = useT(); return t(children); }
export function LanguageSelect({ compact = false }: { compact?: boolean }) {
  const { locale, t, changeLanguage, pending, error } = useI18n();
  return <div className={`language-control${compact ? " compact" : ""}`}>
    <label><Globe size={16} aria-hidden="true" /><span className="visually-hidden">{t("界面语言")}</span><select aria-label={t("界面语言")} value={locale} disabled={pending} onChange={event => { const next = supportedLocale(event.target.value); if (next) void changeLanguage(next); }}>{locales.map(value => <option key={value} value={value} lang={value}>{localeNames[value]}</option>)}</select></label>
    {error ? <span role="alert">{t("语言加载失败，请重试")}</span> : null}
  </div>;
}
