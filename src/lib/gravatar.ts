import crypto from "crypto";

export function gravatarUrl(email?: string | null, size = 96): string {
    const normalized = (email || "").trim().toLowerCase();
    if (!normalized || !normalized.includes("@")) return "";

    const hash = crypto.createHash("sha256").update(normalized).digest("hex");
    const safeSize = Math.max(24, Math.min(size, 512));
    return `https://www.gravatar.com/avatar/${hash}?s=${safeSize}&d=identicon&r=g`;
}
