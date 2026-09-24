"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, type Conversation, type Deployment, type Project } from "@/lib/api";

export default function NewAgentPage() {
  const router = useRouter();
  const started = useRef(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    void (async () => {
      try {
        const [projects, deployments] = await Promise.all([
          api<{ projects: Project[] }>("/api/v1/projects"),
          api<{ deployments: Deployment[] }>("/api/v1/model-deployments"),
        ]);
        const project = projects.projects.find(item => item.is_default) ?? projects.projects[0];
        const model = deployments.deployments.find(item => item.is_default) ?? deployments.deployments[0];
        if (!project || !model) throw new Error("请先创建项目并配置可用模型。");
        const conversation = await api<Conversation>("/api/v1/conversations", {
          method: "POST",
          body: JSON.stringify({ project_id: project.id, agent_slug: "agent-designer", title: "设计新智能体", model_deployment_id: model.id }),
        });
        router.replace(`/app/c/${conversation.id}`);
      } catch (reason) {
        setError(reason instanceof Error ? reason.message : "无法开始智能体设计会话");
      }
    })();
  }, [router]);
  return <main className="agent-start-page"><div><h1>正在邀请智能体设计师…</h1><p>{error || "会话将保存在默认项目中。先聊聊你的想法，再一起完成配置。"}</p>{error ? <Link href="/app/agents">返回 Agent 管理</Link> : null}</div></main>;
}
