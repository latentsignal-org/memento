export interface AgentLogsResponse<L = unknown, T = unknown> {
    loops: L[];
    tool_calls: T[];
    ask_session?: unknown;
}

export function normalizeLogsResponse<L = unknown, T = unknown>(
    data: unknown,
): AgentLogsResponse<L, T> {
    if (Array.isArray(data)) {
        return {loops: data as L[], tool_calls: []};
    }
    if (data && typeof data === "object") {
        const obj = data as { loops?: unknown; tool_calls?: unknown; ask_session?: unknown };
        return {
            loops: Array.isArray(obj.loops) ? (obj.loops as L[]) : [],
            tool_calls: Array.isArray(obj.tool_calls) ? (obj.tool_calls as T[]) : [],
            ask_session: obj.ask_session,
        };
    }
    return {loops: [], tool_calls: []};
}
