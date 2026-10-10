"use client";

import { useEffect, useRef, useState } from "react";
import type { Terminal as XTerm } from "@xterm/xterm";
import { API } from "@/lib/api";
import { ensureSession, redirectToLogin, SessionExpired } from "@/lib/auth-client";

type Connection = "connecting" | "connected" | "disconnected" | "error";
const labels: Record<Connection, string> = { connecting: "连接中…", connected: "已连接", disconnected: "已断开", error: "连接失败" };
const keys = [{ label: "Tab", title: "Tab 补全", data: "\t" }, { label: "↑", title: "上一条历史命令", data: "\x1b[A" }, { label: "↓", title: "下一条历史命令", data: "\x1b[B" }, { label: "Esc", title: "发送 Escape", data: "\x1b" }, { label: "Ctrl+C", title: "中断当前命令", data: "\x03" }, { label: "Ctrl+D", title: "发送 EOF / 退出 Shell", data: "\x04" }];

export function ComputerTerminal({ conversationId }: { conversationId: string }) {
  const [connection, setConnection] = useState<Connection>("connecting");
  const [notice, setNotice] = useState("");
  const [selected, setSelected] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const mount = useRef<HTMLDivElement>(null);
  const terminal = useRef<XTerm | null>(null);
  const help = useRef<HTMLButtonElement>(null);

  async function copySelection(instance = terminal.current) {
    const text = instance?.getSelection();
    if (!text) return;
    try { await navigator.clipboard.writeText(text); setNotice("已复制选中内容"); }
    catch { setNotice("复制失败，请使用浏览器的复制菜单。"); }
  }

  useEffect(() => {
    let active = true;
    let socket: WebSocket | undefined;
    let instance: XTerm | undefined;
    let resize: ResizeObserver | undefined;
    let resizeFrame = 0;
    let timeout = 0;
    const disposables: { dispose: () => void }[] = [];
    const originalFocus = document.activeElement;
    const cleanup = () => {
      clearTimeout(timeout); cancelAnimationFrame(resizeFrame); resize?.disconnect();
      disposables.splice(0).forEach(item => item.dispose()); socket?.close(); instance?.dispose();
      if (terminal.current === instance) terminal.current = null;
      instance = undefined;
    };
    void (async () => {
      const [{ Terminal }, { FitAddon }] = await Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit"), ensureSession(true)]);
      if (!active || !mount.current) return;
      instance = new Terminal({ cursorBlink: true, fontSize: 12, lineHeight: 1.25, scrollback: 5000, macOptionIsMeta: true, fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace', theme: { background: "#151a16", foreground: "#dce6dd", cursor: "#dce6dd", selectionBackground: "#526e58" } });
      const current = instance;
      terminal.current = current;
      const fit = new FitAddon();
      current.loadAddon(fit); current.open(mount.current);
      current.textarea?.setAttribute("aria-label", "交互式终端输入");
      current.textarea?.setAttribute("aria-describedby", "terminal-help");
      current.attachCustomKeyEventHandler(event => {
        if (event.type !== "keydown") return true;
        if (event.key === "Escape" && event.shiftKey) {
          event.preventDefault(); current.blur(); help.current?.focus(); return false;
        }
        if (event.key.toLowerCase() === "c" && ((event.ctrlKey && (event.shiftKey || current.hasSelection())) || event.metaKey)) {
          event.preventDefault(); void copySelection(current); return false;
        }
        // Let the native paste event reach xterm's bracketed-paste handling.
        if (event.key.toLowerCase() === "v" && (event.ctrlKey || event.metaKey)) return false;
        return true;
      });
      disposables.push(current.onSelectionChange(() => { if (active) setSelected(current.hasSelection()); }));
      const endpoint = new URL(`${API}/api/v1/conversations/${conversationId}/terminal`, window.location.origin);
      endpoint.protocol = endpoint.protocol === "https:" ? "wss:" : "ws:";
      socket = new WebSocket(endpoint);
      const fitTerminal = () => {
        if (!active || !mount.current || mount.current.clientWidth === 0 || mount.current.clientHeight === 0) return;
        fit.fit();
        if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ Type: "resize", Cols: current.cols, Rows: current.rows }));
      };
      resize = new ResizeObserver(() => { cancelAnimationFrame(resizeFrame); resizeFrame = requestAnimationFrame(fitTerminal); });
      resize.observe(mount.current); fitTerminal();
      timeout = window.setTimeout(() => {
        if (active && socket?.readyState === WebSocket.CONNECTING) { setNotice("终端连接超时，请检查网络后重新连接。"); socket.close(); }
      }, 15000);
      socket.onmessage = event => {
        if (!active) return;
        try {
          const message = JSON.parse(event.data);
          const type = message.Type ?? message.type, data = message.Data ?? message.data;
          if (type === "output" && typeof data === "string") current.write(data);
          else if (type === "error") { setConnection("error"); setNotice("终端启动或操作失败，请检查 Computer 状态后重新连接。"); }
        } catch { setNotice("收到无法解析的终端消息，请重新连接。"); }
      };
      socket.onopen = () => {
        if (!active) return;
        clearTimeout(timeout); setConnection("connected"); setNotice(""); fitTerminal();
        if (document.activeElement === originalFocus) current.focus();
        disposables.push(current.onData(data => { if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ Type: "input", Data: data })); }));
      };
      socket.onerror = () => { if (active) { setConnection("error"); setNotice("终端连接失败，请检查 Computer 状态或网络。"); } };
      socket.onclose = () => {
        if (!active) return;
        clearTimeout(timeout);
        setConnection(previous => previous === "error" ? previous : "disconnected");
        current.writeln("\r\n终端连接已结束。重新连接会启动新的 Shell。");
      };
    })().catch(error => {
      if (!active) return;
      cleanup(); setConnection("error");
      if (error instanceof SessionExpired) redirectToLogin();
      else setNotice("终端暂时无法连接，请检查网络或 Computer 状态后重试。");
    });
    return () => { active = false; cleanup(); };
  }, [conversationId, attempt]);

  return <section className="terminal-shell" aria-label="会话终端" onKeyDown={event => event.stopPropagation()}>
    <div className="terminal-toolbar">
      <span className={`terminal-connection ${connection}`} role="status">{labels[connection]}</span>
      <button type="button" disabled={!selected} onClick={() => void copySelection()} title="复制选中内容（Ctrl+Shift+C / ⌘C）">复制</button>
      <button type="button" disabled={connection !== "connected"} onClick={() => { terminal.current?.clear(); terminal.current?.focus(); }} title="清除终端显示，保留 Shell 与命令历史">清屏</button>
      {connection === "error" || connection === "disconnected" ? <button type="button" onClick={() => { setConnection("connecting"); setNotice(""); setSelected(false); setAttempt(value => value + 1); }}>重新连接</button> : null}
      <button ref={help} type="button" aria-label="查看终端快捷键" onClick={() => setNotice("Tab 补全 · ↑↓ 历史 · Ctrl+C 中断（选中内容时复制）· Ctrl+R 搜索历史 · Ctrl+L 清屏 · Shift+Esc 离开终端焦点")}>快捷键</button>
    </div>
    <div className="terminal" ref={mount} />
    <div className="terminal-keybar" aria-label="终端辅助按键">{keys.map(key => <button key={key.label} type="button" title={key.title} aria-label={key.title} disabled={connection !== "connected"} onPointerDown={event => event.preventDefault()} onClick={() => { terminal.current?.input(key.data, true); terminal.current?.focus(); }}>{key.label}</button>)}</div>
    <p id="terminal-help" className={notice ? "terminal-notice" : "sr-only"} role="status">{notice || "Tab 补全，方向键查看历史，Ctrl+C 中断。Shift+Esc 将焦点移至终端工具栏。"}</p>
  </section>;
}
