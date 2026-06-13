export function formatMonth(value: string | undefined): string {
    if (!value) return "Unknown";
    return new Date(value).toLocaleDateString("en-US", {month: "short", year: "numeric"});
}

export function formatMonthDay(value: string | undefined): string {
    if (!value) return "Unknown";
    return new Date(value).toLocaleDateString("en-US", {month: "short", day: "numeric", year: "numeric"});
}

// Relative time for human/relationship recency signals.
// ≤7 days: relative ("today", "yesterday", "3 days ago")
// 8–30 days: exact date ("Mar 15, 2026") — "2 weeks ago" is too vague
// >30 days: month+year ("Mar 2026")
export function relativeDate(value: string): string {
    if (!value) return "Unknown";
    const now = new Date();
    const date = new Date(value);
    const diffDays = Math.max(0, Math.round((now.getTime() - date.getTime()) / (1000 * 60 * 60 * 24)));
    if (diffDays === 0) return "today";
    if (diffDays === 1) return "yesterday";
    if (diffDays <= 7) return `${diffDays} days ago`;
    if (diffDays <= 30) return formatMonthDay(value);
    return formatMonth(value);
}
