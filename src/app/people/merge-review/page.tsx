"use client";

import {useCallback, useEffect, useMemo, useState} from "react";
import Link from "next/link";
import {ArrowLeft, Check, ChevronDown, GitMerge, Mail, RefreshCw, ShieldCheck, UserRound, X} from "lucide-react";

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
    error?: string;
};

const sortOptions: Array<{ value: SortKey; label: string }> = [
    {value: "combined", label: "Match score"},
    {value: "name_similarity", label: "Similar spelling"},
    {value: "token_overlap", label: "Shared name words"},
    {value: "signature", label: "Mutual contacts"},
];

function scoreTone(score: number) {
    if (score >= 95) return "bg-primary text-primary-foreground";
    if (score >= 75) return "bg-primary-fixed text-on-primary-fixed-variant";
    return "bg-surface-container-high text-on-surface-variant";
}

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

export default function PeopleMergeReviewPage() {
    const [candidates, setCandidates] = useState<MergeCandidate[]>([]);
    const [expanded, setExpanded] = useState<Set<string>>(new Set());
    const [sortBy, setSortBy] = useState<SortKey>("combined");
    const [isLoading, setIsLoading] = useState(true);
    const [decidingId, setDecidingId] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [notice, setNotice] = useState<string | null>(null);

    const loadSuggestions = useCallback(async (sort: SortKey) => {
        setIsLoading(true);
        setError(null);
        try {
            const res = await fetch(`/api/people/merge-suggestions?limit=50&sort=${sort}`, {cache: "no-store"});
            const data = (await res.json()) as SuggestionsResponse;
            if (!res.ok) throw new Error(data.error || `load suggestions: ${res.status}`);
            const suggestions = data.suggestions || [];
            setCandidates(suggestions);
            setExpanded(new Set(suggestions.slice(0, 2).map((candidate) => candidate.id)));
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setIsLoading(false);
        }
    }, []);

    useEffect(() => {
        void loadSuggestions(sortBy);
    }, [loadSuggestions, sortBy]);

    const pendingMessageCount = useMemo(
        () => candidates.reduce((sum, candidate) => sum + candidate.people.reduce((personSum, person) => personSum + person.message_count, 0), 0),
        [candidates],
    );

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
        try {
            const res = await fetch("/api/people/merge-decision", {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({id: candidate.id, decision}),
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || `merge decision: ${res.status}`);
            setCandidates((prev) => prev.filter((item) => item.id !== candidate.id));
            setExpanded((prev) => {
                const next = new Set(prev);
                next.delete(candidate.id);
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
            <div className="mx-auto grid w-full max-w-[1440px] grid-cols-1 gap-6 px-6 py-10 lg:grid-cols-12">
                <div className="lg:col-span-8">
                    <Link
                        href="/people"
                        className="mb-6 inline-flex items-center gap-2 text-ui-small font-bold text-on-surface-variant hover:text-primary"
                    >
                        <ArrowLeft size={16}/>
                        Back to People
                    </Link>
                    <header>
                        <h1 className="text-display-lg font-display-lg text-primary tracking-tight">Merge People</h1>
                        <p className="mt-3 max-w-[780px] text-body-reading font-body-reading leading-relaxed text-on-surface-variant">
                            Review suggested duplicate people one at a time. Automatic resolution only links deterministic mailbox matches; these rows need human confirmation.
                        </p>
                    </header>
                </div>

                <aside className="lg:col-span-4">
                    <div className="rounded-2xl border border-primary/15 bg-primary p-6 text-primary-foreground shadow-xl">
                        <p className="text-[11px] font-bold uppercase tracking-[0.18em] opacity-75">Pending review</p>
                        <div className="mt-5 grid grid-cols-2 gap-4">
                            <div>
                                <p className="text-headline-md font-headline-md">{candidates.length}</p>
                                <p className="text-[11px] uppercase tracking-wide opacity-70">Suggestions</p>
                            </div>
                            <div>
                                <p className="text-headline-md font-headline-md">{pendingMessageCount.toLocaleString()}</p>
                                <p className="text-[11px] uppercase tracking-wide opacity-70">Messages</p>
                            </div>
                        </div>
                    </div>
                </aside>
            </div>

            <section className="mx-auto grid w-full max-w-[1440px] grid-cols-1 gap-6 px-6 pb-12 lg:grid-cols-12">
                <aside className="lg:col-span-3">
                    <div className="sticky top-24 space-y-4">
                        <div className="rounded-2xl border border-outline-variant/50 bg-surface-container-low p-5 shadow-sm">
                            <div className="flex items-center gap-2 text-primary">
                                <GitMerge size={18}/>
                                <h2 className="text-ui-medium font-bold">Sort review queue</h2>
                            </div>
                            <label className="mt-4 block">
                                <span className="mb-1.5 block text-[11px] font-bold uppercase tracking-wide text-on-surface-variant">Scheme</span>
                                <select
                                    value={sortBy}
                                    onChange={(event) => setSortBy(event.target.value as SortKey)}
                                    className="w-full rounded-lg border border-outline-variant bg-white px-3 py-2 text-ui-small font-bold text-on-surface outline-none focus:border-primary"
                                >
                                    {sortOptions.map((option) => (
                                        <option key={option.value} value={option.value}>{option.label}</option>
                                    ))}
                                </select>
                            </label>
                            <button
                                onClick={() => void loadSuggestions(sortBy)}
                                disabled={isLoading}
                                className="mt-4 flex w-full items-center justify-center gap-2 rounded-lg border border-outline-variant px-3 py-2 text-ui-small font-bold text-on-surface-variant hover:bg-white disabled:opacity-40"
                            >
                                <RefreshCw size={15} className={isLoading ? "animate-spin" : ""}/>
                                Refresh
                            </button>
                            {notice && <p className="mt-4 text-ui-small font-bold text-primary">{notice}</p>}
                            {error && <p className="mt-4 text-ui-small font-bold text-red-700">{error}</p>}
                        </div>

                        <div className="rounded-2xl border border-outline-variant/50 bg-white p-5">
                            <div className="flex items-center gap-2 text-primary">
                                <ShieldCheck size={18}/>
                                <h2 className="text-ui-medium font-bold">Review policy</h2>
                            </div>
                            <p className="mt-3 text-ui-small leading-relaxed text-on-surface-variant">
                                Accept merges the pair immediately. Not the same person removes the suggestion and it will not return on reruns.
                            </p>
                        </div>
                    </div>
                </aside>

                <div className="space-y-5 lg:col-span-9">
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
                        const keepPerson = candidate.people.find((person) => person.id === candidate.recommended_keep_id) ?? candidate.people[0];
                        const mergePerson = candidate.people.find((person) => person.id === candidate.recommended_merge_id) ?? candidate.people[1] ?? candidate.people[0];
                        const graphSignal = hasGraphSignal(candidate);
                        const rows = evidenceRows(candidate);

                        return (
                            <article key={candidate.id} className="overflow-hidden rounded-2xl border border-outline-variant/50 bg-surface-container-low shadow-sm">
                                <div className="grid grid-cols-1 gap-0 lg:grid-cols-[1fr_250px]">
                                    <div className="p-5 md:p-6">
                                        <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                                            <div className="min-w-0">
                                                <div className="flex flex-wrap items-center gap-2">
                                                    <span className="rounded-full bg-primary px-3 py-1 text-[11px] font-bold uppercase tracking-wide text-primary-foreground">
                                                        {graphSignal ? "Social-graph signal" : "Name signal only"}
                                                    </span>
                                                    {graphSignal && (
                                                        <span className={`rounded-full px-3 py-1 text-[11px] font-bold uppercase tracking-wide ${scoreTone(candidate.confidence)}`}>
                                                            Match score {candidate.confidence}%
                                                        </span>
                                                    )}
                                                    <span className="rounded-full bg-white px-3 py-1 text-[11px] font-bold uppercase tracking-wide text-on-surface-variant">
                                                        Top signal: {topSignalLabel(candidate)}
                                                    </span>
                                                </div>
                                                <h2 className="mt-3 text-headline-md font-headline-md font-bold text-primary">
                                                    {candidate.people.map((person) => person.name).join(" + ")}
                                                </h2>
                                                <p className="mt-1 text-ui-small text-on-surface-variant">
                                                    If accepted: merge <strong>{mergePerson.name}</strong> into <strong>{keepPerson.name}</strong>.
                                                </p>
                                            </div>
                                        </div>

                                        <div className="mt-5 grid grid-cols-1 gap-4 md:grid-cols-2">
                                            {candidate.people.map((person) => {
                                                const isKeep = person.id === candidate.recommended_keep_id;
                                                return (
                                                    <div
                                                        key={person.id}
                                                        className={`rounded-2xl border p-4 ${
                                                            isKeep ? "border-primary/30 bg-white shadow-sm" : "border-outline-variant/50 bg-surface-container"
                                                        }`}
                                                    >
                                                        <div className="flex items-start gap-3">
                                                            <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl bg-primary-fixed text-ui-medium font-bold text-on-primary-fixed-variant">
                                                                {initials(person.name)}
                                                            </div>
                                                            <div className="min-w-0 flex-1">
                                                                <div className="flex items-center gap-2">
                                                                    <h3 className="truncate text-ui-medium font-bold text-on-surface">{person.name}</h3>
                                                                    {isKeep && <Check size={15} className="shrink-0 text-primary"/>}
                                                                </div>
                                                                <p className="mt-1 flex items-center gap-1 truncate font-mono text-[11px] text-on-surface-variant">
                                                                    <Mail size={12}/>
                                                                    {person.email}
                                                                </p>
                                                                <p className="mt-2 text-[11px] text-on-surface-variant">
                                                                    {person.message_count.toLocaleString()} messages · last seen {formatDate(person.last_seen)}
                                                                </p>
                                                                {person.aliases && person.aliases.length > 1 && (
                                                                    <p className="mt-1 text-[11px] text-on-surface-variant">{person.aliases.length} aliases linked</p>
                                                                )}
                                                            </div>
                                                        </div>
                                                    </div>
                                                );
                                            })}
                                        </div>

                                        <button
                                            onClick={() => toggleExpanded(candidate.id)}
                                            className="mt-5 inline-flex items-center gap-2 text-ui-small font-bold text-primary hover:underline"
                                        >
                                            <ChevronDown size={16} className={isExpanded ? "rotate-180 transition" : "transition"}/>
                                            {isExpanded ? "Hide evidence" : "Show evidence"}
                                        </button>

                                        {isExpanded && (
                                            <div className="mt-5 grid grid-cols-1 gap-4 rounded-2xl border border-outline-variant/40 bg-white p-4 md:grid-cols-4">
                                                {rows.map((row) => (
                                                    <div key={`${candidate.id}-${row.label}`} title={row.title}>
                                                        <p className="text-[11px] font-bold uppercase tracking-wide text-on-surface-variant">{row.label}</p>
                                                        <p className="mt-1 text-headline-sm font-headline-md text-primary">{row.value}</p>
                                                    </div>
                                                ))}
                                            </div>
                                        )}
                                    </div>

                                    <div className="flex flex-col justify-between border-t border-outline-variant/40 bg-white p-5 lg:border-l lg:border-t-0">
                                        <div>
                                            <p className="text-[11px] font-bold uppercase tracking-[0.16em] text-on-surface-variant">Decision</p>
                                            <div className="mt-4 space-y-2">
                                                <button
                                                    onClick={() => void decide(candidate, "accept")}
                                                    disabled={decidingId !== null}
                                                    className="flex w-full items-center justify-center gap-2 rounded-lg bg-primary px-4 py-3 text-ui-small font-bold text-primary-foreground hover:opacity-95 disabled:cursor-not-allowed disabled:opacity-40"
                                                >
                                                    {decidingId === candidate.id ? <RefreshCw size={16} className="animate-spin"/> : <Check size={16}/>}
                                                    Accept merge
                                                </button>
                                                <button
                                                    onClick={() => void decide(candidate, "reject")}
                                                    disabled={decidingId !== null}
                                                    className="flex w-full items-center justify-center gap-2 rounded-lg border border-outline-variant px-4 py-3 text-ui-small font-bold text-on-surface-variant hover:bg-surface-container disabled:cursor-not-allowed disabled:opacity-40"
                                                >
                                                    <X size={16}/>
                                                    Not the same person
                                                </button>
                                            </div>
                                        </div>

                                        <div className="mt-6 rounded-2xl bg-surface-container-low p-4">
                                            <div className="flex items-center gap-2 text-primary">
                                                <UserRound size={16}/>
                                                <p className="text-ui-small font-bold">Merge preview</p>
                                            </div>
                                            <p className="mt-2 text-ui-small text-on-surface-variant">
                                                Notes, facets, aliases, and project memberships move into <strong>{keepPerson.name}</strong>.
                                            </p>
                                        </div>
                                    </div>
                                </div>
                            </article>
                        );
                    })}
                </div>
            </section>
        </main>
    );
}
