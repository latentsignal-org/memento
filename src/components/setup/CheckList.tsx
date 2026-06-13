import type {SetupCheck} from "./types";

const icons = {ok: "check", warn: "priority_high", fail: "close"} as const;
const tones = {
    ok: "bg-[#e7f0ec] text-primary",
    warn: "bg-[#fbf4e6] text-[#7a5200]",
    fail: "bg-[#fbecec] text-destructive",
} as const;

export function CheckList({checks}: { checks: SetupCheck[] }) {
    return (
        <div className="border-t border-outline-variant/50">
            {checks.map((check) => (
                <div key={check.name} className="flex gap-3 border-b border-outline-variant/50 py-3.5">
          <span
              className={`mt-0.5 flex size-[22px] shrink-0 items-center justify-center rounded-full ${tones[check.status]}`}>
            <span className="material-symbols-outlined text-[16px]">{icons[check.status]}</span>
          </span>
                    <div className="min-w-0">
                        <p className="text-sm font-semibold text-on-surface">{check.name}</p>
                        <p className="mt-0.5 break-words text-[12.5px] text-on-surface-variant">{check.detail}</p>
                        {check.status !== "ok" && check.hint ? (
                            <p className={`mt-1.5 flex gap-1.5 text-[12.5px] ${check.status === "fail" ? "text-destructive" : "text-[#7a5200]"}`}>
                                <span className="material-symbols-outlined mt-0.5 text-[15px]">build</span>
                                <span>{check.hint}</span>
                            </p>
                        ) : null}
                    </div>
                </div>
            ))}
        </div>
    );
}
