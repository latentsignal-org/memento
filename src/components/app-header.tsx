"use client";

import Link from "next/link";
import Image from "next/image";
import {usePathname} from "next/navigation";
import {useEffect, useRef, useState} from "react";
import EmailReveal from "@/components/email-reveal";
import RefreshButton from "@/components/refresh-button";
import {isPrivacyEnabled, setPrivacyEnabled} from "@/lib/contact-display";
import {privacyEnabled as readPrivacyEnabled} from "@/lib/api";

interface ApiConfig {
    db_path?: string;
    port?: number;
    cors_allowed_origins?: string[];
    owner_name?: string;
    owner_email?: string;
    owner_avatar_url?: string;
    privacy_enabled?: boolean;
}

export default function AppHeader() {
    const pathname = usePathname();
    const [settingsOpen, setSettingsOpen] = useState(false);
    const [mobileNavOpen, setMobileNavOpen] = useState(false);
    const [apiConfig, setApiConfig] = useState<ApiConfig | null>(null);
    const [apiReachable, setApiReachable] = useState<boolean | null>(null);
    const settingsRef = useRef<HTMLDivElement | null>(null);
    const [privacyEnabled, setPrivacyEnabledState] = useState(() => isPrivacyEnabled());

    const handleTogglePrivacy = async (checked: boolean) => {
        setPrivacyEnabledState(checked);
        setPrivacyEnabled(checked);
        document.cookie = `memento_privacy_enabled=${checked}; path=/; max-age=31536000`;
        try {
            await fetch("/api/config", {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({privacy_enabled: checked}),
            });
        } catch (err) {
            console.error("Failed to save privacy setting", err);
        }
        window.location.reload();
    };

    // Fetch /api/config on mount for owner name/email, and again when settings opens.
    const fetchConfig = (cancelled: { value: boolean }) => {
        fetch("/api/config", {cache: "no-store"})
            .then(async (res) => {
                if (cancelled.value) return;
                if (!res.ok) {
                    setApiReachable(false);
                    return;
                }
                const data = (await res.json()) as ApiConfig;
                if (data.privacy_enabled !== undefined) {
                    const serverVal = data.privacy_enabled;
                    const browserVal = readPrivacyEnabled();
                    if (serverVal !== browserVal) {
                        document.cookie = `memento_privacy_enabled=${serverVal}; path=/; max-age=31536000`;
                        setPrivacyEnabled(serverVal);
                        window.location.reload();
                        return;
                    }
                    setPrivacyEnabled(serverVal);
                    setPrivacyEnabledState(serverVal);
                }
                setApiConfig(data);
                setApiReachable(true);
            })
            .catch(() => {
                if (!cancelled.value) setApiReachable(false);
            });
    };

    useEffect(() => {
        const cancelled = {value: false};
        fetchConfig(cancelled);
        return () => {
            cancelled.value = true;
        };
    }, []);

    useEffect(() => {
        if (!settingsOpen) return;
        const cancelled = {value: false};
        fetchConfig(cancelled);
        return () => {
            cancelled.value = true;
        };
    }, [settingsOpen]);


    useEffect(() => {
        if (!settingsOpen) return;

        const handlePointerDown = (event: MouseEvent) => {
            if (!settingsRef.current?.contains(event.target as Node)) {
                setSettingsOpen(false);
            }
        };
        const handleKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") setSettingsOpen(false);
        };

        document.addEventListener("mousedown", handlePointerDown);
        document.addEventListener("keydown", handleKeyDown);
        return () => {
            document.removeEventListener("mousedown", handlePointerDown);
            document.removeEventListener("keydown", handleKeyDown);
        };
    }, [settingsOpen]);


    // Close the mobile menu on Escape.
    useEffect(() => {
        if (!mobileNavOpen) return;
        const handleKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") setMobileNavOpen(false);
        };
        document.addEventListener("keydown", handleKeyDown);
        return () => document.removeEventListener("keydown", handleKeyDown);
    }, [mobileNavOpen]);

    const navItems = [
        {name: "Home", href: "/home"},
        {name: "People", href: "/people"},
        {name: "Sessions", href: "/sessions"},
        {name: "Projects", href: "/projects"},
        {name: "Concepts", href: "/concepts"},
        {name: "Newsletter", href: "/newsletters"},
    ];

    if (pathname.startsWith("/onboard")) return null;

    return (
        <>
            <header
                className="fixed top-0 left-0 right-0 h-16 flex justify-between items-center px-4 md:px-8 bg-surface border-b border-outline-variant z-40 w-full">
                {/* Brand logo; hide the wordmark in compact header layouts. */}
                <div className="flex items-center justify-start lg:flex-1">
                    <Link href="/home" className="flex items-center gap-3">
                        <Image
                            src="/memento-logo.png"
                            alt=""
                            width={32}
                            height={32}
                            className="size-8 rounded-md"
                            priority
                        />
                        <span
                            className="hidden lg:inline text-headline-md font-headline-md text-primary font-bold">Memento</span>
                    </Link>
                </div>

                {/* Center Top Navigation */}
                <div className="hidden lg:flex items-center justify-center flex-1 min-w-0">
                    <nav className="flex gap-12 items-center">
                        {navItems.map((item) => {
                            const isActive = pathname.startsWith(item.href) || (pathname === "/" && item.href === "/people");
                            return (
                                <Link
                                    key={item.name}
                                    href={item.href}
                                    className={`pb-1 border-b-2 transition-colors text-ui-small font-ui-small whitespace-nowrap ${
                                        isActive
                                            ? "text-primary font-bold border-primary"
                                            : "text-on-surface-variant border-transparent hover:text-primary"
                                    }`}
                                >
                                    {item.name}
                                </Link>
                            );
                        })}
                    </nav>
                </div>

                {/* Right-Side Search and Profile */}
                <div className="flex items-center justify-end lg:flex-1 gap-2">

                    {/* Auxiliary Icons */}
                    <button
                        type="button"
                        className="hidden lg:flex w-9 h-9 items-center justify-center hover:bg-surface-container-high rounded-full transition-colors active:scale-95 text-primary cursor-pointer"
                        aria-label="Help"
                    >
                        <span className="material-symbols-outlined text-[20px]">help_outline</span>
                    </button>
                    <div className="relative" ref={settingsRef}>
                        <button
                            type="button"
                            onClick={() => setSettingsOpen((current) => !current)}
                            className={`hidden lg:flex w-9 h-9 items-center justify-center rounded-full transition-colors active:scale-95 text-primary cursor-pointer ${
                                settingsOpen ? "bg-primary-fixed" : "hover:bg-surface-container-high"
                            }`}
                            aria-label="Open settings"
                            aria-expanded={settingsOpen}
                        >
                            <span className="material-symbols-outlined text-[20px]">settings</span>
                        </button>

                        {settingsOpen && (
                            <div
                                className="absolute right-0 top-11 w-[380px] max-lg:fixed max-lg:inset-x-4 max-lg:top-20 max-lg:w-auto bg-background border border-outline-variant/70 rounded-xl shadow-xl z-50 overflow-hidden text-on-surface">
                                <div className="p-4 border-b border-outline-variant/50 bg-surface-container-low">
                                    <div className="flex items-start justify-between gap-4">
                                        <div className="min-w-0">
                                            <p className="text-label-caps font-label-caps text-primary mb-1">Vault
                                                Owner</p>
                                            <h2 className="text-ui-medium font-bold text-on-surface truncate">
                                                {apiConfig?.owner_name || "—"}
                                            </h2>
                                            <div className="text-ui-small text-on-surface-variant font-mono mt-1">
                                                {apiConfig?.owner_email ?
                                                    <EmailReveal email={apiConfig.owner_email}/> : null}
                                            </div>
                                        </div>
                                        <OwnerAvatar
                                            name={apiConfig?.owner_name}
                                            avatarUrl={apiConfig?.owner_avatar_url}
                                        />
                                    </div>
                                </div>

                                <div className="p-3 space-y-3">
                                    <SettingsToggleRow
                                        icon={privacyEnabled ? "visibility_off" : "visibility"}
                                        title="Demo privacy"
                                        detail="Emails masked by default. Reveal is per-contact and resets on refresh."
                                        checked={privacyEnabled}
                                        onChange={handleTogglePrivacy}
                                    />
                                    <div
                                        className="rounded-lg px-3 py-2.5 bg-surface-container-low border border-outline-variant/40">
                                        <div className="flex items-start gap-3">
                                            <span
                                                className="material-symbols-outlined text-[18px] text-primary mt-0.5">storage</span>
                                            <div className="min-w-0 flex-1">
                                                <p className="text-ui-small font-bold text-on-surface">Memento
                                                    Server</p>
                                                {apiReachable === false && (
                                                    <p className="text-[11px] text-destructive mt-0.5">
                                                        Configured backend is not reachable. Start with{" "}
                                                        <code className="font-mono">go run ./cmd/memento serve</code>.
                                                    </p>
                                                )}
                                                {apiReachable && apiConfig && (
                                                    <>
                                                        <p className="text-[11px] text-on-surface-variant mt-0.5 font-mono truncate"
                                                           title={apiConfig.db_path}>
                                                            DB: {abbreviatePath(apiConfig.db_path || "")}
                                                        </p>
                                                        <p className="text-[11px] text-on-surface-variant opacity-70 mt-0.5">
                                                            Port {apiConfig.port} · 127.0.0.1 only
                                                        </p>
                                                    </>
                                                )}
                                                {apiReachable === null && (
                                                    <p className="text-[11px] text-on-surface-variant opacity-60 mt-0.5">Checking…</p>
                                                )}
                                            </div>
                                        </div>
                                    </div>

                                    <div
                                        className="rounded-lg px-3 py-2.5 bg-surface-container-low border border-outline-variant/40 space-y-2">
                                        <p className="text-ui-small font-bold text-on-surface flex items-center gap-2">
                                            <span
                                                className="material-symbols-outlined text-[18px] text-primary">refresh</span>
                                            Refresh memory
                                        </p>
                                        <p className="text-[10px] text-on-surface-variant opacity-70 -mt-1">
                                            Triggers a background pipeline run via the Go API. Pages reload to pick up
                                            changes.
                                        </p>
                                        <RefreshButton label="Re-run People resolver + candidates"
                                                       endpoint="/api/people/refresh"/>
                                        <RefreshButton label="Re-run Newsletter detection"
                                                       endpoint="/api/newsletters/detect"/>
                                    </div>
                                </div>
                            </div>
                        )}
                    </div>

                    {/* User Profile Avatar */}
                    <div className="hidden lg:block w-8 h-8 rounded-full flex-shrink-0">
                        <OwnerAvatar
                            name={apiConfig?.owner_name}
                            avatarUrl={apiConfig?.owner_avatar_url}
                            size="sm"
                        />
                    </div>

                    {/* Mobile Menu Toggle */}
                    <button
                        type="button"
                        onClick={() => setMobileNavOpen((current) => !current)}
                        className={`lg:hidden w-9 h-9 flex items-center justify-center rounded-full transition-colors active:scale-95 text-primary cursor-pointer ${
                            mobileNavOpen ? "bg-primary-fixed" : "hover:bg-surface-container-high"
                        }`}
                        aria-label={mobileNavOpen ? "Close navigation menu" : "Open navigation menu"}
                        aria-expanded={mobileNavOpen}
                    >
                        <span
                            className="material-symbols-outlined text-[22px]">{mobileNavOpen ? "close" : "menu"}</span>
                    </button>
                </div>
            </header>
            {mobileNavOpen && (
                <div className="lg:hidden fixed inset-0 top-16 z-40" onClick={() => setMobileNavOpen(false)}>
                    <div className="absolute inset-0 bg-on-surface/20" aria-hidden/>
                    <nav
                        className="relative bg-surface border-b border-outline-variant shadow-lg flex flex-col py-2"
                        onClick={(event) => event.stopPropagation()}
                    >
                        {navItems.map((item) => {
                            const isActive = pathname.startsWith(item.href);
                            return (
                                <Link
                                    key={item.name}
                                    href={item.href}
                                    onClick={() => setMobileNavOpen(false)}
                                    className={`px-6 py-3 text-ui-medium border-l-4 transition-colors ${
                                        isActive
                                            ? "text-primary font-bold border-primary bg-primary-fixed/30"
                                            : "text-on-surface-variant border-transparent hover:text-primary hover:bg-surface-container-low"
                                    }`}
                                >
                                    {item.name}
                                </Link>
                            );
                        })}
                        <div className="my-2 border-t border-outline-variant/50"/>
                        <button
                            type="button"
                            onClick={() => {
                                setMobileNavOpen(false);
                                setSettingsOpen(true);
                            }}
                            className="px-6 py-3 text-ui-medium border-l-4 border-transparent text-on-surface-variant hover:text-primary hover:bg-surface-container-low flex items-center gap-2 text-left cursor-pointer"
                        >
                            <span className="material-symbols-outlined text-[18px]">settings</span>
                            Settings
                        </button>
                    </nav>
                </div>
            )}
            {apiReachable === false ? (
                <div
                    className="fixed left-0 right-0 top-16 z-30 border-b border-[#e6d5ad] bg-[#fbf4e6] px-4 py-2 text-center text-xs text-[#7a5200]">
                    Memento backend is offline. Start <code className="font-mono">./memento serve</code>, try <code
                    className="font-mono">./memento serve --demo</code>, or verify <code
                    className="font-mono">MEMENTO_BACKEND_URL</code>.
                </div>
            ) : null}
        </>
    );
}

// abbreviatePath shortens long absolute paths for display in the panel.
// Keeps the leaf segment and a hint at the parent.
function abbreviatePath(p: string): string {
    if (!p) return "(unknown)";
    if (p.length <= 48) return p;
    const parts = p.split("/");
    if (parts.length <= 3) return p;
    return ".../" + parts.slice(-3).join("/");
}

function OwnerAvatar({name, avatarUrl, size = "lg"}: { name?: string; avatarUrl?: string; size?: "sm" | "lg" }) {
    const initials = name
        ? name.split(" ").map((w) => w[0]).join("").slice(0, 2).toUpperCase()
        : "?";
    const dim = size === "sm" ? "w-8 h-8 text-[13px]" : "w-14 h-14 text-[18px]";
    const radius = size === "sm" ? "rounded-full" : "rounded-xl";
    if (avatarUrl) {
        return (
            <img
                src={avatarUrl}
                alt={name ? `${name} avatar` : "Vault owner avatar"}
                className={`${dim} ${radius} border border-outline-variant shrink-0 object-cover bg-primary-fixed`}
            />
        );
    }
    return (
        <div
            className={`${dim} ${radius} bg-primary text-white flex items-center justify-center font-bold border border-outline-variant shrink-0`}>
            {initials}
        </div>
    );
}

function SettingsToggleRow({
                               icon,
                               title,
                               detail,
                               checked,
                               onChange,
                           }: {
    icon: string;
    title: string;
    detail: string;
    checked: boolean;
    onChange: (checked: boolean) => void;
}) {
    return (
        <label
            className="flex items-start gap-3 rounded-lg px-3 py-2.5 hover:bg-surface-container-low transition-colors cursor-pointer w-full text-left">
            <span className="material-symbols-outlined text-[18px] text-primary mt-0.5">{icon}</span>
            <div className="min-w-0 flex-1">
                <p className="text-ui-small font-bold text-on-surface">{title}</p>
                <p className="text-[11px] leading-relaxed text-on-surface-variant mt-0.5">{detail}</p>
            </div>
            <input
                type="checkbox"
                checked={checked}
                onChange={(e) => onChange(e.target.checked)}
                className="mt-1 h-4 w-4 rounded border-outline-variant text-primary focus:ring-primary/20 accent-primary"
            />
        </label>
    );
}
