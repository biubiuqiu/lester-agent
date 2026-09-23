import {AgentLanding} from "@/components/agent-pages";
export default async function AgentPage({params}:{params:Promise<{slug:string}>}){const {slug}=await params;return <AgentLanding slug={slug}/>}
