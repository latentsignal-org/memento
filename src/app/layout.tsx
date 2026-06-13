import type {Metadata} from "next";
import {Inter, Source_Serif_4} from "next/font/google";
import "./globals.css";
import AppHeader from "@/components/app-header";
import {Suspense} from "react";

const inter = Inter({
    variable: "--font-inter",
    subsets: ["latin"],
});

const sourceSerif = Source_Serif_4({
    variable: "--font-source-serif",
    subsets: ["latin"],
});

const siteDescription = "Discover the Decades of Memory Living in Your Inbox.";
const siteUrl = process.env.NEXT_PUBLIC_MEMENTO_SITE_URL || "http://localhost:3000";
const ogImage = {
    url: "/memento-og.webp",
    width: 1200,
    height: 630,
    alt: "Memento — Discover the Decades of Memory Living in Your Inbox.",
};

export const metadata: Metadata = {
    metadataBase: new URL(siteUrl),
    title: "Memento",
    description: siteDescription,
    applicationName: "Memento",
    openGraph: {
        title: "Memento",
        description: siteDescription,
        siteName: "Memento",
        type: "website",
        images: [ogImage],
    },
    twitter: {
        card: "summary_large_image",
        title: "Memento",
        description: siteDescription,
        images: [ogImage],
    },
};

export default function RootLayout({
                                       children,
                                   }: Readonly<{
    children: React.ReactNode;
}>) {
    return (
        <html
            lang="en"
            className={`${inter.variable} ${sourceSerif.variable} h-full antialiased`}
        >
        <head>
            <link
                href="https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:wght,FILL@100..700,0..1&display=swap"
                rel="stylesheet"
            />
        </head>
        <body className="min-h-full flex flex-col bg-background text-on-background">
        <Suspense fallback={<div className="h-16 bg-surface border-b border-outline-variant"/>}>
            <AppHeader/>
        </Suspense>
        {children}
        </body>
        </html>
    );
}
