import React from "react";

/**
 * CitationChip renders a styled pill linking to a source message.
 * Visual reference: Figma node 1:455 (mint background, dark green text).
 *
 * For the hackathon we don't navigate to a source viewer — the chip carries
 * the message_id in its title attribute so hovering reveals the source. Add
 * navigation later by lifting to a Link.
 */
export function CitationChip({messageId, index}: { messageId: number; index?: number }) {
    return (
        <span
            title={`Source: message #${messageId}`}
            className="inline-flex items-center justify-center align-baseline px-1.5 py-0.5 mx-0.5 rounded-[3px] bg-primary-fixed text-on-primary-fixed text-[10px] font-semibold leading-none cursor-help select-none translate-y-[-1px]"
        >
      {index !== undefined ? index : messageId}
    </span>
    );
}

/**
 * renderCitedText takes prose containing inline `[msg:NNNN]` or
 * `[msg:NNNN, msg:MMMM]` markers and returns a React node where each marker
 * has been replaced by one or more CitationChip elements. Plain text is
 * preserved as-is.
 *
 * If `numbered` is true, each unique message_id is assigned a sequential
 * [1], [2], [3] index across the entire text — matching the Figma reference.
 * Otherwise the raw message_id is shown inside the chip.
 */
export function renderCitedText(text: string, opts: { numbered?: boolean } = {}): React.ReactNode {
    if (!text) return null;

    const indexMap = new Map<number, number>();
    let nextIndex = 1;
    const assignIndex = (id: number): number => {
        if (!opts.numbered) return id;
        if (!indexMap.has(id)) {
            indexMap.set(id, nextIndex++);
        }
        return indexMap.get(id)!;
    };

    // Match groups like `[msg:1234]`, `[msg:1234, msg:5678]`, and the LLM's
    // compact compound form `[msg:1234, 5678, 9012]` (msg: prefix appears once
    // for a multi-id group). Tolerant of casing and whitespace.
    const groupRe = /\[\s*msg:\s*\d+(?:\s*[,;]\s*(?:msg:\s*)?\d+)*\s*\]/gi;
    const idRe = /\d+/g;

    const parts: React.ReactNode[] = [];
    let lastIndex = 0;
    let match: RegExpExecArray | null;

    while ((match = groupRe.exec(text)) !== null) {
        if (match.index > lastIndex) {
            parts.push(text.slice(lastIndex, match.index));
        }
        const idsRaw = match[0];
        const ids: number[] = [];
        let idMatch: RegExpExecArray | null;
        const idIterator = new RegExp(idRe.source, "g");
        while ((idMatch = idIterator.exec(idsRaw)) !== null) {
            ids.push(parseInt(idMatch[0], 10));
        }
        parts.push(
            <span key={`cite-${match.index}`} className="inline-flex items-center gap-0.5 align-baseline">
        {ids.map((id, i) => (
            <CitationChip key={`${match!.index}-${i}-${id}`} messageId={id} index={assignIndex(id)}/>
        ))}
      </span>
        );
        lastIndex = match.index + match[0].length;
    }

    if (lastIndex < text.length) {
        parts.push(text.slice(lastIndex));
    }

    return parts.length ? parts : text;
}

/**
 * citationIdsFrom extracts all message_ids referenced inside `[msg:NNNN]`
 * markers in a string. Useful for sidebar source-counts.
 */
export function citationIdsFrom(text: string): number[] {
    if (!text) return [];
    const set = new Set<number>();
    const re = /msg:\s*(\d+)/gi;
    let m: RegExpExecArray | null;
    while ((m = re.exec(text)) !== null) {
        set.add(parseInt(m[1], 10));
    }
    return Array.from(set);
}

/**
 * slugify converts a canonical name to a URL-friendly slug.
 * "Jane Smith" -> "ann-catherine-jose"
 */
export function slugify(name: string): string {
    return name
        .toLowerCase()
        .normalize("NFKD")
        .replace(/[̀-ͯ]/g, "")
        .replace(/[^a-z0-9\s-]/g, "")
        .trim()
        .replace(/\s+/g, "-")
        .replace(/-+/g, "-");
}
