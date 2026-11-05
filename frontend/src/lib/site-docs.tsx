import { siteDocNavigation, type SiteDocMetadata } from "./site-doc-navigation";
import type { SiteDocSection } from "./site-docs/types";
import { sections as gettingStartedSections } from "./site-docs/getting-started";
import { sections as usageSections } from "./site-docs/usage";
import { sections as personalAssistantSections } from "./site-docs/personal-assistant";
import { sections as projectSections } from "./site-docs/projects-and-files";
import { sections as agentContextSections } from "./site-docs/agents-and-context";
import { sections as connectionSections } from "./site-docs/connections";
import { sections as automationSections } from "./site-docs/automations";
import { sections as speechSections } from "./site-docs/speech";
import { sections as mobileSections } from "./site-docs/mobile";
import { sections as modelSections } from "./site-docs/models";
import { sections as deploymentSections } from "./site-docs/deployment";
import { sections as troubleshootingSections } from "./site-docs/troubleshooting";

export { docURL } from "./site-doc-navigation";
export type SiteDoc = SiteDocMetadata & { note?: string; sections: SiteDocSection[] };

const articles: Record<string, Pick<SiteDoc, "note" | "sections">> = {
  "": { sections: gettingStartedSections, note: "适用于首次 Docker Compose 部署。" },
  "usage": { sections: usageSections },
  "personal-assistant": { sections: personalAssistantSections },
  "projects-and-files": { sections: projectSections },
  "agents-and-context": { sections: agentContextSections },
  "connections": { sections: connectionSections },
  "automations": { sections: automationSections },
  "speech": { sections: speechSections },
  "mobile": { sections: mobileSections },
  "models": { sections: modelSections },
  "deployment": { sections: deploymentSections },
  "troubleshooting": { sections: troubleshootingSections },
};

export const siteDocs: SiteDoc[] = siteDocNavigation.map(metadata => {
  const article = articles[metadata.slug];
  if (!article) throw new Error(`Missing documentation article: ${metadata.slug}`);
  return { ...metadata, ...article };
});

export function getSiteDoc(slug: string) { return siteDocs.find(doc => doc.slug === slug); }
