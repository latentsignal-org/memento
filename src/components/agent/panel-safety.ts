import {maskEmailAddresses} from "@/lib/contact-display";

export function safePreviewText(text: string, limit = 180): string {
    const masked = maskEmailAddresses(text).replace(/\s+/g, " ").trim();
    return masked.length > limit ? `${masked.slice(0, limit - 3)}...` : masked;
}

export function safePreviewJson(value: unknown, limit = 220): string {
    try {
        const text = typeof value === "string" ? value : JSON.stringify(value);
        if (!text) return "empty";
        return safePreviewText(text, limit);
    } catch {
        return "unavailable";
    }
}

