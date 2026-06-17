"use client";
import {useState} from "react";
import {CheckCircle2, XCircle} from "lucide-react";
import {MessageReference} from "@/components/evidence/MessageReference";

interface BackfillCardProps {
    backfillUrl: string;
    decisionId: string;
    rationale: string;
    candidateMessageIds: number[];
    gapKind: string;
}

export default function BackfillCard({
                                         backfillUrl,
                                         decisionId,
                                         rationale,
                                         candidateMessageIds,
                                         gapKind,
                                     }: BackfillCardProps) {
    const [selected, setSelected] = useState<Set<number>>(
        new Set(candidateMessageIds),
    );
    const [decided, setDecided] = useState<"accept" | "skip" | null>(null);
    const [loading, setLoading] = useState(false);

    const toggle = (id: number) => {
        setSelected((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });
    };

    const decide = async (decision: "accept" | "skip") => {
        if (decided || loading) return;
        setLoading(true);
        try {
            await fetch(backfillUrl, {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({
                    decision,
                    decision_id: decisionId,
                    message_ids: decision === "accept" ? [...selected] : [],
                }),
            }).then(async (res) => {
                if (!res.ok) {
                    const text = await res.text();
                    throw new Error(text || `backfill decision failed: ${res.status}`);
                }
            });
            setDecided(decision);
        } catch (err) {
            console.error("backfill decision failed", err);
            alert(err instanceof Error ? err.message : String(err));
        } finally {
            setLoading(false);
        }
    };

    const label = gapKind === "thematic" ? "Thin theme cluster" : "Gap detected";

    return (
        <div className="rounded border border-primary/30 bg-primary-fixed/20 p-3 space-y-2">
            <div className="text-[11px] font-semibold text-on-surface uppercase tracking-wide">
                {label} · {gapKind}
            </div>
            <p className="text-xs text-on-surface leading-5">{rationale}</p>

            <div className="space-y-1 max-h-52 overflow-y-auto pr-1">
                {candidateMessageIds.map((id) => (
                    <label
                        key={id}
                        className="flex items-center gap-2 cursor-pointer group"
                    >
                        <input
                            type="checkbox"
                            checked={selected.has(id)}
                            onChange={() => toggle(id)}
                            disabled={!!decided || loading}
                            className="h-3.5 w-3.5 rounded border-outline-variant accent-primary"
                        />
                        <span className="flex-1 min-w-0">
              <MessageReference messageId={id} display="subject" preview="compact"/>
            </span>
                    </label>
                ))}
            </div>

            {decided ? (
                <div
                    className={`flex items-center gap-1.5 text-xs font-medium ${
                        decided === "accept" ? "text-green-700" : "text-on-surface-variant"
                    }`}
                >
                    {decided === "accept" ? (
                        <CheckCircle2 className="h-3.5 w-3.5"/>
                    ) : (
                        <XCircle className="h-3.5 w-3.5"/>
                    )}
                    {decided === "accept"
                        ? `${selected.size} message${selected.size === 1 ? "" : "s"} added - agent continuing`
                        : "Skipped - agent continuing"}
                </div>
            ) : (
                <div className="flex gap-2">
                    <button
                        className="px-3 py-1 rounded bg-primary text-white text-xs font-semibold hover:opacity-90 disabled:opacity-50"
                        disabled={loading || selected.size === 0}
                        onClick={() => decide("accept")}
                    >
                        Add {selected.size} selected
                    </button>
                    <button
                        className="px-3 py-1 rounded border border-outline-variant text-on-surface text-xs hover:bg-surface-container disabled:opacity-50"
                        disabled={loading}
                        onClick={() => decide("skip")}
                    >
                        Skip all
                    </button>
                </div>
            )}
        </div>
    );
}
