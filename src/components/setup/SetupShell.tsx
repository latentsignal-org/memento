import type {ReactNode} from "react";
import Image from "next/image";

const steps = [
    ["Welcome", "About you"],
    ["Preflight", "Environment checks"],
    ["Email archive", "Archive details"],
    ["AI provider", "Generation settings"],
    ["Build memory", "Create local reports"],
    ["Done", "Ready to explore"],
] as const;

export function SetupShell({
                               current,
                               maxStep = current,
                               onStepSelect,
                               children,
                           }: {
    current: number;
    maxStep?: number;
    onStepSelect?: (step: number) => void;
    children: ReactNode;
}) {
    return (
        <main className="min-h-screen bg-background px-4 py-6 md:px-8 md:py-10">
            <section
                className="mx-auto max-w-[1040px] overflow-hidden rounded-xl border border-outline-variant bg-background shadow-sm">
                <header className="flex h-14 items-center justify-between border-b border-outline-variant px-5 md:px-7">
          <span className="flex items-center gap-2.5 font-headline-md text-xl font-semibold text-primary">
            <Image
                src="/memento-logo.png"
                alt=""
                width={28}
                height={28}
                className="size-7 rounded-md"
                priority
            />
            Memento
          </span>
                    <span
                        className="text-label-caps font-label-caps text-on-surface-variant">First-run onboarding</span>
                </header>
                <div className="flex min-h-[620px] flex-col md:flex-row">
                    <aside
                        className="border-b border-outline-variant bg-surface-container-low px-4 py-4 md:w-[230px] md:shrink-0 md:border-b-0 md:border-r md:px-4 md:py-7">
                        <p className="mb-3 px-2 text-label-caps font-label-caps text-on-surface-variant">Onboarding</p>
                        <ol className="grid grid-cols-2 gap-1 sm:grid-cols-3 md:block">
                            {steps.map(([label, detail], index) => {
                                const number = index + 1;
                                const done = number < current;
                                const active = number === current;
                                const clickable = Boolean(onStepSelect && number <= maxStep && number !== current);
                                const content = (
                                    <>
                    <span
                        className={`flex size-[22px] shrink-0 items-center justify-center rounded-full border text-xs font-semibold ${done ? "border-primary bg-primary text-white" : active ? "border-primary bg-primary-fixed text-primary" : "border-outline-variant bg-white text-on-surface-variant"}`}>
                      {done ? <span className="material-symbols-outlined text-[14px]">check</span> : number}
                    </span>
                                        <span
                                            className={`min-w-0 text-[13px] font-medium ${number > current ? "text-outline" : "text-on-surface"}`}>
                      {label}
                                            {active ? <small
                                                className="mt-0.5 hidden text-[11px] font-normal text-on-surface-variant md:block">{detail}</small> : null}
                    </span>
                                    </>
                                );
                                return (
                                    <li
                                        key={label}
                                        className={`rounded-md ${active ? "border border-outline-variant bg-white" : ""}`}
                                        aria-current={active ? "step" : undefined}
                                    >
                                        {clickable ? (
                                            <button
                                                type="button"
                                                onClick={() => onStepSelect?.(number)}
                                                className="flex w-full min-w-0 cursor-pointer items-start gap-2.5 rounded-md px-2 py-2 text-left transition-colors hover:bg-white"
                                            >
                                                {content}
                                            </button>
                                        ) : (
                                            <div className="flex min-w-0 items-start gap-2.5 px-2 py-2">{content}</div>
                                        )}
                                    </li>
                                );
                            })}
                        </ol>
                    </aside>
                    <div className="flex-1 px-5 py-8 md:px-10 md:py-10">{children}</div>
                </div>
            </section>
        </main>
    );
}
