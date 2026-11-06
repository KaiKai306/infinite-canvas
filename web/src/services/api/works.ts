import { apiDelete, apiGet } from "./request";

export type MediaWork = {
    id: string; kind: "image" | "video"; title: string; prompt: string; model: string; source: string;
    taskId: string; width: number; height: number; bytes: number; mimeType: string; createdAt: string;
    thumbnailState: string; thumbnailUrl: string; url: string;
};

export function fetchMediaWorks(token: string, query: { keyword: string; type: string; page: number; pageSize: number }) {
    return apiGet<{ items: MediaWork[]; total: number }>("/api/v1/works", query, token);
}

export function removeMediaWork(token: string, id: string) {
    return apiDelete<boolean>(`/api/v1/works/${encodeURIComponent(id)}`, token);
}

export async function downloadMediaWork(token: string, work: MediaWork) {
    const response = await fetch(work.url, { headers: { Authorization: `Bearer ${token}` } });
    const type = response.headers.get("Content-Type") || "";
    if (!response.ok || (!type.startsWith("image/") && !type.startsWith("video/"))) throw new Error("原文件读取失败，请稍后重试");
    return response.blob();
}
