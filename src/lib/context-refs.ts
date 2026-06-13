export type ContextRef =
    | {
    kind: "person";
    person_id: number;
    slug: string;
    label: string;
}
    | {
    kind: "project";
    slug: string;
    label: string;
}
    | {
    kind: "concept";
    slug: string;
    label: string;
}
    | {
    kind: "ask_session";
    session_id: number;
    slug: string;
    label: string;
};

export interface ContextSearchResult {
    kind: ContextRef["kind"];
    id: string;
    slug: string;
    label: string;
    subtitle?: string;
}

export function refToken(ref: ContextRef): string {
    return `${ref.kind === "person" ? "@" : "#"}${ref.label}`;
}

export function contextSearchResultToRef(result: ContextSearchResult): ContextRef {
    switch (result.kind) {
        case "person":
            return {
                kind: "person",
                person_id: Number.parseInt(result.id, 10),
                slug: result.slug,
                label: result.label,
            };
        case "ask_session":
            return {
                kind: "ask_session",
                session_id: Number.parseInt(result.id, 10),
                slug: result.slug,
                label: result.label,
            };
        case "project":
            return {kind: "project", slug: result.slug, label: result.label};
        case "concept":
            return {kind: "concept", slug: result.slug, label: result.label};
    }
}

export function pruneContextRefs(text: string, refs: ContextRef[]): ContextRef[] {
    return refs.filter((ref) => text.includes(refToken(ref)));
}

export function encodeContextRefs(refs: ContextRef[]): string {
    return JSON.stringify(refs);
}

export function decodeContextRefs(raw: string | null): ContextRef[] {
    if (!raw) return [];
    try {
        const parsed = JSON.parse(raw) as ContextRef[];
        return Array.isArray(parsed) ? parsed : [];
    } catch {
        return [];
    }
}
