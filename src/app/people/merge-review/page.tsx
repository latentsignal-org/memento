"use client";

import {useEffect, useMemo, useState} from "react";
import Link from "next/link";
import {ArrowLeft, Check, ChevronDown, GitMerge, Mail, RefreshCw, ShieldCheck, UserRound, X,} from "lucide-react";

type Decision = "pending" | "approved" | "rejected";

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
    people: MergePerson[];
    evidence: {
        shared_neighbor_count: number;
        name_similarity: number;
        signature_score: number;
        temporal_score: number;
        combined_score: number;
    };
};

type SuggestionsResponse = {
    suggestions?: MergeCandidate[];
    error?: string;
};

function scoreTone(score: number) {
    if (score >= 95) return "bg-primary text-primary-foreground";
    if (score >= 85) return "bg-primary-fixed text-on-primary-fixed-variant";
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

function temporalLabel(score: number) {
    if (score >= 0.8) return "strong overlap";
    if (score >= 0.45) return "partial overlap";
    if (score > 0) return "weak overlap";
    return "non-overlapping windows";
}

export default function PeopleMergeReviewPage() {
    const [candidates, setCandidates] = useState<MergeCandidate[]>([]);
    const [selected, setSelected] = useState<Set<string>>(new Set());
    const [decisions, setDecisions] = useState<Record<string, Decision>>({});
    const [keepByCandidate, setKeepByCandidate] = useState<Record<string, number>>({});
    const [expanded, setExpanded] = useState<Set<string>>(new Set());
    const [isLoading, setIsLoading] = useState(true);
    const [isCommitting, setIsCommitting] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [commitResult, setCommitResult] = useState<string | null>(null);

    useEffect(() => {
        let cancelled = false;
        (async () => {
            setIsLoading(true);
            setError(null);
            try {
                const res = await fetch("/api/people/merge-suggestions?limit=50", {cache: "no-store"});
                const data = (await res.json()) as SuggestionsResponse;
                if (!res.ok) throw new Error(data.error || `load suggestions: ${res.status}`);
                const suggestions = data.suggestions || [];
                if (cancelled) return;
                setCandidates(suggestions);
                setSelected(new Set(suggestions.filter((candidate) => candidate.confidence >= 85).map((candidate) => candidate.id)));
                setKeepByCandidate(Object.fromEntries(suggestions.map((candidate) => [candidate.id, candidate.recommended_keep_id])));
                setExpanded(new Set(suggestions.slice(0, 2).map((candidate) => candidate.id)));
                setDecisions({});
            } catch (err) {
                if (!cancelled) setError(err instanceof Error ? err.message : String(err));
            } finally {
                if (!cancelled) setIsLoading(false);
            }
        })();
        return () => {
            cancelled = true;
        };
    }, []);

    const pendingCount = candidates.filter((candidate) => !decisions[candidate.id]).length;
    const approvedCandidates = candidates.filter((candidate) => decisions[candidate.id] === "approved");
    const selectedPending = candidates.filter((candidate) => selected.has(candidate.id) && !decisions[candidate.id]);
    const selectedMessageCount = selectedPending.reduce(
        (sum, candidate) => sum + candidate.people.reduce((personSum, person) => personSum + person.message_count, 0),
        0,
    );

    const decisionCounts = useMemo(() => {
        return candidates.reduce(
            (acc, candidate) => {
                const decision = decisions[candidate.id] ?? "pending";
                acc[decision] += 1;
                return acc;
            },
            {pending: 0, approved: 0, rejected: 0} as Record<Decision, number>,
        );
    }, [candidates, decisions]);

    const toggleSelected = (id: string) => {
        setSelected((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });
    };

    const setDecision = (id: string, decision: Decision) => {
        setDecisions((prev) => ({...prev, [id]: decision}));
        setSelected((prev) => {
            const next = new Set(prev);
            next.delete(id);
            return next;
        });
        setCommitResult(null);
    };

    const bulkApprove = () => {
        setDecisions((prev) => {
            const next = {...prev};
            for (const candidate of selectedPending) {
                next[candidate.id] = "approved";
            }
            return next;
        });
        setSelected(new Set());
        setCommitResult(null);
    };

    const toggleExpanded = (id: string) => {
        setExpanded((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });
    };

    const commitApproved = async () => {
        const merges = approvedCandidates.map((candidate) => {
            const intoID = keepByCandidate[candidate.id] ?? candidate.recommended_keep_id;
            const fromPerson = candidate.people.find((person) => person.id !== intoID);
            return {
                from_id: fromPerson?.id ?? candidate.recommended_merge_id,
                into_id: intoID,
            };
        });
        if (merges.length === 0) return;

        setIsCommitting(true);
        setError(null);
        setCommitResult(null);
        try {
            const res = await fetch("/api/people/merge-apply", {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({merges, refresh: true}),
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || `merge apply: ${res.status}`);
            setCommitResult(`Merged ${data.merged} duplicate ${data.merged === 1 ? "person" : "people"} and refreshed People.`);
            setCandidates((prev) => prev.filter((candidate) => decisions[candidate.id] !== "approved"));
            setDecisions((prev) => {
                const next = {...prev};
                for (const candidate of approvedCandidates) delete next[candidate.id];
                return next;
            });
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setIsCommitting(false);
        }
    };

    return (
        <main className="pt-16 min-h-screen bg-background text-on-surface">
            <div className="mx-auto grid w-full max-w-[1440px] grid-cols-1 gap-8 px-6 py-12 lg:grid-cols-12">
                <div className="lg:col-span-8">
                    <Link
                        href="/people"
                        className="mb-6 inline-flex items-center gap-2 text-ui-small font-bold text-on-surface-variant hover:text-primary"
                    >
                        <ArrowLeft size={16}/>
                        Back to People
                    </Link>
                    <header className="space-y-4">
                        <h1 className="text-display-lg font-display-lg text-primary tracking-tight">
                            Merge People
                        </h1>
                        <p className="text-body-reading font-body-reading text-on-surface-variant max-w-[800px] leading-relaxed">
                            Stage duplicate-person suggestions, review the canonical profile, then commit approved
                            merges in
                            one batch. People refresh runs once after the batch, not after each approval.
                        </p>
                    </header>
                </div>

                <aside className="lg:col-span-4">
                    <div
                        className="rounded-3xl border border-primary/15 bg-primary p-6 text-primary-foreground shadow-xl">
                        <p className="text-[11px] font-bold uppercase tracking-[0.18em] opacity-75">Current batch</p>
                        <div className="mt-5 grid grid-cols-3 gap-3">
                            <div>
                                <p className="text-headline-md font-headline-md">{pendingCount}</p>
                                <p className="text-[11px] uppercase tracking-wide opacity-70">Pending</p>
                            </div>
                            <div>
                                <p className="text-headline-md font-headline-md">{decisionCounts.approved}</p>
                                <p className="text-[11px] uppercase tracking-wide opacity-70">Staged</p>
                            </div>
                            <div>
                                <p className="text-headline-md font-headline-md">{decisionCounts.rejected}</p>
                                <p className="text-[11px] uppercase tracking-wide opacity-70">Rejected</p>
                            </div>
                        </div>
                        <div className="mt-6 rounded-2xl bg-white/10 p-4">
                            <p className="text-ui-small font-bold">Stage, then commit</p>
                            <p className="mt-1 text-ui-small opacity-80">
                                Approve/reject only changes this review queue. Commit applies staged merges and
                                refreshes once.
                            </p>
                        </div>
                    </div>
                </aside>
            </div>

            <section className="mx-auto grid w-full max-w-[1440px] grid-cols-1 gap-8 px-6 py-8 lg:grid-cols-12">
                <aside className="lg:col-span-3">
                    <div className="sticky top-24 space-y-4">
                        <div
                            className="rounded-2xl border border-outline-variant/50 bg-surface-container-low p-5 shadow-sm">
                            <div className="flex items-center gap-2 text-primary">
                                <GitMerge size={18}/>
                                <h2 className="text-ui-medium font-bold">Stage merges</h2>
                            </div>
                            <p className="mt-3 text-ui-small text-on-surface-variant">
                                {selectedPending.length} pending suggestions selected,
                                covering {selectedMessageCount.toLocaleString()} messages.
                            </p>
                            <button
                                onClick={bulkApprove}
                                disabled={selectedPending.length === 0 || isCommitting}
                                className="mt-5 flex w-full items-center justify-center gap-2 rounded-xl bg-primary px-4 py-3 text-ui-small font-bold text-primary-foreground transition hover:opacity-95 disabled:cursor-not-allowed disabled:opacity-35"
                            >
                                <ShieldCheck size={16}/>
                                Stage selected
                            </button>
                            <button
                                onClick={() => setSelected(new Set())}
                                disabled={isCommitting}
                                className="mt-2 w-full rounded-xl border border-outline-variant px-4 py-3 text-ui-small font-bold text-on-surface-variant hover:bg-surface-container disabled:opacity-40"
                            >
                                Clear selection
                            </button>
                        </div>

                        <div className="rounded-2xl border border-primary/20 bg-white p-5 shadow-sm">
                            <div className="flex items-center gap-2 text-primary">
                                <RefreshCw size={18}/>
                                <h2 className="text-ui-medium font-bold">Commit staged</h2>
                            </div>
                            <p className="mt-3 text-ui-small text-on-surface-variant">
                                {approvedCandidates.length} staged
                                merge{approvedCandidates.length === 1 ? "" : "s"} will be applied, then People will
                                refresh once.
                            </p>
                            <button
                                onClick={commitApproved}
                                disabled={approvedCandidates.length === 0 || isCommitting}
                                className="mt-5 flex w-full items-center justify-center gap-2 rounded-xl bg-primary px-4 py-3 text-ui-small font-bold text-primary-foreground transition hover:opacity-95 disabled:cursor-not-allowed disabled:opacity-35"
                            >
                                {isCommitting ? <RefreshCw size={16} className="animate-spin"/> : <Check size={16}/>}
                                {isCommitting ? "Committing..." : "Commit & refresh once"}
                            </button>
                            {commitResult &&
                                <p className="mt-3 text-ui-small font-bold text-primary">{commitResult}</p>}
                            {error && <p className="mt-3 text-ui-small font-bold text-red-700">{error}</p>}
                        </div>

                        <div className="rounded-2xl border border-outline-variant/50 bg-white p-5">
                            <div className="flex items-center gap-2 text-primary">
                                <ShieldCheck size={18}/>
                                <h2 className="text-ui-medium font-bold">Deterministic loop</h2>
                            </div>
                            <ol className="mt-4 space-y-3 text-ui-small text-on-surface-variant">
                                <li className="flex gap-3">
                                    <span
                                        className="mt-0.5 h-5 w-5 shrink-0 rounded-full bg-primary text-center text-[11px] font-bold text-white">1</span>
                                    Scorer proposes likely duplicate pairs.
                                </li>
                                <li className="flex gap-3">
                                    <span
                                        className="mt-0.5 h-5 w-5 shrink-0 rounded-full bg-primary text-center text-[11px] font-bold text-white">2</span>
                                    User stages safe merges and rejects false positives.
                                </li>
                                <li className="flex gap-3">
                                    <span
                                        className="mt-0.5 h-5 w-5 shrink-0 rounded-full bg-primary text-center text-[11px] font-bold text-white">3</span>
                                    Backend applies staged merges in a batch.
                                </li>
                                <li className="flex gap-3">
                                    <span
                                        className="mt-0.5 h-5 w-5 shrink-0 rounded-full bg-primary text-center text-[11px] font-bold text-white">4</span>
                                    One refresh rebuilds People and the graph.
                                </li>
                            </ol>
                        </div>
                    </div>
                </aside>

                <div className="space-y-5 lg:col-span-9">
                    {isLoading && (
                        <div
                            className="rounded-3xl border border-outline-variant/50 bg-surface-container-low p-10 text-center">
                            <RefreshCw className="mx-auto animate-spin text-primary"/>
                            <p className="mt-4 text-ui-medium text-on-surface-variant">Loading merge suggestions...</p>
                        </div>
                    )}

                    {!isLoading && candidates.length === 0 && (
                        <div
                            className="rounded-3xl border border-outline-variant/50 bg-surface-container-low p-10 text-center">
                            <ShieldCheck className="mx-auto text-primary"/>
                            <h2 className="mt-4 text-headline-md font-headline-md font-bold text-primary">No duplicate
                                people found</h2>
                            <p className="mt-2 text-ui-medium text-on-surface-variant">
                                The deterministic scorer did not find reviewable merge candidates. Run `./memento
                                refresh` if the graph is stale.
                            </p>
                        </div>
                    )}

                    {candidates.map((candidate) => {
                        const decision = decisions[candidate.id] ?? "pending";
                        const isExpanded = expanded.has(candidate.id);
                        const keepId = keepByCandidate[candidate.id] ?? candidate.recommended_keep_id;
                        const keepPerson = candidate.people.find((person) => person.id === keepId) ?? candidate.people[0];

                        return (
                            <article
                                key={candidate.id}
                                className={`overflow-hidden rounded-3xl border bg-surface-container-low shadow-sm transition ${
                                    decision === "approved"
                                        ? "border-primary/30"
                                        : decision === "rejected"
                                            ? "border-outline-variant/40 opacity-65"
                                            : "border-outline-variant/50"
                                }`}
                            >
                                <div className="grid grid-cols-1 gap-0 lg:grid-cols-[1fr_280px]">
                                    <div className="p-5 md:p-6">
                                        <div
                                            className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                                            <div>
                                                <div className="flex flex-wrap items-center gap-2">
                          <span
                              className={`rounded-full px-3 py-1 text-[11px] font-bold uppercase tracking-wide ${scoreTone(candidate.confidence)}`}>
                            {candidate.confidence}% likely same person
                          </span>
                                                    {decision !== "pending" && (
                                                        <span
                                                            className="rounded-full bg-white px-3 py-1 text-[11px] font-bold uppercase tracking-wide text-on-surface-variant">
                              {decision === "approved" ? "staged" : decision}
                            </span>
                                                    )}
                                                </div>
                                                <h2 className="mt-3 text-headline-md font-headline-md font-bold text-primary">
                                                    {candidate.people.map((person) => person.name).join(" + ")}
                                                </h2>
                                                <p className="mt-1 text-ui-small text-on-surface-variant">
                                                    Canonical profile if
                                                    committed: <strong>{keepPerson.name}</strong> via {keepPerson.email}
                                                </p>
                                            </div>

                                            <label
                                                className="inline-flex items-center gap-2 rounded-xl border border-outline-variant bg-white px-3 py-2 text-ui-small font-bold text-on-surface-variant">
                                                <input
                                                    type="checkbox"
                                                    checked={selected.has(candidate.id)}
                                                    disabled={decision !== "pending" || isCommitting}
                                                    onChange={() => toggleSelected(candidate.id)}
                                                    className="h-4 w-4 accent-primary"
                                                />
                                                Select
                                            </label>
                                        </div>

                                        <div className="mt-5 grid grid-cols-1 gap-4 md:grid-cols-2">
                                            {candidate.people.map((person) => {
                                                const isKeep = person.id === keepId;
                                                return (
                                                    <div
                                                        key={person.id}
                                                        className={`rounded-2xl border p-4 ${
                                                            isKeep ? "border-primary/30 bg-white shadow-sm" : "border-outline-variant/50 bg-surface-container"
                                                        }`}
                                                    >
                                                        <div className="flex items-start gap-3">
                                                            <div
                                                                className="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl bg-primary-fixed text-ui-medium font-bold text-on-primary-fixed-variant">
                                                                {initials(person.name)}
                                                            </div>
                                                            <div className="min-w-0 flex-1">
                                                                <div className="flex items-center gap-2">
                                                                    <h3 className="truncate text-ui-medium font-bold text-on-surface">{person.name}</h3>
                                                                    {isKeep && <Check size={15}
                                                                                      className="shrink-0 text-primary"/>}
                                                                </div>
                                                                <p className="mt-1 flex items-center gap-1 truncate font-mono text-[11px] text-on-surface-variant">
                                                                    <Mail size={12}/>
                                                                    {person.email}
                                                                </p>
                                                                <p className="mt-2 text-[11px] text-on-surface-variant">
                                                                    {person.message_count.toLocaleString()} messages ·
                                                                    last seen {formatDate(person.last_seen)}
                                                                </p>
                                                                {person.aliases && person.aliases.length > 1 && (
                                                                    <p className="mt-1 text-[11px] text-on-surface-variant">{person.aliases.length} aliases
                                                                        linked</p>
                                                                )}
                                                            </div>
                                                        </div>
                                                        <button
                                                            onClick={() => setKeepByCandidate((prev) => ({
                                                                ...prev,
                                                                [candidate.id]: person.id
                                                            }))}
                                                            disabled={decision !== "pending" || isCommitting}
                                                            className={`mt-4 w-full rounded-xl px-3 py-2 text-ui-small font-bold transition disabled:cursor-not-allowed disabled:opacity-40 ${
                                                                isKeep
                                                                    ? "bg-primary text-primary-foreground"
                                                                    : "border border-outline-variant text-on-surface-variant hover:bg-white"
                                                            }`}
                                                        >
                                                            {isKeep ? "Keep as canonical" : "Make canonical"}
                                                        </button>
                                                    </div>
                                                );
                                            })}
                                        </div>

                                        <button
                                            onClick={() => toggleExpanded(candidate.id)}
                                            className="mt-5 inline-flex items-center gap-2 text-ui-small font-bold text-primary hover:underline"
                                        >
                                            <ChevronDown size={16}
                                                         className={isExpanded ? "rotate-180 transition" : "transition"}/>
                                            {isExpanded ? "Hide evidence" : "Show evidence"}
                                        </button>

                                        {isExpanded && (
                                            <div
                                                className="mt-5 grid grid-cols-1 gap-4 rounded-2xl border border-outline-variant/40 bg-white p-4 md:grid-cols-4">
                                                <div>
                                                    <p className="text-[11px] font-bold uppercase tracking-wide text-on-surface-variant">Signature</p>
                                                    <p className="mt-1 text-headline-sm font-headline-md text-primary">
                                                        {Math.round(candidate.evidence.signature_score * 100)}%
                                                    </p>
                                                </div>
                                                <div>
                                                    <p className="text-[11px] font-bold uppercase tracking-wide text-on-surface-variant">Name
                                                        match</p>
                                                    <p className="mt-1 text-headline-sm font-headline-md text-primary">
                                                        {Math.round(candidate.evidence.name_similarity * 100)}%
                                                    </p>
                                                </div>
                                                <div>
                                                    <p className="text-[11px] font-bold uppercase tracking-wide text-on-surface-variant">Shared
                                                        neighbors</p>
                                                    <p className="mt-1 text-headline-sm font-headline-md text-primary">
                                                        {candidate.evidence.shared_neighbor_count}
                                                    </p>
                                                </div>
                                                <div>
                                                    <p className="text-[11px] font-bold uppercase tracking-wide text-on-surface-variant">Temporal
                                                        pattern</p>
                                                    <p className="mt-1 text-ui-small text-on-surface-variant">{temporalLabel(candidate.evidence.temporal_score)}</p>
                                                </div>
                                            </div>
                                        )}
                                    </div>

                                    <div
                                        className="flex flex-col justify-between border-t border-outline-variant/40 bg-white p-5 lg:border-l lg:border-t-0">
                                        <div>
                                            <p className="text-[11px] font-bold uppercase tracking-[0.16em] text-on-surface-variant">Decision</p>
                                            <div className="mt-4 space-y-2">
                                                <button
                                                    onClick={() => setDecision(candidate.id, "approved")}
                                                    disabled={decision !== "pending" || isCommitting}
                                                    className="flex w-full items-center justify-center gap-2 rounded-xl bg-primary px-4 py-3 text-ui-small font-bold text-primary-foreground hover:opacity-95 disabled:cursor-not-allowed disabled:opacity-40"
                                                >
                                                    <Check size={16}/>
                                                    Stage merge
                                                </button>
                                                <button
                                                    onClick={() => setDecision(candidate.id, "rejected")}
                                                    disabled={decision !== "pending" || isCommitting}
                                                    className="flex w-full items-center justify-center gap-2 rounded-xl border border-outline-variant px-4 py-3 text-ui-small font-bold text-on-surface-variant hover:bg-surface-container disabled:cursor-not-allowed disabled:opacity-40"
                                                >
                                                    <X size={16}/>
                                                    Not same person
                                                </button>
                                            </div>
                                        </div>

                                        <div className="mt-6 rounded-2xl bg-surface-container-low p-4">
                                            <div className="flex items-center gap-2 text-primary">
                                                <UserRound size={16}/>
                                                <p className="text-ui-small font-bold">Merge preview</p>
                                            </div>
                                            <p className="mt-2 text-ui-small text-on-surface-variant">
                                                Keep <strong>{keepPerson.name}</strong>; move aliases, notes, facets,
                                                and project memberships from the duplicate profile into it.
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
