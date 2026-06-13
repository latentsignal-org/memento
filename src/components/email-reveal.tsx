"use client";

import {MouseEvent, useEffect, useState} from "react";
import {isPrivacyEnabled, maskEmail} from "@/lib/contact-display";

interface EmailRevealProps {
    email?: string | null;
    className?: string;
    buttonClassName?: string;
    revealLabel?: string;
    hideLabel?: string;
}

export default function EmailReveal({
                                        email,
                                        className = "",
                                        buttonClassName = "",
                                        revealLabel = "Reveal",
                                        hideLabel = "Hide",
                                    }: EmailRevealProps) {
    const [revealed, setRevealed] = useState(false);
    const [copied, setCopied] = useState(false);
    const [mounted, setMounted] = useState(false);

    useEffect(() => {
        setMounted(true);
    }, []);

    const normalized = (email || "").trim();
    const canReveal = normalized.includes("@");
    const privacyEnabled = mounted ? isPrivacyEnabled() : true;

    const handleToggle = (event: MouseEvent<HTMLButtonElement>) => {
        event.preventDefault();
        event.stopPropagation();
        setRevealed((current) => !current);
    };

    const handleCopy = async (event: MouseEvent<HTMLButtonElement>) => {
        event.preventDefault();
        event.stopPropagation();
        try {
            await navigator.clipboard.writeText(normalized);
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
        } catch (err) {
            console.error("Failed to copy email address", err);
        }
    };

    if (!privacyEnabled) {
        return (
            <span className={`group/email-reveal inline-flex items-center gap-1.5 min-w-0 ${className}`}>
        <span className="truncate [overflow-wrap:anywhere]">
          {normalized}
        </span>
                {canReveal && (
                    <button
                        type="button"
                        onClick={handleCopy}
                        className={`inline-flex items-center justify-center text-on-surface-variant/75 transition-colors hover:text-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary ${buttonClassName}`}
                        aria-label={copied ? "Copied email address" : "Copy email address"}
                        title={copied ? "Copied!" : "Copy to clipboard"}
                    >
            <span className="material-symbols-outlined leading-none" style={{fontSize: "14px"}} aria-hidden="true">
              {copied ? "check" : "content_copy"}
            </span>
                        <span className="sr-only">{copied ? "Copied!" : "Copy"}</span>
                    </button>
                )}
      </span>
        );
    }

    return (
        <span className={`group/email-reveal inline-flex items-center gap-1.5 min-w-0 ${className}`}>
      <span className="truncate [overflow-wrap:anywhere]">
        {revealed && canReveal ? normalized : maskEmail(normalized, privacyEnabled)}
      </span>
            {canReveal && (
                <button
                    type="button"
                    onClick={handleToggle}
                    className={`inline-flex items-center justify-center text-on-surface-variant/75 transition-colors hover:text-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary ${buttonClassName}`}
                    aria-label={revealed ? "Hide email address" : "Reveal email address"}
                    title={revealed ? "Hide email address" : "Reveal email address"}
                >
          <span className="material-symbols-outlined leading-none" style={{fontSize: "14px"}} aria-hidden="true">
            {revealed ? "visibility_off" : "visibility"}
          </span>
                    <span className="sr-only">{revealed ? hideLabel : revealLabel}</span>
                </button>
            )}
    </span>
    );
}
