export const locales = ["zh-CN", "en", "ja", "ko", "fr", "es"] as const;
export type Locale = typeof locales[number];
export type Messages = Record<string, string>;
export const localeNames: Record<Locale, string> = { "zh-CN": "中文", en: "English", ja: "日本語", ko: "한국어", fr: "Français", es: "Español" };
export const localeCookie = "lester_locale";

export function supportedLocale(value: string | null | undefined): Locale | undefined {
  const language = value?.trim().replaceAll("_", "-").toLowerCase();
  if (language === "zh" || language?.startsWith("zh-")) return "zh-CN";
  return locales.find(locale => locale !== "zh-CN" && (language === locale || language?.startsWith(`${locale}-`)));
}
export function resolveLocale(cookie: string | undefined, acceptLanguage = ""): Locale {
  const explicit = supportedLocale(cookie);
  if (explicit) return explicit;
  const preferences = acceptLanguage.split(",").map((part, index) => {
    const [language, weight] = part.trim().split(";");
    const quality = weight ? Number(weight.trim().replace(/^q=/, "")) : 1;
    return { locale: supportedLocale(language), quality: Number.isFinite(quality) ? quality : 0, index };
  }).filter(item => item.locale && item.quality > 0).sort((a, b) => b.quality - a.quality || a.index - b.index);
  return preferences[0]?.locale ?? "en";
}

export function formatMessage(template: string, values: readonly unknown[] = []): string {
  return template.replace(/\{(\d+)\}/g, (match, index: string) => Number(index) < values.length ? String(values[Number(index)] ?? "") : match);
}
export function translator(messages: Messages) {
  // Legacy UI notices are stored in drafts as source strings. Match only known UI templates.
  const patterns = Object.keys(messages).filter(key => /\{\d+\}/.test(key) && /[\u3400-\u9fff]/.test(key)).map(key => {
    const indices: number[] = [];
    const pattern = key.split(/(\{\d+\})/).map(part => {
      const parameter = /^\{(\d+)\}$/.exec(part);
      if (parameter) { indices.push(Number(parameter[1])); return "([\\s\\S]*?)"; }
      return part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    }).join("");
    const marker = key.split(/\{\d+\}/).sort((a, b) => b.length - a.length)[0];
    return { key, indices, marker, pattern: new RegExp(`^${pattern}$`) };
  });
  return (source: string, values: readonly unknown[] = []): string => {
    if (Object.hasOwn(messages, source)) return formatMessage(messages[source], values);
    if (!values.length) for (const item of patterns) {
      if (!source.includes(item.marker)) continue;
      const match = item.pattern.exec(source);
      if (match) { const captured: string[] = []; item.indices.forEach((index, position) => { captured[index] = match[position + 1]; }); return formatMessage(messages[item.key], captured); }
    }
    return formatMessage(source, values);
  };
}
export async function loadMessages(locale: Locale): Promise<Messages> {
  switch (locale) {
    case "zh-CN": return {};
    case "en": return (await import("./i18n/messages/en.json")).default;
    case "ja": return (await import("./i18n/messages/ja.json")).default;
    case "ko": return (await import("./i18n/messages/ko.json")).default;
    case "fr": return (await import("./i18n/messages/fr.json")).default;
    case "es": return (await import("./i18n/messages/es.json")).default;
  }
}
