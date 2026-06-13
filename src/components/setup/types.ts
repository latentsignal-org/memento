export type CheckStatus = "ok" | "warn" | "fail";

export interface SetupCheck {
    name: string;
    status: CheckStatus;
    detail: string;
    hint: string;
}

export interface SetupStatus {
    initialized: boolean;
    msgvault: { path: string; exists: boolean; messageCount: number };
    ownerConfigured: boolean;
    ownerName?: string;
    ownerEmail?: string;
    inferredOwnerEmail: string;
    provider: { name: string; hasApiKey: boolean; model: string; baseUrl: string };
    checks: SetupCheck[];
    toolVersions?: SetupCheck[];
    archiveChecks?: SetupCheck[];
    archivePreview?: SetupEmailPreview[];
    developerChecks?: SetupCheck[];
    postflightChecks?: SetupCheck[];
}

export interface SetupArchiveCheckResult {
    ok: boolean;
    checks: SetupCheck[];
    messageCount: number;
}

export interface SetupEmailPreview {
    id: number;
    sentAt: string;
    sender: string;
    subject: string;
    snippet: string;
    group: "latest" | "middle" | "earliest";
}

export interface SetupSummary {
    persons: number;
    humans: number;
    excluded: number;
    newsletters: number;
    duplicates: number;
    warnings?: SetupCheck[];
}

export interface InitProgress {
    timestamp: string;
    message: string;
    status: "pending" | "running" | "succeeded" | "failed";
    step?: string;
    done?: number;
    total?: number;
    detail?: string;
    result?: SetupSummary;
}
