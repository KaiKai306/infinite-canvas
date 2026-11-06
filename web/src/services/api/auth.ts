import { apiGet, apiPost } from "@/services/api/request";
import { AUTH_STORAGE_KEY } from "@/lib/account-storage";

export const AUTH_TOKEN_KEY = AUTH_STORAGE_KEY;

export type UserRole = "guest" | "user" | "admin";

export type AuthUser = {
    id: string;
    username: string;
    displayName: string;
    avatarUrl: string;
    role: UserRole;
    credits: number;
    createdAt: string;
    updatedAt: string;
};

export type AuthSession = {
    token: string;
    user: AuthUser;
};

export type AuthPayload = {
    username: string;
    password: string;
    challengeId?: string;
    verificationCode?: string;
    redirect?: string;
};

export class NewAPIChallengeError extends Error {
    constructor(public challengeId: string) { super("请输入 New API 两步验证码"); }
}

let pendingLogout: Promise<unknown> = Promise.resolve();

export function logout() {
    pendingLogout = pendingLogout.catch(() => undefined).then(() => apiPost<boolean>("/api/auth/logout"));
    return pendingLogout;
}

export async function login(payload: AuthPayload) {
    await pendingLogout.catch(() => undefined);
    const result = await apiPost<AuthSession & { requireTwoFA?: boolean; challengeId?: string }>("/api/auth/login", payload);
    if (result.requireTwoFA && result.challengeId) throw new NewAPIChallengeError(result.challengeId);
    return result;
}

export async function register(payload: AuthPayload) {
    await pendingLogout.catch(() => undefined);
    return apiPost<AuthSession>("/api/auth/register", payload);
}

export async function fetchCurrentUser(token?: string) {
    return apiGet<AuthUser>("/api/auth/me", undefined, token);
}
