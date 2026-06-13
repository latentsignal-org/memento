"use client";

import Link from "next/link";

export default function EntityNotFound({
                                           kind,
                                           backHref,
                                           backLabel,
                                       }: {
    kind: string;
    backHref: string;
    backLabel: string;
}) {
    return (
        <main className="pt-16 min-h-screen flex flex-col items-center justify-center bg-background text-on-surface">
            <h1 className="text-headline-md font-headline-md text-primary mb-2 capitalize">
                {kind} not found
            </h1>
            <p className="text-ui-medium text-on-surface-variant mb-6">
                This {kind} does not exist in the local Memento database.
            </p>
            <Link
                href={backHref}
                className="px-4 py-2 rounded bg-primary text-white font-medium text-sm hover:opacity-90"
            >
                {backLabel}
            </Link>
        </main>
    );
}
