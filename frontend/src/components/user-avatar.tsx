import { AvatarKey } from "@/lib/api";

export const avatarOptions: { key: AvatarKey; label: string }[] = [
  { key: "forest", label: "森林" },
  { key: "ocean", label: "海洋" },
  { key: "clay", label: "陶土" },
  { key: "lilac", label: "丁香" },
  { key: "amber", label: "琥珀" },
  { key: "graphite", label: "石墨" },
];

export function UserAvatar({ displayName, avatarKey = "forest", size = "medium" }: { displayName?: string; avatarKey?: AvatarKey; size?: "small" | "medium" | "large" }) {
  const initial = Array.from(displayName?.trim() || "U")[0]?.toUpperCase() || "U";
  return <span className={`user-avatar avatar-${avatarKey} ${size}`} aria-hidden="true"><span>{initial}</span></span>;
}
