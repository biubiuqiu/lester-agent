import type { SVGProps } from "react";
import { ProductGlyph } from "./product-glyph";

type IconProps = Omit<SVGProps<SVGSVGElement>, "children"> & { size?: number };

/** Entity aliases share the same geometry, paints and optical sizes everywhere. */
export function ProjectIcon({ personal = false, size = 18, ...props }: IconProps & { personal?: boolean }) {
  return <ProductGlyph kind={personal ? "personal-project" : "project"} size={size} {...props} />;
}
export function ConversationIcon({ size = 18, ...props }: IconProps) {
  return <ProductGlyph kind="conversation" size={size} {...props} />;
}
export function ArtifactIcon({ size = 18, ...props }: IconProps) {
  return <ProductGlyph kind="artifact" size={size} {...props} />;
}
