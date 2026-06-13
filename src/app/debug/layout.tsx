import type {Metadata} from "next";

export const metadata: Metadata = {
    title: "Debug | Memento",
};

export default function DebugLayout({children}: { children: React.ReactNode }) {
    return children;
}
