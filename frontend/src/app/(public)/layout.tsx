
import { T } from "@/components/i18n";
import { SiteFooter, SiteHeader } from "@/components/site/site-shell";
import "./site.css";
import "./home.css";

export default function PublicLayout({ children }: { children: React.ReactNode }) {
  return <div className="public-site">
    <a className="site-skip-link" href="#site-main"><T>{"跳到主要内容"}</T></a>
    <SiteHeader />
    {children}
    <SiteFooter />
  </div>;
}
