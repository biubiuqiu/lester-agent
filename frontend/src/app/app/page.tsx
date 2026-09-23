import {Workspace} from "@/components/workspace";
export default async function AppHome({searchParams}:{searchParams:Promise<{agent?:string}>}){const {agent}=await searchParams;return <Workspace initialAgentSlug={agent}/>}

