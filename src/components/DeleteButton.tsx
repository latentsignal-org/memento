"use client";
import {useState} from "react";
import {Trash2} from "lucide-react";

interface DeleteButtonProps {
    onDelete: () => Promise<void>;
}

export function DeleteButton({onDelete}: DeleteButtonProps) {
    const [state, setState] = useState<"idle" | "confirming" | "deleting">("idle");

    const stopEvent = (e: React.MouseEvent) => {
        e.preventDefault();
        e.stopPropagation();
    };

    const handleTrashClick = (e: React.MouseEvent) => {
        stopEvent(e);
        setState("confirming");
    };

    const handleConfirm = async (e: React.MouseEvent) => {
        stopEvent(e);
        setState("deleting");
        try {
            await onDelete();
        } catch {
            setState("idle");
        }
    };

    const handleCancel = (e: React.MouseEvent) => {
        stopEvent(e);
        setState("idle");
    };

    // When confirming/deleting, always visible; when idle, reveal on card hover.
    // Touch devices have no hover, so stay visible below the md breakpoint.
    const wrapperClass =
        state !== "idle"
            ? "flex items-center"
            : "flex items-center opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity";

    if (state === "deleting") {
        return (
            <span className={`${wrapperClass} text-[10px] text-on-surface-variant/60 animate-pulse select-none`}>
        Deleting…
      </span>
        );
    }

    if (state === "confirming") {
        return (
            <div className={`${wrapperClass} gap-1`} onClick={stopEvent}>
        <span className="text-[10px] text-on-surface-variant font-medium select-none mr-0.5">
          Delete?
        </span>
                <button
                    onClick={handleConfirm}
                    className="text-[10px] font-bold text-red-700 hover:text-red-800 px-1.5 py-0.5 rounded bg-red-50 border border-red-200 hover:bg-red-100 transition"
                >
                    Yes
                </button>
                <button
                    onClick={handleCancel}
                    className="text-[10px] font-bold text-on-surface-variant hover:text-on-surface px-1.5 py-0.5 rounded bg-surface-container border border-outline-variant hover:bg-surface-container-high transition"
                >
                    No
                </button>
            </div>
        );
    }

    return (
        <div className={wrapperClass} onClick={stopEvent}>
            <button
                onClick={handleTrashClick}
                aria-label="Delete"
                className="p-1 rounded text-on-surface-variant/50 hover:text-red-600 hover:bg-red-50 transition-colors"
            >
                <Trash2 size={16}/>
            </button>
        </div>
    );
}
