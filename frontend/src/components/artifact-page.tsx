"use client";

import { ArrowLeft, LoaderCircle } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import { api, type Conversation, type Project } from "@/lib/api";
import { ArtifactManager } from "./artifact-manager";
import { Brand } from "./brand";

export function ArtifactPage({ returnTo = "/app" }: { returnTo?: string }) {
  const [attempt, setAttempt] = useState(0);
  const [projects, setProjects] = useState<Project[]>([]);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    Promise.all([
      api<{ projects: Project[] }>("/api/v1/projects"),
      api<{ conversations: Conversation[] }>("/api/v1/conversations"),
    ])
      .then(([projectResult, conversationResult]) => {
        if (!active) return;
        setProjects(projectResult.projects);
        setConversations(conversationResult.conversations);
      })
      .catch((reason: unknown) => {
        if (active) {
          setError(reason instanceof Error ? reason.message : "产物页面加载失败");
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [attempt]);

  return (
    <main className="artifacts-page-shell">
      <header className="artifacts-page-topbar">
        <Link className="artifacts-page-brand" href={returnTo} aria-label="返回 Lester 工作区">
          <Brand />
        </Link>
        <Link className="artifacts-page-back" href={returnTo}>
          <ArrowLeft size={16} />
          返回工作区
        </Link>
      </header>
      {loading ? (
        <div className="artifact-page-loading">
          <LoaderCircle className="spin" size={20} />
          正在载入产物管理…
        </div>
      ) : error ? (
        <div className="artifact-page-error" role="alert">
          {error}
          <button type="button" onClick={() => { setError(""); setLoading(true); setAttempt(value => value + 1); }}>重新加载</button>
        </div>
      ) : (
        <ArtifactManager projects={projects} conversations={conversations} />
      )}
    </main>
  );
}
