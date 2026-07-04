"use client";

import {useCallback, useEffect, useMemo, useState} from "react";
import Link from "next/link";
import {
    ArrowLeft,
    ArrowRightLeft,
    Check,
    ChevronDown,
    GitMerge,
    Mail,
    MoveDown,
    MoveLeft,
    MoveRight,
    MoveUp,
    RefreshCw,
    ShieldCheck,
    X,
} from "lucide-react";

type SortKey = "combined" | "name_similarity" | "token_overlap" | "signature";

type MergePerson = {
    id: number;
    name: string;
    email: string;
    message_count: number;
    last_seen?: string;
    aliases?: string[];
    locked_count: number;
};

type MergeCandidate = {
    id: string;
    confidence: number;
    recommended_keep_id: number;
    recommended_merge_id: number;
    sources?: string[];
    scores_pending?: boolean;
    status?: string;
    people: MergePerson[];
    evidence: {
        shared_neighbor_count?: number;
        name_similarity: number;
        token_overlap?: number;
        signature_score: number;
        temporal_score?: number;
        combined_score: number;
    };
};

type SuggestionsResponse = {
    suggestions?: MergeCandidate[];
    total?: number;
    limit?: number;
    offset?: number;
    error?: string;
};

const PAGE_SIZE = 50;

const sortOptions: Array<{ value: SortKey; label: string }> = [
    {value: "combined", label: "Match score"},
    {value: "name_similarity", label: "Similar spelling"},
    {value: "token_overlap", label: "Shared name words"},
    {value: "signature", label: "Mutual contacts"},
];

function initials(name: string) {
    return name
        .split(/\s+/)
        .filter(Boolean)
        .slice(0, 2)
        .map((part) => part[0]?.toUpperCase())
        .join("");
}

function formatDate(value?: string) {
    if (!value) return "unknown";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleDateString(undefined, {month: "short", day: "numeric", year: "numeric"});
}

function formatPercent(score?: number) {
    return `${Math.round((score ?? 0) * 100)}%`;
}

function hasGraphSignal(candidate: MergeCandidate) {
    return candidate.sources?.includes("graph") || candidate.evidence.signature_score > 0;
}

function hasSource(candidate: MergeCandidate, source: string) {
    return candidate.sources?.includes(source) ?? false;
}

function sourceLabel(source: string) {
    switch (source) {
        case "graph":
            return "Mutual contacts";
        case "jaro_winkler":
            return "Similar spelling";
        case "jaccard":
            return "Shared name words";
        case "exact_name":
            return "Same display name";
        case "forwarder_unwrap":
            return "Forwarded name";
        default:
            return source.replaceAll("_", " ");
    }
}

function strongestEvidenceLabel(candidate: MergeCandidate) {
    const signals = [
        {label: "mutual contacts", score: hasGraphSignal(candidate) ? candidate.evidence.signature_score : 0},
        {label: "similar spelling", score: candidate.evidence.name_similarity},
        {label: "shared name words", score: candidate.evidence.token_overlap ?? 0},
    ].filter((signal) => signal.score > 0);

    if (signals.length === 0) {
        if (hasSource(candidate, "exact_name")) return "same display name";
        if (hasSource(candidate, "forwarder_unwrap")) return "forwarded display-name cleanup";
        return "available name evidence";
    }

    signals.sort((a, b) => b.score - a.score);
    const strongest = signals[0];
    const tied = signals.filter((signal) => Math.abs(signal.score - strongest.score) < 0.01);
    if (tied.length > 1) {
        return tied.map((signal) => signal.label).join(" and ");
    }
    return strongest.label;
}

function topSignalLabel(candidate: MergeCandidate) {
    if (hasSource(candidate, "exact_name")) {
        return "Same display name";
    }
    if (hasSource(candidate, "forwarder_unwrap")) {
        return "Forwarded name";
    }
    return strongestEvidenceLabel(candidate);
}

function evidenceRows(candidate: MergeCandidate) {
    const rows: Array<{ label: string; value: string; title: string }> = [];
    const graphSignal = hasGraphSignal(candidate);

    if (graphSignal) {
        rows.push({
            label: "Match score",
            value: formatPercent(candidate.evidence.combined_score),
            title: `Combined score: ${candidate.evidence.combined_score.toFixed(3)}`,
        });
        rows.push({
            label: "Mutual contacts",
            value: formatPercent(candidate.evidence.signature_score),
            title: `Mutual contacts score: ${candidate.evidence.signature_score.toFixed(3)}`,
        });
    } else {
        rows.push({
            label: "Mutual contacts",
            value: "none found",
            title: "No supporting social-graph signal for this suggestion",
        });
    }

    rows.push({
        label: "Similar spelling",
        value: formatPercent(candidate.evidence.name_similarity),
        title: `Similar spelling score: ${candidate.evidence.name_similarity.toFixed(3)}`,
    });
    rows.push({
        label: "Shared name words",
        value: formatPercent(candidate.evidence.token_overlap),
        title: `Shared name words score: ${(candidate.evidence.token_overlap ?? 0).toFixed(3)}`,
    });

    if ((candidate.evidence.shared_neighbor_count ?? 0) > 0) {
        rows.push({
            label: "Shared neighbors",
            value: String(candidate.evidence.shared_neighbor_count),
            title: `${candidate.evidence.shared_neighbor_count} shared graph neighbors`,
        });
    }
    if ((candidate.evidence.temporal_score ?? 0) > 0) {
        rows.push({
            label: "Temporal overlap",
            value: formatPercent(candidate.evidence.temporal_score),
            title: `Temporal overlap score: ${(candidate.evidence.temporal_score ?? 0).toFixed(3)}`,
        });
    }

    for (const source of candidate.sources || []) {
        if (["graph", "jaro_winkler", "jaccard"].includes(source)) continue;
        rows.push({label: sourceLabel(source), value: "present", title: sourceLabel(source)});
    }
    return rows;
}

function selectedKeepPerson(candidate: MergeCandidate, overrideKeepId?: number) {
    return (
        candidate.people.find((person) => person.id === overrideKeepId) ??
        candidate.people.find((person) => person.id === candidate.recommended_keep_id) ??
        candidate.people[0]
    );
}

function selectedMergePerson(candidate: MergeCandidate, keepId: number) {
    return (
        candidate.people.find((person) => person.id !== keepId) ??
        candidate.people.find((person) => person.id === candidate.recommended_merge_id) ??
        candidate.people[0]
    );
}

function suggestionReason(candidate: MergeCandidate) {
    const strongest = topSignalLabel(candidate).toLowerCase();
    if (candidate.confidence > 0) {
        return `Suggested because ${strongest} is the strongest signal, with a ${candidate.confidence}% match score.`;
    }
    return `Suggested because ${strongest} is the strongest available signal.`;
}

function MergeProfileCard({person, isKeep}: { person: MergePerson; isKeep: boolean }) {
    return (
        <div
            className={`rounded-2xl border p-4 ${
                isKeep ? "border-primary/30 bg-white shadow-sm" : "border-outline-variant/50 bg-surface-container"
            }`}
        >
            <div className="flex items-start gap-3">
                <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl bg-primary-fixed text-ui-medium font-bold text-on-primary-fixed-variant">
                    {initials(person.name)}
                </div>
                <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                        <h3 className="min-w-0 truncate text-ui-medium font-bold text-on-surface">{person.name}</h3>
                    </div>
                    <div className="mt-2 min-h-6">
                        {isKeep && (
                            <span className="inline-flex items-center gap-1 rounded-full bg-primary-fixed px-2.5 py-1 text-[11px] font-bold text-on-primary-fixed-variant">
                                <ShieldCheck size={12}/>
                                Canonical Profile
                            </span>
                        )}
                    </div>
                    <p className="mt-1 flex items-center gap-1 truncate font-mono text-[11px] text-on-surface-variant">
                        <Mail size={12}/>
                        {person.email}
                    </p>
                    <p className="mt-2 text-[11px] text-on-surface-variant">
                        {person.message_count.toLocaleString()} messages · last seen {formatDate(person.last_seen)}
                    </p>
                </div>
            </div>
        </div>
    );
}

export default function PeopleMergeReviewPage() {
    const [candidates, setCandidates] = useState<MergeCandidate[]>([]);
    const [expanded, setExpanded] = useState<Set<string>>(new Set());
    const [sortBy, setSortBy] = useState<SortKey>("combined");
    const [totalCount, setTotalCount] = useState(0);
    const [isLoading, setIsLoading] = useState(true);
    const [isLoadingMore, setIsLoadingMore] = useState(false);
    const [decidingId, setDecidingId] = useState<string | null>(null);
    const [keepOverrides, setKeepOverrides] = useState<Record<string, number>>({});
    const [error, setError] = useState<string | null>(null);
    const [notice, setNotice] = useState<string | null>(null);

    const loadSuggestions = useCallback(async (sort: SortKey, offset = 0) => {
        const append = offset > 0;
        if (append) setIsLoadingMore(true);
        else setIsLoading(true);
        setError(null);
        try {
            const res = await fetch(`/api/people/merge-suggestions?limit=${PAGE_SIZE}&offset=${offset}&sort=${sort}`, {cache: "no-store"});
            const data = (await res.json()) as SuggestionsResponse;
            if (!res.ok) throw new Error(data.error || `load suggestions: ${res.status}`);
            const suggestions = data.suggestions || [];
            setTotalCount(data.total ?? suggestions.length);
            if (append) {
                setCandidates((prev) => [...prev, ...suggestions]);
            } else {
                setCandidates(suggestions);
                setExpanded(new Set());
                setKeepOverrides({});
            }
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            if (append) setIsLoadingMore(false);
            else setIsLoading(false);
        }
    }, []);

    useEffect(() => {
        void loadSuggestions(sortBy);
    }, [loadSuggestions, sortBy]);

    const pendingMessageCount = useMemo(
        () => candidates.reduce((sum, candidate) => sum + candidate.people.reduce((personSum, person) => personSum + person.message_count, 0), 0),
        [candidates],
    );
    const hasMoreSuggestions = candidates.length < totalCount;

    const toggleExpanded = (id: string) => {
        setExpanded((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });
    };

    const decide = async (candidate: MergeCandidate, decision: "accept" | "reject") => {
        setDecidingId(candidate.id);
        setError(null);
        setNotice(null);
        const keepPerson = selectedKeepPerson(candidate, keepOverrides[candidate.id]);
        const mergePerson = selectedMergePerson(candidate, keepPerson.id);
        const body =
            decision === "accept"
                ? {id: candidate.id, decision, keep_person_id: keepPerson.id, merge_person_id: mergePerson.id}
                : {id: candidate.id, decision};
        try {
            const res = await fetch("/api/people/merge-decision", {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify(body),
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || `merge decision: ${res.status}`);
            setCandidates((prev) => prev.filter((item) => item.id !== candidate.id));
            setTotalCount((prev) => Math.max(0, prev - 1));
            setExpanded((prev) => {
                const next = new Set(prev);
                next.delete(candidate.id);
                return next;
            });
            setKeepOverrides((prev) => {
                const next = {...prev};
                delete next[candidate.id];
                return next;
            });
            if (decision === "accept") {
                setNotice("Merged one duplicate person. Run refresh when you are done reviewing to rebuild derived views.");
            }
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setDecidingId(null);
        }
    };

    return (
        <main className="pt-16 min-h-screen bg-background text-on-surface">
            <div className="mx-auto w-full max-w-[1180px] px-6 py-10">
                <Link
                    href="/people"
                    className="mb-6 inline-flex items-center gap-2 text-ui-small font-bold text-on-surface-variant hover:text-primary"
                >
                    <ArrowLeft size={16}/>
                    Back to People
                </Link>
                <header>
                    <h1 className="text-display-lg font-display-lg text-primary tracking-tight">Merge People</h1>
                    <p className="mt-3 text-body-reading font-body-reading leading-relaxed text-on-surface-variant">
                        Review suggested duplicate people one pair at a time. Choose the canonical profile, then accept the merge or dismiss it so it does not return.
                    </p>
                </header>
            </div>

            <section className="mx-auto w-full max-w-[1180px] px-6 pb-12">
                <div className="mb-6 rounded-xl border border-outline-variant/50 bg-white px-4 py-3 shadow-sm">
                    <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                            <label className="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-2">
                                <span className="flex items-center gap-2 text-ui-small font-bold text-primary">
                                    <GitMerge size={16}/>
                                    Sort
                                </span>
                                <span className="relative block w-full sm:w-56">
                                    <select
                                        value={sortBy}
                                        onChange={(event) => setSortBy(event.target.value as SortKey)}
                                        className="w-full appearance-none rounded-lg border border-outline-variant bg-white px-3 py-2 pr-9 text-ui-small font-bold text-on-surface outline-none focus:border-primary"
                                    >
                                        {sortOptions.map((option) => (
                                            <option key={option.value} value={option.value}>{option.label}</option>
                                        ))}
                                    </select>
                                    <ChevronDown
                                        size={15}
                                        className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-on-surface-variant"
                                    />
                                </span>
                            </label>
                        </div>

                        <div className="flex flex-wrap gap-x-5 gap-y-1 text-ui-small text-on-surface-variant">
                            <span><strong className="text-primary">{totalCount.toLocaleString()}</strong> suggestions</span>
                            {totalCount > candidates.length && <span><strong className="text-primary">{candidates.length}</strong> loaded</span>}
                            <span><strong className="text-primary">{pendingMessageCount.toLocaleString()}</strong> loaded messages</span>
                        </div>
                    </div>
                    {notice && <p className="mt-3 text-ui-small font-bold text-primary">{notice}</p>}
                    {error && <p className="mt-3 text-ui-small font-bold text-red-700">{error}</p>}
                </div>

                <div className="space-y-5">
                    {isLoading && (
                        <div className="rounded-2xl border border-outline-variant/50 bg-surface-container-low p-10 text-center">
                            <RefreshCw className="mx-auto animate-spin text-primary"/>
                            <p className="mt-4 text-ui-medium text-on-surface-variant">Loading merge suggestions...</p>
                        </div>
                    )}

                    {!isLoading && candidates.length === 0 && (
                        <div className="rounded-2xl border border-outline-variant/50 bg-surface-container-low p-10 text-center">
                            <ShieldCheck className="mx-auto text-primary"/>
                            <h2 className="mt-4 text-headline-md font-headline-md font-bold text-primary">No duplicate people pending</h2>
                            <p className="mt-2 text-ui-medium text-on-surface-variant">Run refresh after archive changes to rebuild the review queue.</p>
                        </div>
                    )}

                    {candidates.map((candidate) => {
                        const isExpanded = expanded.has(candidate.id);
                        const keepPerson = selectedKeepPerson(candidate, keepOverrides[candidate.id]);
                        const firstPerson = candidate.people[0];
                        const secondPerson = candidate.people[1] ?? candidate.people[0];
                        const arrowPointsForward = keepPerson.id === secondPerson.id;
                        const rows = evidenceRows(candidate);

                        return (
                            <article key={candidate.id} className="rounded-2xl border border-outline-variant/50 bg-surface-container-low p-5 shadow-sm">
                                <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1fr)_220px]">
                                    <div>
                                        <h2 className="line-clamp-2 text-headline-md font-headline-md font-bold text-primary">
                                            {candidate.people.map((person) => person.name).join(" + ")}
                                        </h2>

                                        <div className="mt-5 grid grid-cols-1 items-stretch gap-3 md:grid-cols-[minmax(0,1fr)_72px_minmax(0,1fr)]">
                                            <MergeProfileCard person={firstPerson} isKeep={firstPerson.id === keepPerson.id}/>

                                            <div className="flex items-center justify-center">
                                                <button
                                                    type="button"
                                                    onClick={() =>
                                                        setKeepOverrides((prev) => ({
                                                            ...prev,
                                                            [candidate.id]: arrowPointsForward ? firstPerson.id : secondPerson.id,
                                                        }))
                                                    }
                                                    disabled={decidingId !== null || firstPerson.id === secondPerson.id}
                                                    className="group flex min-h-16 w-full flex-col items-center justify-center gap-2 rounded-xl border border-outline-variant/60 bg-white px-2 py-3 text-on-surface-variant hover:border-primary/45 hover:bg-surface-container-low hover:text-primary disabled:cursor-not-allowed disabled:opacity-40"
                                                    aria-label="Reverse merge direction"
                                                    title="Reverse merge direction"
                                                >
                                                    {arrowPointsForward ? (
                                                        <MoveRight size={24} className="hidden md:block"/>
                                                    ) : (
                                                        <MoveLeft size={24} className="hidden md:block"/>
                                                    )}
                                                    {arrowPointsForward ? (
                                                        <MoveDown size={24} className="md:hidden"/>
                                                    ) : (
                                                        <MoveUp size={24} className="md:hidden"/>
                                                    )}
                                                    <span className="flex h-7 w-7 items-center justify-center rounded-full border border-outline-variant bg-white text-primary group-hover:border-primary/35">
                                                        <ArrowRightLeft size={15}/>
                                                    </span>
                                                    <span className="text-[10px] font-bold uppercase tracking-wide">Swap</span>
                                                </button>
                                            </div>

                                            <MergeProfileCard person={secondPerson} isKeep={secondPerson.id === keepPerson.id}/>
                                        </div>

                                        <p className="mt-5 text-ui-small text-on-surface-variant">{suggestionReason(candidate)}</p>

                                        <button
                                            onClick={() => toggleExpanded(candidate.id)}
                                            className="mt-3 inline-flex items-center gap-2 text-ui-small font-bold text-primary hover:underline"
                                        >
                                            <ChevronDown size={16} className={isExpanded ? "rotate-180 transition" : "transition"}/>
                                            {isExpanded ? "Hide evidence" : "Show evidence"}
                                        </button>

                                        {isExpanded && (
                                            <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-3">
                                                {rows.map((row) => (
                                                    <div key={`${candidate.id}-${row.label}`} title={row.title} className="rounded-xl bg-white px-3 py-2">
                                                        <p className="text-[10px] font-bold uppercase tracking-wide text-on-surface-variant">{row.label}</p>
                                                        <p className="mt-1 text-ui-small font-bold text-primary">{row.value}</p>
                                                    </div>
                                                ))}
                                            </div>
                                        )}
                                    </div>

                                    <aside className="flex flex-col justify-between rounded-2xl bg-white p-4">
                                        <div>
                                            <p className="text-[11px] font-bold uppercase tracking-[0.16em] text-on-surface-variant">Decision</p>
                                            <div className="mt-4 space-y-2">
                                                <button
                                                    onClick={() => void decide(candidate, "accept")}
                                                    disabled={decidingId !== null}
                                                    className="grid w-full grid-cols-[18px_1fr] items-center gap-2 rounded-lg bg-primary px-3 py-2.5 text-ui-small font-bold text-primary-foreground hover:opacity-95 disabled:cursor-not-allowed disabled:opacity-40"
                                                >
                                                    {decidingId === candidate.id ? <RefreshCw size={16} className="animate-spin"/> : <Check size={16}/>}
                                                    <span className="text-left">Accept merge</span>
                                                </button>
                                                <button
                                                    onClick={() => void decide(candidate, "reject")}
                                                    disabled={decidingId !== null}
                                                    className="grid w-full grid-cols-[18px_1fr] items-center gap-2 rounded-lg border border-outline-variant bg-white px-3 py-2.5 text-ui-small font-bold text-on-surface-variant hover:bg-surface-container disabled:cursor-not-allowed disabled:opacity-40"
                                                >
                                                    <X size={16}/>
                                                    <span className="text-left">Not the same person</span>
                                                </button>
                                            </div>
                                        </div>

                                        <p className="mt-5 text-ui-small text-on-surface-variant">
                                            Notes, facets, aliases, and project memberships move into <strong>{keepPerson.name}</strong>.
                                        </p>
                                    </aside>
                                </div>
                            </article>
                        );
                    })}

                    {!isLoading && hasMoreSuggestions && (
                        <button
                            onClick={() => void loadSuggestions(sortBy, candidates.length)}
                            disabled={isLoadingMore}
                            className="flex w-full items-center justify-center gap-2 rounded-lg border border-outline-variant bg-white px-4 py-3 text-ui-small font-bold text-on-surface-variant hover:bg-surface-container disabled:opacity-40"
                        >
                            <RefreshCw size={15} className={isLoadingMore ? "animate-spin" : ""}/>
                            Load more suggestions
                        </button>
                    )}
                </div>
            </section>
        </main>
    );
}
