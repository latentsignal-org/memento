"use client";
import {Tag} from "lucide-react";
import {cleanCardText} from "./card-text";

interface NarrativeSection {
    content: string;
    source_message_ids: number[];
}

interface ConceptCardProps {
    data: {
        concept_id: number;
        slug: string;
        name: string;
        scope_description: string;
        status: string;
        seed_keywords?: string[];
        narrative?: Record<string, NarrativeSection>;
        message_count: number;
    };
}

export default function ConceptCard({data}: ConceptCardProps) {
    const {
        name,
        slug,
        scope_description,
        status,
        seed_keywords = [],
        narrative = {},
        message_count,
    } = data;

    return (
        <div className="space-y-6 animate-fade-in">
            {/* Header Info */}
            <div
                className="relative overflow-hidden rounded-xl border border-outline-variant/60 bg-gradient-to-br from-primary/5 to-primary-fixed/5 p-5 shadow-sm">
                <div className="flex items-start gap-4">
                    <div className="min-w-0 flex-1 space-y-1">
                        <div className="flex items-center flex-wrap gap-2">
                            <h2 className="text-xl font-bold tracking-tight text-on-surface">
                                {name}
                            </h2>
                            {status && (
                                <span
                                    className="inline-flex items-center rounded-full bg-primary/10 px-2.5 py-0.5 text-xs font-semibold text-primary capitalize">
                  {status}
                </span>
                            )}
                        </div>
                        <p className="text-xs text-on-surface-variant font-mono">
                            slug: {slug}
                        </p>
                    </div>
                </div>

                {scope_description && (
                    <p className="mt-4 text-xs leading-relaxed text-on-surface-variant/90 border-t border-outline-variant/20 pt-3">
                        <span className="font-bold text-on-surface">Scope:</span> {cleanCardText(scope_description)}
                    </p>
                )}

                {/* Stats Grid */}
                <div className="mt-5 grid grid-cols-1 gap-2.5 border-t border-outline-variant/30 pt-4">
                    <div className="rounded bg-surface-container-low p-2.5 text-center">
                        <div
                            className="text-[10px] font-semibold text-on-surface-variant uppercase tracking-wider">Associated
                            Messages
                        </div>
                        <div className="mt-1 text-lg font-bold text-on-surface">{message_count}</div>
                    </div>
                </div>
            </div>

            {/* Narrative Section */}
            {Object.keys(narrative).length > 0 && (
                <section className="space-y-4">
                    <h3 className="text-sm font-bold uppercase tracking-wider text-on-surface-variant">Narrative</h3>
                    <div className="space-y-4">
                        {narrative.scope_summary && narrative.scope_summary.content && (
                            <div
                                className="rounded-lg border border-outline-variant/40 bg-surface-container-lowest p-4 shadow-xs">
                                <h4 className="text-xs font-semibold text-primary mb-1">Scope Summary</h4>
                                <p className="text-sm leading-relaxed text-on-surface">{cleanCardText(narrative.scope_summary.content)}</p>
                            </div>
                        )}
                        {narrative.distilled_insights && narrative.distilled_insights.content && (
                            <div
                                className="rounded-lg border border-outline-variant/40 bg-surface-container-lowest p-4 shadow-xs">
                                <h4 className="text-xs font-semibold text-primary mb-1">Distilled Insights</h4>
                                <p className="text-sm leading-relaxed text-on-surface whitespace-pre-wrap">{cleanCardText(narrative.distilled_insights.content)}</p>
                            </div>
                        )}
                        {narrative.evolving_understanding && narrative.evolving_understanding.content && (
                            <div
                                className="rounded-lg border border-outline-variant/40 bg-surface-container-lowest p-4 shadow-xs">
                                <h4 className="text-xs font-semibold text-primary mb-1">Evolving Understanding</h4>
                                <p className="text-sm leading-relaxed text-on-surface whitespace-pre-wrap">{cleanCardText(narrative.evolving_understanding.content)}</p>
                            </div>
                        )}
                    </div>
                </section>
            )}

            {/* Seed Keywords */}
            {seed_keywords.length > 0 && (
                <section className="space-y-3">
                    <h3 className="flex items-center gap-1.5 text-sm font-bold uppercase tracking-wider text-on-surface-variant">
                        <Tag className="h-4 w-4"/>
                        Seed Keywords
                    </h3>
                    <div className="flex flex-wrap gap-2">
                        {seed_keywords.map((kw, i) => (
                            <span
                                key={i}
                                className="inline-flex items-center rounded border border-outline-variant bg-surface-container-low px-2.5 py-1 text-xs text-on-surface-variant"
                            >
                {kw}
              </span>
                        ))}
                    </div>
                </section>
            )}
        </div>
    );
}
