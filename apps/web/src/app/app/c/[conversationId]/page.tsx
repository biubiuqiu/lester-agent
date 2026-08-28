import {Workspace} from "@/components/workspace";
export default async function ConversationPage({params}:{params:Promise<{conversationId:string}>}){const{conversationId}=await params;return <Workspace conversationId={conversationId}/>}

