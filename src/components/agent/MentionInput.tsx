"use client";

import {type KeyboardEvent, useEffect, useLayoutEffect, useMemo, useRef, useState,} from "react";
import {
    type ContextRef,
    type ContextSearchResult,
    contextSearchResultToRef,
    pruneContextRefs,
    refToken,
} from "@/lib/context-refs";

interface MentionInputProps {
    value: string;
    onChange: (value: string, refs: ContextRef[]) => void;
    refs: ContextRef[];
    placeholder?: string;
    disabled?: boolean;
    inputClassName?: string;
    autoComplete?: string;
}

type ActiveMention = {
    trigger: "@" | "#";
    query: string;
    start: number;
    end: number;
};

type ValueSegment =
    | { type: "text"; text: string; key: string }
    | { type: "ref"; ref: ContextRef; token: string; key: string };

const TOKEN_BASE_CLASS =
    "mx-0.5 inline-flex max-w-[18rem] select-none items-center align-baseline rounded-full border px-2 py-0.5 text-[0.85em] font-semibold leading-5 shadow-sm";

function findActiveMention(value: string, cursor: number): ActiveMention | null {
    const prefix = value.slice(0, cursor);
    const match = prefix.match(/(^|[\s([{])([@#])([^\s@#]*)$/);
    if (!match) return null;
    const leading = match[1] ?? "";
    const trigger = match[2] as "@" | "#";
    const query = match[3] ?? "";
    const start = (match.index ?? 0) + leading.length;
    return {trigger, query, start, end: cursor};
}

function activeEquals(a: ActiveMention | null, b: ActiveMention | null) {
    if (!a || !b) return a === b;
    return a.trigger === b.trigger && a.query === b.query && a.start === b.start && a.end === b.end;
}

function contextRefKey(ref: ContextRef): string {
    switch (ref.kind) {
        case "person":
            return `person:${ref.person_id}`;
        case "ask_session":
            return `ask_session:${ref.session_id}`;
        case "project":
            return `project:${ref.slug}`;
        case "concept":
            return `concept:${ref.slug}`;
    }
}

function dedupeRefs(refs: ContextRef[]): ContextRef[] {
    const seen = new Set<string>();
    const deduped: ContextRef[] = [];
    for (const ref of refs) {
        const key = contextRefKey(ref);
        if (seen.has(key)) continue;
        seen.add(key);
        deduped.push(ref);
    }
    return deduped;
}

function resultSubtitle(result: ContextSearchResult) {
    if (result.subtitle) return result.subtitle;
    switch (result.kind) {
        case "person":
            return "Person";
        case "ask_session":
            return "Session";
        case "project":
            return "Project";
        case "concept":
            return "Concept";
    }
}

function kindLabel(kind: ContextSearchResult["kind"]) {
    switch (kind) {
        case "person":
            return "Person";
        case "ask_session":
            return "Session";
        case "project":
            return "Project";
        case "concept":
            return "Concept";
    }
}

function tokenTone(kind: ContextRef["kind"]) {
    if (kind === "person") {
        return "border-primary/25 bg-primary-fixed/80 text-primary";
    }
    if (kind === "ask_session") {
        return "border-tertiary/25 bg-tertiary-fixed/60 text-on-tertiary-fixed";
    }
    return "border-secondary-container bg-secondary-container/70 text-on-secondary-container";
}

function tokenElement(ref: ContextRef, token: string) {
    const element = document.createElement("span");
    element.contentEditable = "false";
    element.dataset.contextToken = token;
    element.className = `${TOKEN_BASE_CLASS} ${tokenTone(ref.kind)}`;
    element.title = token;
    element.textContent = token;
    return element;
}

function popupPositionTargets(element: HTMLElement | null): Array<HTMLElement | Window> {
    const targets: Array<HTMLElement | Window> = [window];
    let node = element?.parentElement ?? null;
    while (node) {
        const style = window.getComputedStyle(node);
        const overflow = `${style.overflow} ${style.overflowX} ${style.overflowY}`;
        if (/(auto|scroll|overlay)/.test(overflow)) {
            targets.push(node);
        }
        node = node.parentElement;
    }
    return targets;
}

function tokenizeValue(value: string, refs: ContextRef[]): ValueSegment[] {
    if (!value) return [];
    const entries = refs
        .map((ref) => ({ref, token: refToken(ref), key: contextRefKey(ref)}))
        .sort((a, b) => b.token.length - a.token.length);
    const segments: ValueSegment[] = [];
    let index = 0;
    let textStart = 0;

    while (index < value.length) {
        const match = entries.find((entry) => value.startsWith(entry.token, index));
        if (!match) {
            index += 1;
            continue;
        }
        if (textStart < index) {
            segments.push({
                type: "text",
                text: value.slice(textStart, index),
                key: `text:${textStart}:${index}`,
            });
        }
        segments.push({
            type: "ref",
            ref: match.ref,
            token: match.token,
            key: `ref:${match.key}:${index}`,
        });
        index += match.token.length;
        textStart = index;
    }

    if (textStart < value.length) {
        segments.push({
            type: "text",
            text: value.slice(textStart),
            key: `text:${textStart}:${value.length}`,
        });
    }
    return segments;
}

function serializeEditor(root: HTMLElement): string {
    const readNode = (node: ChildNode): string => {
        if (node.nodeType === Node.TEXT_NODE) {
            return node.textContent ?? "";
        }
        if (!(node instanceof HTMLElement)) {
            return "";
        }
        const token = node.dataset.contextToken;
        if (token) return token;
        if (node.tagName === "BR") return "";
        return Array.from(node.childNodes).map(readNode).join("");
    };
    return Array.from(root.childNodes).map(readNode).join("");
}

function caretOffset(root: HTMLElement): number {
    const selection = window.getSelection();
    if (!selection || selection.rangeCount === 0) return serializeEditor(root).length;
    const range = selection.getRangeAt(0);
    if (!root.contains(range.startContainer)) return serializeEditor(root).length;
    const prefix = range.cloneRange();
    prefix.selectNodeContents(root);
    prefix.setEnd(range.startContainer, range.startOffset);
    return prefix.toString().length;
}

function placeCaretAtOffset(root: HTMLElement, offset: number) {
    const selection = window.getSelection();
    if (!selection) return;

    let remaining = Math.max(0, offset);
    const range = document.createRange();
    const setAtEnd = () => {
        range.selectNodeContents(root);
        range.collapse(false);
    };

    const walk = (node: ChildNode): boolean => {
        if (node.nodeType === Node.TEXT_NODE) {
            const text = node.textContent ?? "";
            if (remaining <= text.length) {
                range.setStart(node, remaining);
                range.collapse(true);
                return true;
            }
            remaining -= text.length;
            return false;
        }

        if (!(node instanceof HTMLElement)) return false;
        const token = node.dataset.contextToken;
        if (token) {
            if (remaining <= 0) {
                range.setStartBefore(node);
                range.collapse(true);
                return true;
            }
            if (remaining <= token.length) {
                range.setStartAfter(node);
                range.collapse(true);
                return true;
            }
            remaining -= token.length;
            return false;
        }

        for (const child of Array.from(node.childNodes)) {
            if (walk(child)) return true;
        }
        return false;
    };

    const placed = Array.from(root.childNodes).some(walk);
    if (!placed) setAtEnd();
    selection.removeAllRanges();
    selection.addRange(range);
}

function tokenDeletionBefore(value: string, refs: ContextRef[], offset: number) {
    const before = value.slice(0, offset);
    const entries = refs
        .map((ref) => ({token: refToken(ref)}))
        .sort((a, b) => b.token.length - a.token.length);
    for (const {token} of entries) {
        if (before.endsWith(`${token} `)) {
            return {start: offset - token.length - 1, end: offset};
        }
        if (before.endsWith(token)) {
            return {start: offset - token.length, end: offset};
        }
    }
    return null;
}

function tokenDeletionAfter(value: string, refs: ContextRef[], offset: number) {
    const after = value.slice(offset);
    const entries = refs
        .map((ref) => ({token: refToken(ref)}))
        .sort((a, b) => b.token.length - a.token.length);
    for (const {token} of entries) {
        if (after.startsWith(`${token} `)) {
            return {start: offset, end: offset + token.length + 1};
        }
        if (after.startsWith(token)) {
            return {start: offset, end: offset + token.length};
        }
    }
    return null;
}

export default function MentionInput({
                                         value,
                                         onChange,
                                         refs,
                                         placeholder,
                                         disabled,
                                         inputClassName,
                                         autoComplete = "off",
                                     }: MentionInputProps) {
    const editorRef = useRef<HTMLDivElement | null>(null);
    const highlightedRef = useRef<HTMLButtonElement | null>(null);
    const pendingCaretOffsetRef = useRef<number | null>(null);
    const [active, setActive] = useState<ActiveMention | null>(null);
    const [results, setResults] = useState<ContextSearchResult[]>([]);
    const [highlighted, setHighlighted] = useState(0);
    const [direction, setDirection] = useState<"above" | "below">("above");
    const segments = useMemo(() => tokenizeValue(value, refs), [value, refs]);
    const renderSignature = useMemo(
        () => `${value}\n${refs.map(contextRefKey).join("|")}`,
        [value, refs],
    );

    useLayoutEffect(() => {
        if (!editorRef.current) return;
        const editor = editorRef.current;
        const shouldRestoreCaret = document.activeElement === editor;
        const nextCaretOffset = pendingCaretOffsetRef.current ?? (shouldRestoreCaret ? caretOffset(editor) : null);

        if (editor.dataset.renderSignature !== renderSignature) {
            const fragment = document.createDocumentFragment();
            for (const segment of segments) {
                if (segment.type === "text") {
                    fragment.append(document.createTextNode(segment.text));
                } else {
                    fragment.append(tokenElement(segment.ref, segment.token));
                }
            }
            editor.replaceChildren(fragment);
            editor.dataset.renderSignature = renderSignature;
        }

        if (nextCaretOffset !== null && shouldRestoreCaret) {
            placeCaretAtOffset(editor, nextCaretOffset);
        }
        pendingCaretOffsetRef.current = null;
    }, [renderSignature, segments]);

    useEffect(() => {
        highlightedRef.current?.scrollIntoView({block: "nearest"});
    }, [highlighted, results]);

    useEffect(() => {
        if (!value) {
            let cancelled = false;
            window.queueMicrotask(() => {
                if (cancelled) return;
                setActive(null);
                setResults([]);
            });
            return () => {
                cancelled = true;
            };
        }
    }, [value]);

    useEffect(() => {
        if (!active || !editorRef.current) return;
        let frame = 0;
        const updateDirection = () => {
            if (frame) return;
            frame = window.requestAnimationFrame(() => {
                frame = 0;
                const editor = editorRef.current;
                if (!editor) return;
                const rect = editor.getBoundingClientRect();
                const spaceAbove = rect.top;
                const spaceBelow = window.innerHeight - rect.bottom;
                setDirection(spaceBelow > spaceAbove ? "below" : "above");
            });
        };
        updateDirection();
        const targets = popupPositionTargets(editorRef.current);
        targets.forEach((target) => {
            target.addEventListener("scroll", updateDirection, {passive: true});
        });
        window.addEventListener("resize", updateDirection);
        return () => {
            if (frame) window.cancelAnimationFrame(frame);
            targets.forEach((target) => {
                target.removeEventListener("scroll", updateDirection);
            });
            window.removeEventListener("resize", updateDirection);
        };
    }, [active, results]);

    const updateActive = () => {
        const editor = editorRef.current;
        if (!editor) return;
        const nextValue = serializeEditor(editor);
        const nextActive = findActiveMention(nextValue, caretOffset(editor));
        setActive((current) => (activeEquals(current, nextActive) ? current : nextActive));
        if (!nextActive) {
            setResults([]);
        }
    };

    useEffect(() => {
        if (!active) return;
        const controller = new AbortController();
        const timer = window.setTimeout(async () => {
            try {
                const params = new URLSearchParams({
                    trigger: active.trigger,
                    q: active.query,
                });
                const res = await fetch(`/api/context-search?${params.toString()}`, {
                    cache: "no-store",
                    signal: controller.signal,
                });
                if (!res.ok) return;
                const data = (await res.json()) as { results?: ContextSearchResult[] };
                setResults(Array.isArray(data.results) ? data.results : []);
                setHighlighted(0);
            } catch (err) {
                if (!controller.signal.aborted) {
                    console.warn("context search failed", err);
                }
            }
        }, 120);
        return () => {
            controller.abort();
            window.clearTimeout(timer);
        };
    }, [active]);

    const selectedKeys = useMemo(() => new Set(refs.map(contextRefKey)), [refs]);

    const updateValueFromEditor = () => {
        const editor = editorRef.current;
        if (!editor) return;
        const nextValue = serializeEditor(editor);
        pendingCaretOffsetRef.current = caretOffset(editor);
        onChange(nextValue, pruneContextRefs(nextValue, refs));
        window.requestAnimationFrame(updateActive);
    };

    const updateValueAfterTokenDelete = (start: number, end: number) => {
        const nextValue = `${value.slice(0, start)}${value.slice(end)}`;
        pendingCaretOffsetRef.current = start;
        onChange(nextValue, pruneContextRefs(nextValue, refs));
        setActive(null);
        setResults([]);
    };

    const selectResult = (result: ContextSearchResult) => {
        if (!active) return;
        const nextRef = contextSearchResultToRef(result);
        const token = refToken(nextRef);
        const nextValue = `${value.slice(0, active.start)}${token} ${value.slice(active.end)}`;
        const nextRefs = dedupeRefs(pruneContextRefs(nextValue, [...refs, nextRef]));
        pendingCaretOffsetRef.current = active.start + token.length + 1;
        onChange(nextValue, nextRefs);
        setActive(null);
        setResults([]);
        window.requestAnimationFrame(() => editorRef.current?.focus());
    };

    const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
        if (disabled) return;
        if (active && results.length > 0) {
            if (event.key === "ArrowDown") {
                event.preventDefault();
                setHighlighted((current) => (current + 1) % results.length);
                return;
            }
            if (event.key === "ArrowUp") {
                event.preventDefault();
                setHighlighted((current) => (current - 1 + results.length) % results.length);
                return;
            }
            if (event.key === "Enter" || event.key === "Tab") {
                event.preventDefault();
                selectResult(results[highlighted]);
                return;
            }
        }
        if (active && (event.key === "Enter" || event.key === "Tab")) {
            event.preventDefault();
            return;
        }

        if (event.key === "Escape") {
            setActive(null);
            setResults([]);
            return;
        }

        if (event.key === "Enter") {
            event.preventDefault();
            editorRef.current?.closest("form")?.requestSubmit();
            return;
        }

        if ((event.key === "Backspace" || event.key === "Delete") && editorRef.current) {
            const selection = window.getSelection();
            if (!selection || !selection.isCollapsed) return;
            const offset = caretOffset(editorRef.current);
            const deletion =
                event.key === "Backspace"
                    ? tokenDeletionBefore(value, refs, offset)
                    : tokenDeletionAfter(value, refs, offset);
            if (deletion) {
                event.preventDefault();
                updateValueAfterTokenDelete(deletion.start, deletion.end);
            }
        }
    };

    const handleKeyUp = (event: KeyboardEvent<HTMLDivElement>) => {
        if (["ArrowDown", "ArrowUp", "Enter", "Tab", "Escape"].includes(event.key)) return;
        updateActive();
    };

    return (
        <div className="relative min-w-0 flex-1">
            <div
                ref={editorRef}
                role="textbox"
                aria-autocomplete="list"
                aria-expanded={active && results.length > 0 ? true : undefined}
                aria-disabled={disabled || undefined}
                contentEditable={!disabled}
                suppressContentEditableWarning
                data-autocomplete={autoComplete}
                className={`${inputClassName ?? ""} min-h-[2.5rem] cursor-text whitespace-pre-wrap break-words leading-6 empty:before:text-transparent`}
                onInput={updateValueFromEditor}
                onKeyDown={handleKeyDown}
                onKeyUp={handleKeyUp}
                onClick={updateActive}
                onFocus={updateActive}
                onBlur={() => {
                    window.setTimeout(() => setActive(null), 120);
                }}
                onPaste={(event) => {
                    event.preventDefault();
                    const text = event.clipboardData.getData("text/plain");
                    document.execCommand("insertText", false, text);
                }}
            >
            </div>
            {!value && placeholder && (
                <span
                    className="pointer-events-none absolute inset-y-0 left-3 right-3 flex items-center truncate text-sm text-on-surface-variant/60">
          {placeholder}
        </span>
            )}
            {active && results.length > 0 && (
                <div
                    className={`absolute left-0 z-50 max-h-72 w-full min-w-[260px] overflow-y-auto rounded-lg border border-outline-variant/70 bg-background shadow-xl ${
                        direction === "above" ? "bottom-full mb-2" : "top-full mt-2"
                    }`}
                >
                    <div
                        className="border-b border-outline-variant/40 px-3 py-2 text-[10px] font-bold uppercase tracking-[0.08em] text-on-surface-variant">
                        {active.trigger === "@" ? "People" : "Sessions, Projects, Concepts"}
                    </div>
                    {results.map((result, index) => {
                        const ref = contextSearchResultToRef(result);
                        const key = contextRefKey(ref);
                        const selected = selectedKeys.has(key);
                        return (
                            <button
                                key={`${result.kind}:${result.id}:${result.slug}`}
                                ref={index === highlighted ? highlightedRef : undefined}
                                type="button"
                                onMouseEnter={() => setHighlighted(index)}
                                onMouseDown={(event) => {
                                    event.preventDefault();
                                    selectResult(result);
                                }}
                                className={`flex w-full items-start gap-3 px-3 py-2.5 text-left transition ${
                                    index === highlighted ? "bg-primary-fixed/50" : "hover:bg-surface-container-low"
                                }`}
                            >
                <span
                    className="mt-0.5 rounded border border-outline-variant/50 bg-surface-container-low px-1.5 py-0.5 text-[10px] font-semibold text-primary">
                  {result.kind === "person" ? "@" : "#"}
                </span>
                                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-semibold text-on-surface">
                    {result.label}
                  </span>
                  <span className="block truncate text-xs text-on-surface-variant">
                    {kindLabel(result.kind)} · {resultSubtitle(result)}
                  </span>
                </span>
                                {selected && (
                                    <span className="mt-1 text-[10px] font-semibold text-primary">Added</span>
                                )}
                            </button>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
