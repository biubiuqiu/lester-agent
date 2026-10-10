import { SiteFooter, SiteHeader } from "@/components/site/site-shell";
import "./site.css";
import "./home.css";

export default function PublicLayout({ children }: { children: React.ReactNode }) {
  return <div className="public-site">
    <a className="site-skip-link" href="#site-main">跳到主要内容</a>
    <SiteHeader />
    {children}
    <SiteFooter />
  </div>;
}
