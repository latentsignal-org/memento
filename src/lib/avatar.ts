import {sha256} from "js-sha256";

export function avatarUrl(email?: string | null, size = 96, initials?: string | null): string {
    const normalized = (email || "").trim().toLowerCase();
    if (!normalized || !normalized.includes("@")) return "";

    const hash = sha256(normalized);
    const bucket = size <= 64 ? 64 : 256;
    const safeInitials = normalizeInitials(initials);
    return `/api/avatar/${hash}?s=${bucket}&i=${encodeURIComponent(safeInitials)}`;
}

export function initialsFromName(name?: string | null, fallbackEmail?: string | null): string {
    const source = (name || "").trim() || emailLocalPart(fallbackEmail);
    const parts = source.split(/\s+/).filter(Boolean);
    const chars: string[] = [];
    for (const part of parts) {
        const match = part.match(/[\p{L}\p{N}]/u);
        if (match) chars.push(match[0].toUpperCase());
        if (chars.length === 2) break;
    }
    if (chars.length === 0) return "?";
    return chars.join("");
}

function normalizeInitials(initials?: string | null): string {
    const chars = Array.from((initials || "").trim())
        .filter((char) => char.trim() && !/[\u0000-\u001f\u007f]/.test(char))
        .slice(0, 2)
        .map((char) => char.toUpperCase());
    return chars.join("") || "?";
}

function emailLocalPart(email?: string | null): string {
    const local = (email || "").trim().split("@")[0] || "";
    return local.replace(/[._-]+/g, " ");
}
