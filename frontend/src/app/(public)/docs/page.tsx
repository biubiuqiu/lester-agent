import { getI18n } from "@/lib/i18n/server";
import type { Metadata } from "next";
import { DocsArticle } from "@/components/site/docs-article";
import { siteDocs } from "@/lib/site-docs";

export async function generateMetadata(): Promise<Metadata> { const { t } = await getI18n(); return { title: t("快速开始 · Lester Agent 帮助文档"), description: t("部署 Lester，配置模型，并完成你的第一项任务。") }; }

export default function Documentation() { return <DocsArticle doc={siteDocs[0]} />; }
