import { apiDelete, apiGet, apiPost } from "./request";

export type ImageAPIConfig = {
    channels: Array<{ id: string; name: string; protocol: string; models: string[] }>;
    defaultChannelId: string; defaultModel: string; archiveEnabled: boolean;
};
export type ImageAPICredential = {
    id: string; name: string; prefix: string; channelId: string; model: string;
    createdAt: string; expiresAt: string; lastUsedAt: string; revokedAt: string;
};
export const fetchImageAPIConfig = (token: string) => apiGet<ImageAPIConfig>("/api/v1/image-api/config", undefined, token);
export const fetchImageAPICredentials = (token: string) => apiGet<ImageAPICredential[]>("/api/v1/image-api/credentials", undefined, token);
export const createImageAPICredential = (token: string, input: { name: string; channelId: string; model: string; days: number }) =>
    apiPost<{ credential: ImageAPICredential; secret: string }>("/api/v1/image-api/credentials", input, token);
export const revokeImageAPICredential = (token: string, id: string) => apiDelete<boolean>(`/api/v1/image-api/credentials/${encodeURIComponent(id)}`, token);
