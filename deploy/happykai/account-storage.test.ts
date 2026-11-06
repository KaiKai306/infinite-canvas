import { expect, test } from "bun:test";
import { storageAccount } from "../../web/src/lib/account-storage";

const token = (userId: unknown) => `fixture.${Buffer.from(JSON.stringify({ userId })).toString("base64url")}.fixture`;

test("distinct identities use distinct partitions; malformed identities stay guests", () => {
    expect(storageAccount(token("newapi-42"))).toBe("newapi-42");
    expect(storageAccount(token("newapi-43"))).toBe("newapi-43");
    expect(storageAccount(token("newapi-42"))).not.toBe(storageAccount(token("newapi-43")));
    for (const value of ["", "invalid", token("../another"), token(42), token(null), token("a".repeat(101))]) {
        expect(storageAccount(value)).toBe("guest");
    }
});
