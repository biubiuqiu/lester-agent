import {AgentEditor} from "@/components/agent-pages";
export default async function EditAgentPage({params}:{params:Promise<{slug:string}>}){const {slug}=await params;return <AgentEditor slug={slug}/>}
