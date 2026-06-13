"use client";

import Link from "next/link";
import {type ReactNode, useCallback, useEffect, useState} from "react";
import {Button, buttonVariants} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {CheckList} from "./CheckList";
import {SetupShell} from "./SetupShell";
import type {InitProgress, SetupArchiveCheckResult, SetupStatus, SetupSummary} from "./types";

const buildSteps = [
    ["preflight", "Preflight checks"],
    ["migrate_schema", "Migrate schema"],
    ["save_config", "Save owner and model config"],
    ["resolve_persons", "Resolve persons"],
    ["classify_people", "Classify people"],
    ["detect_newsletters", "Detect newsletters"],
    ["reclassify_people", "Re-classify newsletter senders"],
    ["refresh_people", "Refresh People report"],
    ["refresh_newsletters", "Refresh Newsletters report"],
    ["refresh_projects", "Refresh Projects report"],
    ["refresh_concepts", "Refresh Concepts report"],
    ["build_social_graph", "Build social graph"],
    ["scan_duplicates", "Scan duplicates"],
    ["postflight", "Postflight checks"],
] as const;

async function readJSON<T>(response: Response): Promise<T> {
    const body = (await response.json()) as T & { error?: string };
    if (!response.ok) throw new Error(body.error || `HTTP ${response.status}`);
    return body;
}

export function SetupWizard() {
    const [step, setStep] = useState(1);
    const [maxStep, setMaxStep] = useState(1);
    const [status, setStatus] = useState<SetupStatus | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [archivePath, setArchivePath] = useState("");
    const [archiveCheck, setArchiveCheck] = useState<SetupArchiveCheckResult | null>(null);
    const [ownerName, setOwnerName] = useState("");
    const [ownerEmail, setOwnerEmail] = useState("");
    const [provider, setProvider] = useState("gemini");
    const [model, setModel] = useState("gemini-3.5-flash");
    const [baseURL, setBaseURL] = useState("");
    const [apiKey, setAPIKey] = useState("");
    const [restartRequired, setRestartRequired] = useState(false);
    const [testResult, setTestResult] = useState<"" | "ok" | "fail">("");
    const [progress, setProgress] = useState<InitProgress[]>([]);
    const [summary, setSummary] = useState<SetupSummary | null>(null);

    const goToStep = useCallback((nextStep: number) => {
        setStep(nextStep);
        setMaxStep((current) => Math.max(current, nextStep));
    }, []);

    const selectRailStep = useCallback((nextStep: number) => {
        if (nextStep <= maxStep) setStep(nextStep);
    }, [maxStep]);

    const loadStatus = async () => {
        setLoading(true);
        setError("");
        try {
            const next = await readJSON<SetupStatus>(await fetch("/api/setup/status", {cache: "no-store"}));
            setStatus(next);
            setArchivePath((current) => current || next.msgvault.path);
            setOwnerName((current) => current || next.ownerName || "");
            setOwnerEmail((current) => current || next.ownerEmail || next.inferredOwnerEmail);
            setProvider(next.provider.name || "gemini");
            setModel(next.provider.model || "gemini-3.5-flash");
            setBaseURL(next.provider.baseUrl || "");
            if (next.initialized) goToStep(6);
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : String(cause));
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        let cancelled = false;
        fetch("/api/setup/status", {cache: "no-store"})
            .then((response) => readJSON<SetupStatus>(response))
            .then((next) => {
                if (cancelled) return;
                setStatus(next);
                setArchivePath(next.msgvault.path);
                setOwnerName(next.ownerName || "");
                setOwnerEmail(next.ownerEmail || next.inferredOwnerEmail);
                setProvider(next.provider.name || "gemini");
                setModel(next.provider.model || "gemini-3.5-flash");
                setBaseURL(next.provider.baseUrl || "");
                if (next.initialized) goToStep(6);
            })
            .catch((cause) => {
                if (!cancelled) setError(cause instanceof Error ? cause.message : String(cause));
            });
        return () => {
            cancelled = true;
        };
    }, [goToStep]);

    const saveEnv = async (body: Record<string, unknown>) => {
        return readJSON<{ ok: boolean; restartRequired: boolean }>(
            await fetch("/api/setup/env", {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify(body),
            }),
        );
    };

    const checkArchive = async () => {
        if (!archivePath.trim()) {
            setError("Enter a msgvault database path.");
            return null;
        }
        setLoading(true);
        setError("");
        try {
            const result = await readJSON<SetupArchiveCheckResult>(
                await fetch("/api/setup/check-archive", {
                    method: "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify({msgvaultDb: archivePath.trim()}),
                }),
            );
            setArchiveCheck(result);
            if (!result.ok) setError("Fix the archive checks before continuing.");
            return result;
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : String(cause));
            return null;
        } finally {
            setLoading(false);
        }
    };

    const chooseArchive = async () => {
        if (!ownerName.trim() || !ownerEmail.includes("@")) {
            setError("Enter your name and a valid email address.");
            return;
        }
        setLoading(true);
        setError("");
        try {
            await saveEnv({
                ownerName: ownerName.trim(),
                ownerEmail: ownerEmail.trim(),
            });
            await loadStatus();
            goToStep(2);
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : String(cause));
        } finally {
            setLoading(false);
        }
    };

    const saveArchive = async () => {
        const checked = await checkArchive();
        if (!checked?.ok) return;
        setLoading(true);
        setError("");
        try {
            const changed = Boolean(status && archivePath.trim() !== status.msgvault.path);
            const result = await saveEnv({
                msgvaultDb: archivePath.trim(),
                confirmDbChange: changed,
            });
            setRestartRequired(result.restartRequired);
            if (!result.restartRequired) {
                await loadStatus();
                goToStep(4);
            }
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : String(cause));
        } finally {
            setLoading(false);
        }
    };

    const selectProvider = (value: string) => {
        setProvider(value);
        setTestResult("");
        if (value === "fake") setModel("fake");
        if (value === "gemini") setModel("gemini-3.5-flash");
        if (value === "openai_compatible") setModel("gemma-4-26b-a4b-it");
    };

    const saveProvider = async (continueToBuild = true) => {
        if (!model.trim() || (provider === "openai_compatible" && !baseURL.trim())) {
            setError("Enter a model and the required provider settings.");
            return false;
        }
        setLoading(true);
        setError("");
        try {
            await saveEnv({
                modelProvider: provider,
                model: model.trim(),
                modelBaseUrl: baseURL.trim(),
                ...(apiKey ? {modelApiKey: apiKey} : {}),
            });
            setAPIKey("");
            if (continueToBuild) goToStep(5);
            return true;
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : String(cause));
            return false;
        } finally {
            setLoading(false);
        }
    };

    const testProvider = async () => {
        if (!(await saveProvider(false))) return;
        setLoading(true);
        setTestResult("");
        try {
            const result = await readJSON<{ ok: boolean; error?: string }>(
                await fetch("/api/setup/test-provider", {method: "POST"}),
            );
            setTestResult(result.ok ? "ok" : "fail");
            if (!result.ok) setError(result.error || "Provider test failed.");
        } catch (cause) {
            setTestResult("fail");
            setError(cause instanceof Error ? cause.message : String(cause));
        } finally {
            setLoading(false);
        }
    };

    const startBuild = async () => {
        setLoading(true);
        setError("");
        setProgress([]);
        try {
            const result = await readJSON<{ jobId: string }>(
                await fetch("/api/setup/init", {
                    method: "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify({
                        ownerName: ownerName.trim(), ownerEmail: ownerEmail.trim(),
                        modelProvider: provider, model, modelBaseUrl: baseURL.trim(),
                        msgvaultDb: archivePath.trim(),
                    }),
                }),
            );
            const source = new EventSource(`/api/setup/jobs/${result.jobId}`);
            source.onmessage = (event) => {
                const item = JSON.parse(event.data) as InitProgress;
                setProgress((current) => [...current, item]);
                if (item.status === "succeeded") {
                    source.close();
                    setSummary(item.result ?? null);
                    goToStep(6);
                    setLoading(false);
                } else if (item.status === "failed") {
                    source.close();
                    setError(item.message || "Initialization failed.");
                    setLoading(false);
                }
            };
            source.onerror = async () => {
                source.close();
                try {
                    const snapshot = await readJSON<{ status: string; error?: string; result?: SetupSummary }>(
                        await fetch(`/api/setup/jobs/${result.jobId}/status`, {cache: "no-store"}),
                    );
                    if (snapshot.status === "succeeded") {
                        setSummary(snapshot.result ?? null);
                        goToStep(6);
                    } else if (snapshot.status === "failed") {
                        setError(snapshot.error || "Initialization failed.");
                    } else {
                        setError("The progress stream disconnected. Reload this page to check setup status.");
                    }
                } catch {
                    setError("The progress stream disconnected. Check that ./memento serve is still running.");
                }
                setLoading(false);
            };
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : String(cause));
            setLoading(false);
        }
    };

    const latestProgress = progress.at(-1);
    const completedStep = latestProgress?.done ?? 0;
    const percent = Math.round((completedStep / buildSteps.length) * 100);
    const hardFailures = status ? [...(status.toolVersions ?? []), ...status.checks].filter((check) => check.status === "fail").length : 0;
    const displayedArchiveChecks = archiveCheck?.checks ?? status?.archiveChecks ?? [];
    const displayedArchiveMessageCount = archiveCheck ? archiveCheck.messageCount : status?.msgvault.messageCount;

    return (
        <SetupShell current={step} maxStep={maxStep} onStepSelect={selectRailStep}>
            <div className="mx-auto max-w-[590px]">
                {error ? (
                    <div
                        className="mb-5 flex gap-2 rounded-lg border border-[#e6bcbc] bg-[#fbecec] px-3.5 py-3 text-sm text-destructive">
                        <span className="material-symbols-outlined text-[19px]">error</span><span>{error}</span>
                    </div>
                ) : null}

                {step === 1 ? (
                    <>
                        <h1 className="font-display-lg text-[30px] leading-tight text-on-surface">Turn your email into
                            memory.</h1>
                        <p className="mt-2 max-w-[54ch] text-[15px] text-on-surface-variant">Source-attributed People,
                            Projects, Newsletters, and Concepts from a local archive, all on your machine.</p>
                        <p className="mt-3 max-w-[54ch] text-[14px] text-on-surface-variant">Next, Memento will connect
                            msgvault, set an AI provider, and build your local memory surfaces.</p>
                        <section className="mt-7 rounded-lg border border-outline-variant bg-white p-4">
                            <h2 className="font-headline-md text-xl">About you</h2>
                            <p className="mt-1 text-sm text-on-surface-variant">Memento uses this to recognize messages
                                you sent, separate your voice from other people&apos;s, and label your local memory.</p>
                            <div className="mt-4 space-y-4">
                                <Field label="Your name"><Input value={ownerName}
                                                                onChange={(event) => setOwnerName(event.target.value)}
                                                                className="h-10 bg-white"
                                                                placeholder="Full name"/></Field>
                                <Field label="Your email"
                                       hint={status?.inferredOwnerEmail ? `Suggested from this archive: ${status.inferredOwnerEmail}` : undefined}><Input
                                    type="email" value={ownerEmail}
                                    onChange={(event) => setOwnerEmail(event.target.value)} className="h-10 bg-white"
                                    placeholder="name@example.com"/></Field>
                            </div>
                        </section>
                        <div className="mt-7 flex justify-end">
                            <Button size="lg" onClick={() => void chooseArchive()} disabled={loading}>Continue<span
                                className="material-symbols-outlined text-[18px]">arrow_forward</span></Button>
                        </div>
                    </>
                ) : null}

                {step === 2 ? (
                    <>
                        <h1 className="font-headline-md text-[25px] font-semibold">Checking your environment</h1>
                        <p className="mt-2 text-[15px] text-on-surface-variant">Confirm the local tools, project
                            checkout, and environment file before checking your email archive.</p>
                        {status ? (
                            <div className="mt-6 space-y-5">
                                <ToolVersions checks={status.toolVersions ?? []}/>
                                <CheckList checks={status.checks}/>
                            </div>
                        ) : <p className="mt-6 text-sm text-on-surface-variant">Loading checks...</p>}
                        <div className="mt-7 flex items-center justify-between gap-3">
                            <Button variant="ghost" onClick={() => void loadStatus()} disabled={loading}><span
                                className="material-symbols-outlined text-[18px]">refresh</span>Re-run</Button>
                            <Button size="lg" onClick={() => goToStep(3)} disabled={hardFailures > 0}>Continue<span
                                className="material-symbols-outlined text-[18px]">arrow_forward</span></Button>
                        </div>
                        {hardFailures > 0 ?
                            <p className="mt-3 text-right text-xs text-destructive">{hardFailures} failure{hardFailures === 1 ? "" : "s"} need
                                attention.</p> : null}
                    </>
                ) : null}

                {step === 3 ? (
                    <>
                        <h1 className="font-headline-md text-[25px] font-semibold">Email archive</h1>
                        <p className="mt-2 text-[15px] text-on-surface-variant">Memento reads your msgvault archive and
                            writes only its own <code className="font-mono text-xs">memento_*</code> tables.</p>
                        <div className="mt-6 space-y-5">
                            <h2 className="font-headline-md text-xl">Archive database</h2>
                            <Field label="Msgvault database path"
                                   hint={displayedArchiveMessageCount ? `${displayedArchiveMessageCount.toLocaleString()} messages found.` : "Archive not checked yet. Run msgvault quickstart, then sync your mail if this path is empty."}>
                                <div className="flex flex-col gap-2 sm:flex-row">
                                    <Input value={archivePath} onChange={(event) => {
                                        setArchivePath(event.target.value);
                                        setRestartRequired(false);
                                        setArchiveCheck(null);
                                    }} className="h-10 bg-white font-mono text-xs"/>
                                    <Button type="button" variant="outline" onClick={() => void checkArchive()}
                                            disabled={loading} className="shrink-0">Check archive</Button>
                                </div>
                            </Field>
                            {!status?.msgvault.exists ?
                                <a href="https://github.com/wesm/msgvault" target="_blank" rel="noreferrer"
                                   className="text-sm font-medium text-primary underline underline-offset-4">Open
                                    msgvault setup documentation</a> : null}
                            {displayedArchiveChecks.length ? <CheckList checks={displayedArchiveChecks}/> : null}
                            <EmailPreview emails={status?.archivePreview ?? []}/>
                        </div>
                        {restartRequired ? <div
                            className="mt-5 flex gap-2 rounded-lg border border-[#e6d5ad] bg-[#fbf4e6] p-3.5 text-sm text-[#7a5200]">
                            <span className="material-symbols-outlined text-[19px]">restart_alt</span><span>The archive default was saved. Restart <code
                            className="font-mono">./memento serve</code>, reload this page, and continue. The running backend intentionally keeps its current database handle.</span>
                        </div> : null}
                        <Actions back={() => setStep(2)} next={() => void saveArchive()}
                                 disabled={loading || restartRequired}/>
                    </>
                ) : null}

                {step === 4 ? (
                    <>
                        <h1 className="font-headline-md text-[25px] font-semibold">Choose an AI provider</h1>
                        <p className="mt-2 text-[15px] text-on-surface-variant">Providers generate cited narratives.
                            Configuration stays in your local <code className="font-mono text-xs">.env</code>.</p>
                        <div className="mt-6 grid grid-cols-3 gap-2">
                            {[['openai_compatible', 'OpenAI Compatible', 'Local or hosted'], ['gemini', 'Gemini', 'Google models'], ['fake', 'Fake / Demo', 'Simulation']].map(([value, label, detail]) =>
                                <button key={value} type="button" onClick={() => selectProvider(value)}
                                        className={`cursor-pointer rounded-md border px-2 py-3 text-center text-[13px] ${provider === value ? "border-primary bg-primary-fixed font-semibold text-primary" : "border-outline-variant bg-white"}`}>{label}<small
                                    className="mt-1 block text-[11px] font-normal text-on-surface-variant">{detail}</small>
                                </button>)}
                        </div>
                        <div className="mt-6 space-y-5">
                            <Field label="Model"><Input value={model} onChange={(event) => setModel(event.target.value)}
                                                        className="h-10 bg-white"/></Field>
                            {provider === "openai_compatible" ?
                                <Field label="Base URL" hint="Required for OpenAI-compatible providers."><Input
                                    value={baseURL} onChange={(event) => setBaseURL(event.target.value)}
                                    className="h-10 bg-white font-mono text-xs"
                                    placeholder="http://127.0.0.1:11434/v1"/></Field> : null}
                            {provider !== "fake" ? <Field label="API key"
                                                          hint={status?.provider.hasApiKey ? "Leave blank to keep the configured key, or enter a new key to replace it." : "Write-only. Saved locally and never returned to the browser."}>{status?.provider.hasApiKey ?
                                <div
                                    className="mb-2 inline-flex items-center gap-1.5 rounded-full border border-[#bcd3cb] bg-[#e7f0ec] px-2.5 py-1 text-xs font-medium text-primary">
                                    <span className="material-symbols-outlined text-[15px]">check</span>Configured:
                                    ••••••••</div> : null}<Input type="password" value={apiKey}
                                                                 onChange={(event) => setAPIKey(event.target.value)}
                                                                 className="h-10 bg-white"
                                                                 placeholder={status?.provider.hasApiKey ? "Enter a new key to replace it" : "Enter API key"}
                                                                 autoComplete="new-password"/></Field> : <div
                                className="flex gap-2 rounded-lg border border-[#bcd3cb] bg-[#e7f0ec] p-3.5 text-sm text-primary">
                                <span className="material-symbols-outlined text-[19px]">info</span><span>The fake provider needs no key or network access. Generation is simulated.</span>
                            </div>}
                        </div>
                        <div className="mt-5 flex items-center gap-3"><Button variant="outline"
                                                                              onClick={() => void testProvider()}
                                                                              disabled={loading}>Test
                            connection</Button>{testResult ? <span
                            className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-sm ${testResult === "ok" ? "bg-[#e7f0ec] text-primary" : "bg-[#fbecec] text-destructive"}`}><span
                            className="material-symbols-outlined text-[16px]">{testResult === "ok" ? "check" : "close"}</span>{testResult === "ok" ? "Connection succeeded." : "Connection failed."}</span> : null}
                        </div>
                        <Actions back={() => setStep(3)} next={() => void saveProvider()} disabled={loading}/>
                    </>
                ) : null}

                {step === 5 ? (
                    <>
                        <h1 className="font-headline-md text-[25px] font-semibold">Build your memory</h1>
                        <p className="mt-2 text-[15px] text-on-surface-variant">Resolve people, detect newsletters,
                            build each report, and create the social graph from your archive.</p>
                        <div className="mt-6 h-1.5 overflow-hidden rounded-full bg-surface-container-high"><span
                            className="block h-full rounded-full bg-primary transition-all"
                            style={{width: `${percent}%`}}/></div>
                        <div className="mt-4 overflow-hidden rounded-lg border border-outline-variant bg-white">
                            {buildSteps.map(([key, label], index) => {
                                const done = index + 1 < completedStep || latestProgress?.status === "succeeded";
                                const running = index + 1 === completedStep && latestProgress?.status !== "succeeded";
                                return <div key={key}
                                            className={`flex items-center gap-2.5 border-b border-outline-variant/40 px-3.5 py-2.5 text-[13.5px] last:border-0 ${running ? "bg-[#e7f0ec] font-medium" : done ? "text-on-surface" : "text-outline"}`}>
                                    <span
                                        className={`material-symbols-outlined text-[17px] ${done || running ? "text-primary" : "text-outline"}`}>{done ? "check_circle" : running ? "progress_activity" : "radio_button_unchecked"}</span><span>{label}</span>{running ?
                                    <span className="ml-auto text-xs text-on-surface-variant">Running</span> : null}
                                </div>;
                            })}
                        </div>
                        {progress.length === 0 ? <div className="mt-7 flex justify-between"><Button variant="outline"
                                                                                                    onClick={() => setStep(4)}>Back</Button><Button
                                size="lg" onClick={() => void startBuild()} disabled={loading}>Build memory<span
                                className="material-symbols-outlined text-[18px]">construction</span></Button></div> :
                            <p className="mt-4 text-sm text-on-surface-variant">{latestProgress?.detail || latestProgress?.message}</p>}
                    </>
                ) : null}

                {step === 6 ? (
                    <>
                        <div className="flex size-12 items-center justify-center rounded-full bg-primary text-white">
                            <span className="material-symbols-outlined text-[27px]">check</span></div>
                        <h1 className="mt-5 font-display-lg text-[30px] leading-tight">Your memory is ready.</h1>
                        <p className="mt-2 text-[15px] text-on-surface-variant">Memento is initialized. Explore Home or
                            ask a question grounded in your archive.</p>
                        {summary ? <div
                            className="mt-7 grid grid-cols-2 gap-2.5 sm:grid-cols-4">{[[summary.humans, "People"], [summary.newsletters, "Newsletters"], [summary.persons, "Resolved"], [summary.duplicates, "Merge candidates"]].map(([value, label]) =>
                            <div key={label}
                                 className="rounded-lg border border-outline-variant bg-white p-3.5 text-center">
                                <div className="font-headline-md text-[26px] font-semibold text-primary">{value}</div>
                                <div
                                    className="mt-1 text-label-caps font-label-caps text-on-surface-variant">{label}</div>
                            </div>)}</div> : null}
                        {summary?.warnings?.length ?
                            <div className="mt-6"><h2 className="mb-2 text-sm font-semibold">Postflight notes</h2>
                                <CheckList checks={summary.warnings}/></div> : null}
                        <div className="mt-7"><p className="text-label-caps font-label-caps text-on-surface-variant">Try
                            asking</p>
                            <div
                                className="mt-2 flex flex-wrap gap-2">{["Who have I worked with most?", "What projects are active?", "What themes keep recurring?"].map((question) =>
                                <span key={question}
                                      className="rounded-full border border-outline-variant bg-white px-3 py-2 text-[13px]">{question}</span>)}</div>
                        </div>
                        <Link href="/home" prefetch={false} className={buttonVariants({size: "lg", className: "mt-8"})}>Open
                            Home<span className="material-symbols-outlined text-[18px]">arrow_forward</span></Link>
                    </>
                ) : null}
            </div>
        </SetupShell>
    );
}

function Field({label, hint, children}: { label: string; hint?: string; children: ReactNode }) {
    return <label className="block"><span
        className="mb-1.5 block text-label-caps font-label-caps text-on-surface-variant">{label}</span>{children}{hint ?
        <span className="mt-1.5 block text-xs text-on-surface-variant">{hint}</span> : null}</label>;
}

const statusIcon = {ok: "check", warn: "priority_high", fail: "close"} as const;
const statusTone = {
    ok: "bg-[#e7f0ec] text-primary",
    warn: "bg-[#fbf4e6] text-[#7a5200]",
    fail: "bg-[#fbecec] text-destructive",
} as const;

function ToolVersions({checks}: { checks: SetupStatus["toolVersions"] }) {
    if (!checks?.length) return null;
    return (
        <section className="rounded-lg border border-outline-variant bg-white p-3.5">
            <h2 className="text-sm font-semibold text-on-surface">Tool versions</h2>
            <div className="mt-3 space-y-2">
                {checks.map((check) => (
                    <div key={check.name} className="flex items-start gap-2.5 text-sm">
            <span
                className={`mt-0.5 flex size-[20px] shrink-0 items-center justify-center rounded-full ${statusTone[check.status]}`}>
              <span className="material-symbols-outlined text-[14px]">{statusIcon[check.status]}</span>
            </span>
                        <div className="min-w-0">
                            <p className="font-medium text-on-surface">{check.name}</p>
                            <p className="break-words text-xs text-on-surface-variant">{check.detail}</p>
                            {check.status !== "ok" && check.hint ?
                                <p className="mt-1 text-xs text-[#7a5200]">{check.hint}</p> : null}
                        </div>
                    </div>
                ))}
            </div>
        </section>
    );
}

function EmailPreview({emails}: { emails: NonNullable<SetupStatus["archivePreview"]> }) {
    if (!emails.length) return null;
    const rows: ReactNode[] = [];
    let lastGroup = "";
    emails.forEach((email) => {
        if (lastGroup && email.group !== lastGroup) {
            rows.push(<div key={`${lastGroup}-${email.group}-gap`}
                           className="py-1 text-center text-lg tracking-[0.35em] text-outline">···</div>);
        }
        lastGroup = email.group;
        rows.push(
            <div key={email.id}
                 className="grid grid-cols-[88px_130px_1fr] gap-3 border-b border-outline-variant/40 py-2 text-[12.5px] last:border-0">
                <span className="text-on-surface-variant">{formatEmailDate(email.sentAt)}</span>
                <span className="truncate font-medium text-on-surface">{email.sender}</span>
                <span className="truncate text-on-surface">{email.subject}</span>
            </div>,
        );
    });
    return (
        <details className="group rounded-lg border border-outline-variant bg-white">
            <summary className="flex cursor-pointer list-none items-center justify-between gap-3 p-3.5">
        <span>
          <span className="block text-sm font-semibold text-on-surface">Email archive preview</span>
          <span className="mt-1 block text-xs text-on-surface-variant">Open to verify the latest, middle, and earliest messages from this archive.</span>
        </span>
                <span
                    className="material-symbols-outlined text-[20px] text-outline transition-transform group-open:rotate-180">expand_more</span>
            </summary>
            <div className="border-t border-outline-variant/50 px-3.5 pb-3.5 pt-2">
                <div className="overflow-hidden">{rows}</div>
            </div>
        </details>
    );
}

function formatEmailDate(value: string) {
    const normalized = value.includes("T") ? value : value.replace(" ", "T");
    const date = new Date(normalized);
    if (Number.isNaN(date.getTime())) return value.slice(0, 10);
    return new Intl.DateTimeFormat("en", {month: "short", day: "numeric", year: "numeric"}).format(date);
}

function Actions({back, next, disabled}: { back: () => void; next: () => void; disabled?: boolean }) {
    return <div className="mt-8 flex items-center justify-between gap-3"><Button variant="outline"
                                                                                 onClick={back}>Back</Button><Button
        size="lg" onClick={next} disabled={disabled}>Continue<span
        className="material-symbols-outlined text-[18px]">arrow_forward</span></Button></div>;
}
