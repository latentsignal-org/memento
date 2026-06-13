"use client";

import {useEffect} from "react";
import {useRouter} from "next/navigation";

// The Go server 302-redirects "/" to "/home"; this client redirect is the
// fallback for `next dev` and for direct openings of the exported shell.
export default function RootPage() {
    const router = useRouter();
    useEffect(() => {
        router.replace("/home");
    }, [router]);
    return null;
}
