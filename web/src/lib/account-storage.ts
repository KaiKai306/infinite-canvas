// A browser partition, not authentication. The server verifies every account request.
export const AUTH_STORAGE_KEY = "infinite-canvas-auth-token-v1";

export function storageAccount(token: string): string {
    try {
        const payload = JSON.parse(atob(token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/")));
        return typeof payload.userId === "string" && /^[a-zA-Z0-9_-]{1,100}$/.test(payload.userId) ? payload.userId : "guest";
    } catch { return "guest"; }
}

function currentAccount() {
    if (typeof window === "undefined") return "guest";
    try { return storageAccount(JSON.parse(localStorage.getItem(AUTH_STORAGE_KEY) || "null")?.state?.token || ""); }
    catch { return "guest"; }
}

// Fixed for this document. Login/logout reload before another account can sync these stores.
export const accountStorageScope = currentAccount();
export const accountDatabaseName = `infinite-canvas-${accountStorageScope}`;

if (typeof window !== "undefined") {
    window.addEventListener("storage", (event) => {
        if ((event.key === AUTH_STORAGE_KEY || event.key === null) && currentAccount() !== accountStorageScope) window.location.reload();
    });
}
