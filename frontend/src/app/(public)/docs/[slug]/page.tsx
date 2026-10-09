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
  return { title: `${doc.navTitle} · Lester Agent 帮助文档`, description: doc.description };
}

export default async function DocumentationChapter({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const doc = getSiteDoc(slug);
  if (!doc) notFound();
  return <DocsArticle doc={doc} />;
}
