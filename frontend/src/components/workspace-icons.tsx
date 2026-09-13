import type { SVGProps } from "react";

type IconProps = SVGProps<SVGSVGElement> & { size?: number };
function IconFrame({ size = 18, children, ...props }: IconProps) {
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" {...props}>{children}</svg>;
}
export function ProjectIcon(props: IconProps) {
  return <IconFrame {...props}><path d="M3.5 7V5.5A1.5 1.5 0 0 1 5 4h4l2.5 3H19a1.5 1.5 0 0 1 1.5 1.5v10A1.5 1.5 0 0 1 19 20H5a1.5 1.5 0 0 1-1.5-1.5Z"/><path d="M8 12h8M8 16h5"/></IconFrame>;
}
export function ConversationIcon(props: IconProps) {
  return <IconFrame {...props}><path d="M7 18 3.5 21V6A2.5 2.5 0 0 1 6 3.5h12A2.5 2.5 0 0 1 20.5 6v9.5A2.5 2.5 0 0 1 18 18Z"/><path d="M8 8.5h8M8 13h5"/></IconFrame>;
}
export function ArtifactIcon(props: IconProps) {
  return <IconFrame {...props}><rect x="3" y="4" width="18" height="16" rx="3"/><path d="M3 9h18M7 6.5h.01M10 6.5h.01M7 13h4v4H7zM14 13h3M14 17h3"/></IconFrame>;
}
