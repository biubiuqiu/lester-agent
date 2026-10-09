import { readConversationFile, readConversationFileBytes } from "./api";
import { resolveWorkspaceReference } from "./workspace-reference";

const ignoredScriptAttributes = new Set(["src", "integrity", "crossorigin"]);
function fileExtension(path: string) { return path.split(".").at(-1)?.toLowerCase() ?? ""; }

export async function buildHTMLPreview(conversationId: string, filePath: string, source: string, signal: AbortSignal) {
  const document = new DOMParser().parseFromString(source, "text/html");
  document.querySelectorAll('meta[http-equiv="Content-Security-Policy" i]').forEach((element) => element.remove());
  const policy = document.createElement("meta");
  policy.httpEquiv = "Content-Security-Policy";
  policy.content = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; media-src data: blob:; object-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'";
  document.head.prepend(policy);

  // Keep local navigation in the verified conversation inventory. A srcdoc URL
  // otherwise resolves against the Lester page instead of the source HTML file.
  for (const link of document.querySelectorAll<HTMLAnchorElement>("a[href], area[href]")) {
    const path = resolveWorkspaceReference(filePath, link.getAttribute("href") || "");
    link.removeAttribute("data-lester-preview-path");
    if (path) link.setAttribute("data-lester-preview-path", path);
  }
  const navigation = document.createElement("script");
  navigation.textContent = `document.addEventListener("click", function(event) {
    var link = event.target instanceof Element && event.target.closest("[data-lester-preview-path]");
    if (!link) return;
    event.preventDefault();
    parent.postMessage({ type: "lester:preview:navigate", path: link.getAttribute("data-lester-preview-path") }, "*");
  }, true);`;
  policy.after(navigation);

  const tasks: Promise<void>[] = [];
  const stylesheets = [...document.querySelectorAll<HTMLLinkElement>('link[rel~="stylesheet"][href]')].slice(0, 20);
  for (const link of stylesheets) {
    const assetPath = resolveWorkspaceReference(filePath, link.getAttribute("href") || "");
    if (!assetPath) continue;
    tasks.push(readConversationFile(conversationId, assetPath, signal).then((content) => {
      if (content.length > 512 * 1024) return;
      const style = document.createElement("style");
      style.textContent = content;
      link.replaceWith(style);
    }));
  }

  const scripts = [...document.querySelectorAll<HTMLScriptElement>("script[src]")].slice(0, 20);
  for (const script of scripts) {
    const assetPath = resolveWorkspaceReference(filePath, script.getAttribute("src") || "");
    if (!assetPath) continue;
    tasks.push(readConversationFile(conversationId, assetPath, signal).then((content) => {
      if (content.length > 512 * 1024) return;
      const inline = document.createElement("script");
      for (const attribute of [...script.attributes]) {
        if (!ignoredScriptAttributes.has(attribute.name)) inline.setAttribute(attribute.name, attribute.value);
      }
      inline.textContent = content;
      script.replaceWith(inline);
    }));
  }

  const images = [...document.querySelectorAll<HTMLImageElement>("img[src]")].slice(0, 20);
  for (const image of images) {
    const assetPath = resolveWorkspaceReference(filePath, image.getAttribute("src") || "");
    if (!assetPath) continue;
    tasks.push(readConversationFileBytes(conversationId, assetPath, signal).then((content) => {
      if (content.byteLength > 4 * 1024 * 1024) return;
      image.src = bytesToDataURL(content, imageMIMEType(assetPath));
      image.removeAttribute("srcset");
    }));
  }

  await Promise.allSettled(tasks);
  if (signal.aborted) throw new DOMException("Aborted", "AbortError");
  return `<!doctype html>\n${document.documentElement.outerHTML}`;
}

function imageMIMEType(path: string) {
  const extension = fileExtension(path);
  if (extension === "jpg" || extension === "jpeg") return "image/jpeg";
  if (extension === "svg") return "image/svg+xml";
  if (extension === "ico") return "image/x-icon";
  return `image/${extension || "png"}`;
}

function bytesToDataURL(bytes: Uint8Array, mimeType: string) {
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  }
  return `data:${mimeType};base64,${btoa(binary)}`;
}
