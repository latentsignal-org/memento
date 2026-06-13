export type AgentEvent =
    | { type: "text_delta"; text: string }
    | { type: "tool_call_start"; name: string; args: unknown }
    | { type: "tool_call_result"; name: string; result: unknown; duration_ms?: number }
    | { type: "done"; interaction_id: string }
    | { type: "error"; message: string }
    | {
    type: "context_loaded";
    refs: Array<{ kind?: string; label?: string; slug?: string; person_id?: number; session_id?: number }>;
    warnings?: string[];
}
    | {
    type: "proposed_backfill";
    decision_id: string;
    rationale: string;
    candidate_message_ids: number[];
    gap_kind: string;
    slug: string;
};
