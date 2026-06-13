"use client";

import {useEffect, useMemo, useState} from "react";
import type {MessageDetail} from "./types";

export function useMessageDetail(messageId: number | null) {
    const [cache, setCache] = useState<Record<number, MessageDetail>>({});
    const [loadingIds, setLoadingIds] = useState<Record<number, boolean>>({});
    const [errors, setErrors] = useState<Record<number, string>>({});
    const [attemptedIds, setAttemptedIds] = useState<Record<number, boolean>>({});

    useEffect(() => {
        if (!messageId || cache[messageId] || loadingIds[messageId] || attemptedIds[messageId]) {
            return;
        }

        let cancelled = false;
        setLoadingIds((current) => ({...current, [messageId]: true}));
        setAttemptedIds((current) => ({...current, [messageId]: true}));

        (async () => {
            try {
                const res = await fetch(`/api/messages/${messageId}`, {cache: "no-store"});
                if (!res.ok) {
                    throw new Error(`message ${messageId}: ${res.status}`);
                }
                const data = (await res.json()) as MessageDetail;
                if (cancelled) return;
                setCache((current) => ({...current, [messageId]: data}));
                setErrors((current) => {
                    const next = {...current};
                    delete next[messageId];
                    return next;
                });
            } catch (error) {
                if (cancelled) return;
                setErrors((current) => ({
                    ...current,
                    [messageId]: error instanceof Error ? error.message : String(error),
                }));
            } finally {
                if (!cancelled) {
                    setLoadingIds((current) => {
                        const next = {...current};
                        delete next[messageId];
                        return next;
                    });
                }
            }
        })();

        return () => {
            cancelled = true;
        };
    }, [messageId]);

    return useMemo(
        () => ({
            detail: messageId ? cache[messageId] ?? null : null,
            isLoading: Boolean(messageId && loadingIds[messageId]),
            error: messageId ? errors[messageId] ?? null : null,
        }),
        [cache, errors, loadingIds, messageId],
    );
}
