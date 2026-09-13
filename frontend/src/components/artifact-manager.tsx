"use client";

import { ArtifactIcon } from "./workspace-icons";
import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  Check,
  Copy,
  ExternalLink,
  LoaderCircle,
  MoreHorizontal,
  Eye,
  Folder,
  Plus,
  RefreshCw,
  X,
} from "lucide-react";
import { api, type Artifact, type Conversation, type Project } from "@/lib/api";

type PublishChoice = {
  conversationId: string;
  sourcePath: string;
  name?: string;
  artifact?: Artifact;
};
export function DeployDialog({
  choice,
  conversations,
  onClose,
  onPublished,
}: {
  choice: PublishChoice;
  conversations?: Conversation[];
  onClose: () => void;
  onPublished?: (item: Artifact) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [conversation, setConversation] = useState(choice.conversationId);
  const [source, setSource] = useState(choice.sourcePath);
  const [name, setName] = useState(
    choice.name || choice.sourcePath.split("/").pop() || "我的站点",
  );
  const [entry, setEntry] = useState(
    choice.artifact && !/\.html?$/i.test(choice.sourcePath)
      ? choice.artifact.entry_path.replace(
          new RegExp(
            `^${choice.sourcePath.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}/`,
          ),
          "",
        )
      : "index.html",
  );
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<Artifact | null>(null);
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  async function deploy() {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError("");
    try {
      const a = await api<Artifact>(
        `/api/v1/conversations/${conversation}/artifacts`,
        {
          method: "POST",
          body: JSON.stringify({
            name,
            source_path: source,
            entry,
            artifact_id: choice.artifact?.id,
          }),
        },
      );
      setResult(a);
      onPublished?.(a);
    } catch (e) {
      setError(e instanceof Error ? e.message : "部署失败");
    } finally {
      setBusy(false);
      pending.current = false;
    }
  }
  return (
    <dialog
      className="deploy-dialog"
      ref={dialog}
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) onClose();
      }}
    >
      <header>
        <div>
          <p className="eyebrow">PUBLISH</p>
          <h2>
            {result
              ? "站点已部署"
              : choice.artifact
                ? "更新部署"
                : "部署 HTML 站点"}
          </h2>
        </div>
        <button
          type="button"
          className="icon-button"
          disabled={busy}
          aria-label="关闭部署窗口"
          onClick={onClose}
        >
          <X />
        </button>
      </header>
      {result ? (
        <div className="deploy-result">
          <ArtifactIcon />
          <p>页面和本地资源已保存。拥有链接的人可以直接访问。</p>
          <a href={result.url} target="_blank" rel="noopener noreferrer">
            打开站点 <ExternalLink size={14} />
          </a>
          <div className="site-url">
            <code>{result.url}</code>
            <button
              className="icon-button"
              aria-label="复制站点链接"
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(result.url);
                  setCopied(true);
                } catch {
                  setError("复制失败，请手动复制链接");
                }
              }}
            >
              {copied ? <Check /> : <Copy />}
            </button>
          </div>
          {result.warnings?.map((w) => (
            <p className="deployment-warning" key={w}>
              {w}
            </p>
          ))}
          <p>
            {result.file_count} 个文件 · {formatSize(result.size_bytes)}
          </p>
          <Link href={`/app/artifacts?returnTo=${encodeURIComponent(`/app/c/${conversation}`)}`}>查看所有产物</Link>
        </div>
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void deploy();
          }}
        >
          <p className="deployment-note">
            部署后生成公开链接，可在产物管理中下线。只选择你希望分享的页面和资源。
          </p>
          {conversations ? (
            <label className="field">
              来源会话
              <select
                aria-label="来源会话"
                value={conversation}
                disabled={busy || !!choice.artifact}
                onChange={(e) => setConversation(e.target.value)}
              >
                <option value="">选择会话</option>
                {conversations.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.title}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <label className="field">
            站点名称
            <input
              aria-label="站点名称"
              required
              maxLength={120}
              value={name}
              disabled={busy}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <label className="field">
            HTML 文件或站点目录
            <input
              aria-label="部署源路径"
              required
              value={source}
              disabled={busy}
              placeholder="index.html 或 website"
              onChange={(e) => setSource(e.target.value)}
            />
          </label>
          {!/\.html?$/i.test(source) ? (
            <label className="field">
              目录入口
              <input
                aria-label="站点入口"
                required
                value={entry}
                disabled={busy}
                onChange={(e) => setEntry(e.target.value)}
              />
            </label>
          ) : null}
          <p className="deployment-note">
            单文件会收集其本地引用；目录会包含静态资源。支持图片、视频、CSS 和
            JavaScript。单文件上限 25 MiB，站点上限 100 MiB / 256 个文件。
          </p>
          <button
            className="primary-button"
            disabled={busy || !conversation || !source.trim() || !name.trim()}
          >
            {busy ? (
              <LoaderCircle className="spin" size={16} />
            ) : (
              <ArtifactIcon size={16} />
            )}{" "}
            {busy
              ? "正在收集资源并部署…"
              : choice.artifact
                ? "更新并发布"
                : "部署并生成链接"}
          </button>
        </form>
      )}
      {error ? (
        <p role="alert" className="form-error">
          {error}
        </p>
      ) : null}
    </dialog>
  );
}
export function PublishFileButton({
  conversationId,
  path,
}: {
  conversationId: string;
  path: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        className="file-ask-agent"
        aria-label="部署 HTML 站点"
        title="部署并生成公开链接"
        onClick={() => setOpen(true)}
      >
        <ArtifactIcon size={15} />
        <span>部署</span>
      </button>
      {open ? (
        <DeployDialog
          choice={{ conversationId, sourcePath: path }}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </>
  );
}
export function ArtifactManager({
  projects,
  conversations,
}: {
  projects: Project[];
  conversations: Conversation[];
}) {
  const [items, setItems] = useState<Artifact[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [search, setSearch] = useState("");
  const [project, setProject] = useState("");
  const [choice, setChoice] = useState<PublishChoice | null>(null);
  const [copied, setCopied] = useState("");
  const [preview, setPreview] = useState<Artifact | null>(null);
  const refresh = useCallback(async () => {
    try {
      const data = await api<{ artifacts: Artifact[] }>("/api/v1/artifacts");
      setItems(data.artifacts);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "产物加载失败");
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => {
    let active = true;
    api<{ artifacts: Artifact[] }>("/api/v1/artifacts")
      .then((data) => { if (active) { setItems(data.artifacts); setError(""); } })
      .catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : "产物加载失败"); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, []);
  async function unpublish(a: Artifact) {
    if (busy) return;
    setBusy(a.id);
    setError("");
    try {
      await api(`/api/v1/artifacts/${a.id}/unpublish`, { method: "POST" });
      setItems((prev) =>
        prev.map((i) => (i.id === a.id ? { ...i, status: "unpublished" } : i)),
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "下线失败");
    } finally {
      setBusy("");
    }
  }
  const sourceConversations = conversations.filter(c => !project || c.project_id === project);
  const filtered = items.filter(
    (a) =>
      (!project || a.project_id === project) &&
      a.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
  );
  return (
    <div className="artifact-manager">
      <header className="artifact-heading">
        <div>
          <p className="eyebrow">YOUR PUBLISHED WORK</p>
          <h1>产物管理</h1>
          <p>把对话中的成果变成可分享的页面。</p>
        </div>
        <button
          className="primary-button"
          disabled={!sourceConversations.length}
          onClick={() =>
            setChoice({
              conversationId: "",
              sourcePath: "index.html",
            })
          }
        >
          <Plus size={16} />
          部署站点
        </button>
      </header>
      <div className="artifact-summary" aria-label="部署概况">
        <span><strong>{items.length}</strong> 全部产物</span>
        <span><i /> <strong>{items.filter(a => a.status === "published").length}</strong> 已发布</span>
        <span><strong>{items.filter(a => a.status === "unpublished").length}</strong> 已下线</span>
      </div>
      <div className="artifact-filters">
        <input
          aria-label="搜索产物"
          placeholder="搜索站点名称"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select
          aria-label="按项目筛选产物"
          value={project}
          onChange={(e) => setProject(e.target.value)}
        >
          <option value="">所有项目</option>
          {projects.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
        <button
          className="icon-button"
          aria-label="刷新产物"
          onClick={() => void refresh()}
        >
          <RefreshCw />
        </button>
      </div>
      {error ? (
        <p role="alert" className="workspace-error">
          {error}
          <button onClick={() => void refresh()}>重试</button>
        </p>
      ) : null}
      {loading ? (
        <div className="artifact-empty">
          <LoaderCircle />
          正在载入产物…
        </div>
      ) : !filtered.length ? (
        <div className="artifact-empty">
          <ArtifactIcon size={38} />
          <h2>{items.length ? "没有匹配的产物" : "让成果拥有自己的链接"}</h2>
          <p>
            在对话中生成 HTML 后，点击文件工具栏的“部署”，或让 Lester 帮你发布。
          </p>
        </div>
      ) : (
        <div className="artifact-grid">
          {filtered.map((a) => (
            <article className="published-site-card" key={a.id}>
              <div className={`artifact-cover ${a.status}`}>
                <span className="artifact-cover-icon"><ArtifactIcon size={30} /></span>
                <span className={`site-status ${a.status}`}>{a.status === "published" ? "已发布" : "已下线"}</span>
                {a.status === "published" ? <button className="artifact-preview-button" onClick={() => setPreview(a)} aria-label={`预览 ${a.name}`}><Eye size={15} />预览页面</button> : <span className="artifact-offline-note">站点已下线</span>}
              </div>
              <div className="artifact-card-body">
              <p className="artifact-project"><Folder size={13} />{projects.find(p => p.id === a.project_id)?.name || "项目"}</p>
              <h2>{a.name}</h2>
              <p className="artifact-date">更新于 {new Date(a.updated_at).toLocaleDateString()}</p>
              <div className="artifact-actions">
                {a.status === "published" ? (
                  <>
                    <a href={a.url} target="_blank" rel="noopener noreferrer">
                      打开页面 <ExternalLink size={13} />
                    </a>
                    <button
                      onClick={async () => {
                        try {
                          await navigator.clipboard.writeText(a.url);
                          setCopied(a.id);
                        } catch {
                          setError("复制失败，请打开站点后复制地址");
                        }
                      }}
                    >
                      {copied === a.id ? "已复制" : "复制链接"}
                    </button>
                  </>
                ) : null}
                <details className="artifact-more" name="artifact-actions" onClick={e => { if ((e.target as Element).closest("button, a")) e.currentTarget.open = false; }}>
                  <summary aria-label={`${a.name} 的更多操作`}><MoreHorizontal size={18} /></summary>
                  <div>
                <Link href={`/app/c/${a.conversation_id}`}>来源会话</Link>
                <button
                  disabled={!!busy}
                  onClick={() =>
                    setChoice({
                      conversationId: a.conversation_id,
                      sourcePath: a.source_path,
                      name: a.name,
                      artifact: a,
                    })
                  }
                >
                  {a.status === "published" ? "更新部署" : "重新部署"}
                </button>
                {a.status === "published" ? (
                  <button
                    className="unpublish-button"
                    disabled={!!busy}
                    onClick={() => void unpublish(a)}
                  >
                    {busy === a.id ? "正在下线…" : "下线"}
                  </button>
                ) : null}
                  <p className="artifact-file-detail" title={a.source_path}>{a.source_path}<br />{a.file_count} 个文件 · {formatSize(a.size_bytes)}</p>
                  </div>
                </details>
              </div>
              </div>
            </article>
          ))}
        </div>
      )}
      {preview ? <ArtifactPreview artifact={preview} onClose={() => setPreview(null)} /> : null}
      {choice ? (
        <DeployDialog
          choice={choice}
          conversations={choice.artifact ? conversations : sourceConversations}
          onClose={() => setChoice(null)}
          onPublished={(a) =>
            setItems((prev) => [a, ...prev.filter((i) => i.id !== a.id)])
          }
        />
      ) : null}
    </div>
  );
}
function formatSize(n: number) {
  return n >= 1024 * 1024
    ? `${(n / 1024 / 1024).toFixed(1)} MB`
    : `${Math.max(1, Math.round(n / 1024))} KB`;
}

function ArtifactPreview({ artifact, onClose }: { artifact: Artifact; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => { dialog.current?.showModal(); }, []);
  return <dialog className="artifact-preview-dialog" ref={dialog} onCancel={e => { e.preventDefault(); onClose(); }}>
    <header><strong>{artifact.name}</strong><a href={artifact.url} target="_blank" rel="noopener noreferrer">打开页面 <ExternalLink size={14} /></a><button className="icon-button" aria-label="关闭预览" onClick={onClose}><X /></button></header>
    <iframe src={artifact.url} title={`${artifact.name} 的预览`} sandbox="allow-scripts" referrerPolicy="no-referrer" />
  </dialog>;
}
