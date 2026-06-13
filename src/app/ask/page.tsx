import {Metadata} from "next";
import {Suspense} from "react";
import AskMementoClient from "@/components/agent/AskMementoClient";

export const metadata: Metadata = {
    title: "Ask Memento",
    description: "Chat with Memento to explore your People, Projects, Newsletters, and Concepts.",
};

export default function AskMementoPage() {
    return (
        <Suspense fallback={null}>
            <AskMementoClient/>
        </Suspense>
    );
}
