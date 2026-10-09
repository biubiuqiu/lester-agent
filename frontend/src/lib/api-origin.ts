// Empty in gateway/Ingress deployments; development may supply an API origin.
export const API = process.env.NEXT_PUBLIC_API_URL || "";
