"use client";

import {useEffect, useMemo, useState} from "react";
import type {MessageDetail, MessageSummary} from "./types";

export type MessageReferenceError = {
    status?: number;
    message: string;
};

export type MessageDataState = {
    summary?: MessageSummary | null;
    detail?: MessageDetail | null;
    isLoading: boolean;
    error?: MessageReferenceError | null;
};

const detailCache = new Map<number, MessageDetail>();
const errorCache = new Map<number, MessageReferenceError>();
const inflight = new Map<number, Promise<MessageDetail>>();

export function useMessageReferenceData(
    messageId: number,
    {
        enabled,
        summary = null,
        detail = null,
    }: {
        enabled: boolean;
        summary?: MessageSummary | null;
        detail?: MessageDetail | null;
    },
): MessageDataState {
    const cachedDetail = detailCache.get(messageId) ?? null;
    const cachedError = errorCache.get(messageId) ?? null;
    const [fetchedDetail, setFetchedDetail] = useState<MessageDetail | null>(cachedDetail);
    const [fetchError, setFetchError] = useState<{ messageId: number; error: MessageReferenceError } | null>(
        cachedError ? {messageId, error: cachedError} : null,
    );
    const [isLoading, setIsLoading] = useState(false);
    const effectiveFetchedDetail = fetchedDetail?.message_id === messageId ? fetchedDetail : cachedDetail;
    const effectiveFetchError =
        fetchError?.messageId === messageId && !effectiveFetchedDetail ? fetchError.error : cachedError;

    useEffect(() => {
        if (!enabled || detail || effectiveFetchedDetail || effectiveFetchError) {
            return;
        }

        let cancelled = false;
        window.queueMicrotask(() => {
            if (!cancelled) setIsLoading(true);
        });

        const existing = inflight.get(messageId);
        const request = existing ?? fetch(`/api/messages/${messageId}`, {cache: "no-store"})
            .then(async (res) => {
                if (!res.ok) {
                    const error: MessageReferenceError = {
                        status: res.status,
                        message: res.status === 404 ? "Source message not found." : "Preview unavailable.",
                    };
                    errorCache.set(messageId, error);
                    throw error;
                }
                const data = (await res.json()) as MessageDetail;
                detailCache.set(messageId, data);
                errorCache.delete(messageId);
                return data;
            });

        if (!existing) {
            inflight.set(messageId, request);
        }

        request
            .then((data) => {
                if (cancelled) return;
                setFetchedDetail(data);
                setFetchError(null);
            })
            .catch((error: unknown) => {
                if (cancelled) return;
                if (typeof error === "object" && error && "message" in error) {
                    setFetchError({messageId, error: error as MessageReferenceError});
                    return;
                }
                setFetchError({messageId, error: {message: "Preview unavailable."}});
            })
            .finally(() => {
                if (!existing) {
                    inflight.delete(messageId);
                }
                if (!cancelled) {
                    setIsLoading(false);
                }
            });

        return () => {
            cancelled = true;
        };
    }, [detail, effectiveFetchError, effectiveFetchedDetail, enabled, messageId]);

    return useMemo(
        () => ({
            summary,
            detail: detail ?? effectiveFetchedDetail,
            isLoading,
            error: effectiveFetchError,
        }),
        [detail, effectiveFetchError, effectiveFetchedDetail, isLoading, summary],
    );
}
