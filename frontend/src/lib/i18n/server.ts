import "server-only";
import { cache } from "react";
import { cookies, headers } from "next/headers";
import { loadMessages, localeCookie, resolveLocale, translator } from "../i18n";

export const getI18n = cache(async () => {
  const [cookieJar, requestHeaders] = await Promise.all([cookies(), headers()]);
  const locale = resolveLocale(cookieJar.get(localeCookie)?.value, requestHeaders.get("accept-language") ?? "");
  const messages = await loadMessages(locale);
  return { locale, messages, t: translator(messages) };
});
