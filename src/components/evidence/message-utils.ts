export function normalizePreviewText(value?: string) {
    return cleanPreviewArtifacts(
        humanizeLinks(decodeHTMLEntities((value || "").replace(/\s+/g, " ").trim())),
    );
}

export function bestPreviewExcerpt(subject?: string, snippet?: string, bodyText?: string) {
    const cleanSubject = normalizePreviewText(subject);
    const cleanBody = normalizePreviewText(bodyText);
    const cleanSnippet = normalizePreviewText(snippet);
    let preview = cleanBody || cleanSnippet;
    if (!preview) {
        return "";
    }
    if (cleanSubject) {
        const loweredSubject = cleanSubject.toLowerCase();
        if (preview.toLowerCase().startsWith(loweredSubject)) {
            preview = preview.slice(cleanSubject.length).trim();
        }
    }
    if (cleanSnippet && cleanBody && preview.toLowerCase().startsWith(cleanSnippet.toLowerCase())) {
        preview = preview.slice(cleanSnippet.length).trim();
    }
    preview = preview.replace(/^(?:[-:|.,;]\s*)+/, "").trim();
    if (!preview) {
        preview = cleanSnippet || cleanBody;
    }
    if (preview.length > 220) {
        preview = `${preview.slice(0, 220).trimEnd()}...`;
    }
    return preview;
}

export function buildPreviewParagraphs(
    bodyText?: string,
    snippet?: string,
    expanded = false,
) {
    const bodyBlocks = buildPreviewBlocks(bodyText);
    const body = bodyBlocks.join("\n\n");
    const cleanSnippet = normalizePreviewText(snippet);
    const preview = body || cleanSnippet;
    const compactCharLimit = 220;
    if (!preview) {
        return {
            paragraphs: ["No preview text available for this message yet."],
            truncated: false,
        };
    }

    if (bodyBlocks.length > 0) {
        if (expanded) {
            return {
                paragraphs: bodyBlocks,
                truncated: body.length > compactCharLimit || bodyBlocks.length > 4,
            };
        }

        const compactParagraphs: string[] = [];
        let usedChars = 0;
        for (const paragraph of bodyBlocks) {
            if (compactParagraphs.length >= 4 || usedChars >= compactCharLimit) {
                break;
            }
            const remaining = compactCharLimit - usedChars;
            if (paragraph.length <= remaining) {
                compactParagraphs.push(paragraph);
                usedChars += paragraph.length + 2;
                continue;
            }

            const sliced = paragraph.slice(0, Math.max(remaining - 3, 0)).trimEnd();
            compactParagraphs.push(sliced ? `${sliced}...` : "...");
            usedChars = compactCharLimit;
            break;
        }

        return {
            paragraphs: compactParagraphs,
            truncated: compactParagraphs.join("\n\n").trim() !== body.trim(),
        };
    }

    const segments = preview
        .split(/(?<=[.!?])\s+/)
        .map((segment) => segment.trim())
        .filter(Boolean);

    if (segments.length === 0) {
        return {
            paragraphs: [preview],
            truncated: false,
        };
    }

    const paragraphs: string[] = [];
    for (let i = 0; i < segments.length; i += 2) {
        paragraphs.push(segments.slice(i, i + 2).join(" "));
    }

    if (expanded) {
        return {
            paragraphs,
            truncated: preview.length > compactCharLimit || paragraphs.length > 4,
        };
    }

    const compactParagraphs: string[] = [];
    let usedChars = 0;
    for (const paragraph of paragraphs) {
        if (compactParagraphs.length >= 4 || usedChars >= compactCharLimit) {
            break;
        }
        const remaining = compactCharLimit - usedChars;
        if (paragraph.length <= remaining) {
            compactParagraphs.push(paragraph);
            usedChars += paragraph.length + 1;
            continue;
        }

        const sliced = paragraph.slice(0, Math.max(remaining - 3, 0)).trimEnd();
        compactParagraphs.push(sliced ? `${sliced}...` : "...");
        usedChars = compactCharLimit;
        break;
    }

    const compactText = compactParagraphs.join(" ");
    return {
        paragraphs: compactParagraphs,
        truncated: compactText.trim() !== preview.trim(),
    };
}

function buildPreviewBlocks(value?: string) {
    if (!value) {
        return [];
    }

    const cleaned = cleanPreviewBodyArtifacts(
        humanizeLinks(
            decodeHTMLEntities(value)
                .replace(/\r\n?/g, "\n")
                .trim(),
        ),
    );

    if (!cleaned) {
        return [];
    }

    return cleaned
        .split(/\n{2,}/)
        .map((block) => {
            const lines = block
                .split("\n")
                .map((line) => line.trim())
                .filter(Boolean);
            if (lines.length === 0) {
                return "";
            }
            if (lines.every((line) => line.startsWith("- "))) {
                return lines.join("\n");
            }
            return lines.join(" ");
        })
        .map((block) => block.trim())
        .filter(Boolean)
        .slice(0, 8);
}

function humanizeLinks(value: string) {
    return value.replace(/https?:\/\/\S+/gi, (rawUrl) => {
        const trimmed = rawUrl.replace(/[)\].,;!?]+$/g, "");
        const suffix = rawUrl.slice(trimmed.length);
        try {
            const parsed = new URL(trimmed);
            const host = parsed.hostname.replace(/^www\./, "");
            return `[Link: ${host}]${suffix}`;
        } catch {
            return `[Link]${suffix}`;
        }
    });
}

function cleanPreviewArtifacts(value: string) {
    return value
        .replace(
            /[\u00ad\u034f\u061c\u115f\u1160\u17b4\u17b5\u180b-\u180f\u200b-\u200f\u2060-\u206f\u3164\ufeff]+/g,
            "",
        )
        .replace(/\[\s*\[Link:\s*([^\]]+)\]\s*\]/g, "[Link: $1]")
        .replace(/\[\s+\]/g, "")
        .replace(/\s{2,}/g, " ")
        .trim();
}

function cleanPreviewBodyArtifacts(value: string) {
    return value
        .replace(
            /[\u00ad\u034f\u061c\u115f\u1160\u17b4\u17b5\u180b-\u180f\u200b-\u200f\u2060-\u206f\u3164\ufeff]+/g,
            "",
        )
        .replace(/\[\s*\[Link:\s*([^\]]+)\]\s*\]/g, "[Link: $1]")
        .replace(/\[\s+\]/g, "")
        .replace(/[ \t]+/g, " ")
        .replace(/\n{3,}/g, "\n\n")
        .trim();
}

function decodeHTMLEntities(value: string) {
    return value
        .replace(/&#(\d+);/g, (_, code) => String.fromCodePoint(Number(code)))
        .replace(/&#x([0-9a-f]+);/gi, (_, code) => String.fromCodePoint(parseInt(code, 16)))
        .replace(/&quot;/g, '"')
        .replace(/&#39;|&apos;/g, "'")
        .replace(/&amp;/g, "&")
        .replace(/&lt;/g, "<")
        .replace(/&gt;/g, ">");
}
