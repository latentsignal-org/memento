const EMAIL_RE = /[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi;

let privacyEnabled = true;

export function isPrivacyEnabled(): boolean {
    return privacyEnabled;
}

export function setPrivacyEnabled(enabled: boolean) {
    privacyEnabled = enabled;
}

export function maskEmail(email?: string | null, overridePrivacyEnabled?: boolean): string {
    const normalized = (email || "").trim().toLowerCase();
    if (!normalized) return "***@***";
    const isPrivate = overridePrivacyEnabled !== undefined ? overridePrivacyEnabled : privacyEnabled;
    if (!isPrivate) {
        return normalized;
    }

    const atIndex = normalized.indexOf("@");
    if (atIndex < 0) return "***";

    const local = normalized.slice(0, atIndex);
    const domain = normalized.slice(atIndex + 1);
    const visible = local.slice(0, Math.min(2, local.length));
    return `${visible}***@${domain}`;
}

export function maskEmailAddresses(text?: string | null, overridePrivacyEnabled?: boolean): string {
    if (!text) return "";
    const isPrivate = overridePrivacyEnabled !== undefined ? overridePrivacyEnabled : privacyEnabled;
    if (!isPrivate) {
        return text;
    }
    return text.replace(EMAIL_RE, (match) => maskEmail(match, isPrivate));
}

export function displayContactName(
    name?: string | null,
    fallbackEmail?: string | null,
    overridePrivacyEnabled?: boolean
): string {
    const normalizedName = (name || "").trim();
    if (normalizedName) return maskEmailAddresses(normalizedName, overridePrivacyEnabled);
    return maskEmail(fallbackEmail, overridePrivacyEnabled);
}

export function contactInitials(name?: string | null, overridePrivacyEnabled?: boolean): string {
    const safeName = displayContactName(name, null, overridePrivacyEnabled);
    const parts = safeName.trim().split(/\s+/).filter(Boolean);
    if (parts.length === 0) return "?";
    if (parts.length === 1) return parts[0].charAt(0).toUpperCase();
    return `${parts[0].charAt(0)}${parts[parts.length - 1].charAt(0)}`.toUpperCase();
}
