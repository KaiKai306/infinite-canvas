import { accountDatabaseName, accountStorageScope } from "@/lib/account-storage";
import localforage from "localforage";
import type { StateStorage } from "zustand/middleware";

localforage.config({
    name: accountDatabaseName,
    storeName: "app_state",
});

export const localForageStorage: StateStorage = {
    getItem: async (name) => {
        if (typeof window === "undefined") return null;
        try {
            return (await localforage.getItem<string>(name)) || null;
        } catch {
            return window.localStorage.getItem(`${name}:${accountStorageScope}`);
        }
    },
    setItem: async (name, value) => {
        if (typeof window === "undefined") return;
        try {
            await localforage.setItem(name, value);
        } catch {
            window.localStorage.setItem(`${name}:${accountStorageScope}`, value);
        }
    },
    removeItem: async (name) => {
        if (typeof window === "undefined") return;
        try {
            await localforage.removeItem(name);
        } catch {
            window.localStorage.removeItem(`${name}:${accountStorageScope}`);
        }
    },
};
