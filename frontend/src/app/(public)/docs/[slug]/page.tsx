import { getI18n } from "@/lib/i18n/server";
import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { DocsArticle } from "@/components/site/docs-article";
import { getSiteDoc, siteDocs } from "@/lib/site-docs";

export const dynamicParams = false;
export function generateStaticParams() { return siteDocs.filter((doc) => doc.slug).map(({ slug }) => ({ slug })); }
export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
  const { slug } = await params;
  const doc = getSiteDoc(slug);
  if (!doc) notFound();
  const { t } = await getI18n();
  return { title: t("{0} · Lester Agent 帮助文档", [t(doc.navTitle)]), description: t(doc.description) };
}

export default async function DocumentationChapter({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const doc = getSiteDoc(slug);
  if (!doc) notFound();
  return <DocsArticle doc={doc} />;
}
