import { Suspense } from "react";
import { notFound } from "next/navigation";
import { StandaloneHTMLPreview } from "@/components/standalone-html-preview";

export default async function PreviewPage({ params }: { params: Promise<{ conversationId: string }> }) {
  const { conversationId } = await params;
  if (!/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i.test(conversationId)) notFound();
  return <Suspense fallback={<p role="status">正在加载预览…</p>}><StandaloneHTMLPreview conversationId={conversationId} /></Suspense>;
}
