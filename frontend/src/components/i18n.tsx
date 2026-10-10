"use client";

import { createContext, useCallback, useContext, useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { Check, ChevronDown, Globe } from "lucide-react";
import { loadMessages, localeCookie, localeNames, locales, translator, type Locale, type Messages } from "@/lib/i18n";
import { FloatingPopover } from "./floating-popover";
import { LanguageFlag } from "./language-flag";

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
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const options = useRef<Array<HTMLButtonElement | null>>([]);
  const id = useId();
  useEffect(() => {
    if (!open) return;
    const frame = requestAnimationFrame(() => options.current[locales.indexOf(locale)]?.focus());
    return () => cancelAnimationFrame(frame);
  }, [open, locale]);
  async function choose(next: Locale) {
    setOpen(false);
    await changeLanguage(next);
    trigger.current?.focus();
  }
  return <div className={`language-control${compact ? " compact" : ""}`}>
    <button ref={trigger} type="button" className="language-trigger" aria-label={[t("界面语言"), localeNames[locale]].join(" · ")} title={t("界面语言")} aria-haspopup="menu" aria-expanded={open} aria-controls={open ? id : undefined} disabled={pending} onClick={() => setOpen(value => !value)} onKeyDown={event => {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); setOpen(true); }
    }}>
      <LanguageFlag locale={locale} /><span className="language-trigger-name" lang={locale}>{localeNames[locale]}</span><span className="language-trigger-code" aria-hidden="true">{locale === "zh-CN" ? "ZH" : locale.toUpperCase()}</span><ChevronDown size={13} aria-hidden="true" />
    </button>
    {open && <FloatingPopover id={id} className="language-picker" role="menu" aria-label={t("界面语言")} anchor={trigger} side="below" align="end" width={208} onClose={reason => { setOpen(false); if (reason === "escape") trigger.current?.focus(); }} onKeyDown={event => {
      const current = options.current.findIndex(option => option === document.activeElement);
      let next: number | undefined;
      if (event.key === "ArrowDown") next = (current + 1) % locales.length;
      if (event.key === "ArrowUp") next = (current + locales.length - 1) % locales.length;
      if (event.key === "Home") next = 0;
      if (event.key === "End") next = locales.length - 1;
      if (next !== undefined) { event.preventDefault(); options.current[next]?.focus(); }
      if (event.key === "Tab") { setOpen(false); trigger.current?.focus(); }
    }}>
      <div className="language-picker-heading"><Globe size={14} aria-hidden="true" />{t("界面语言")}</div>
      <div className="language-picker-options">{locales.map((value, index) => <button ref={element => { options.current[index] = element; }} key={value} type="button" role="menuitemradio" tabIndex={-1} aria-checked={value === locale} disabled={pending} className={value === locale ? "selected" : undefined} onClick={() => void choose(value)}>
        <LanguageFlag locale={value} /><span lang={value}>{localeNames[value]}</span>{value === locale && <Check size={15} aria-hidden="true" />}
      </button>)}</div>
    </FloatingPopover>}
    {error ? <span role="alert">{t("语言加载失败，请重试")}</span> : null}
  </div>;
}
