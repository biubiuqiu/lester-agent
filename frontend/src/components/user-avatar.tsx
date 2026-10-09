"use client";

import { useState } from "react";
import { API, AvatarKey } from "@/lib/api";

export const avatarOptions: { key: AvatarKey; label: string }[] = [
  { key: "forest", label: "森林" },
  { key: "ocean", label: "海洋" },
  { key: "clay", label: "陶土" },
  { key: "lilac", label: "丁香" },
  { key: "amber", label: "琥珀" },
  { key: "graphite", label: "石墨" },
];

export function UserAvatar({ displayName, avatarKey = "forest", avatarURL, size = "medium" }: { displayName?: string; avatarKey?: AvatarKey; avatarURL?: string; size?: "small" | "medium" | "large" }) {
  const [failedURL, setFailedURL] = useState("");
  const source = avatarURL?.startsWith("/api/v1/me/avatar?") ? API + avatarURL : "";
  const initial = Array.from(displayName?.trim() || "U")[0]?.toUpperCase() || "U";
  return <span className={`user-avatar avatar-${avatarKey} ${size}`} aria-hidden="true">{source && failedURL !== source ?
    // The authenticated API owns this image; Next's image proxy cannot carry
    // the user's session. Fetch directly and fall back to the built-in avatar.
    // eslint-disable-next-line @next/next/no-img-element
    <img src={source} alt="" width={64} height={64} onError={() => setFailedURL(source)} />
    : <span>{initial}</span>}</span>;
}
