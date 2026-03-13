import type { Metadata } from "next";
import { GeistMono } from "geist/font/mono";
import "./globals.css";

const metadataBase = new URL(
  process.env.NEXT_PUBLIC_SITE_URL ??
    (process.env.VERCEL_PROJECT_PRODUCTION_URL
      ? `https://${process.env.VERCEL_PROJECT_PRODUCTION_URL}`
      : process.env.VERCEL_URL
        ? `https://${process.env.VERCEL_URL}`
        : "http://localhost:3000")
);

export const metadata: Metadata = {
  metadataBase,
  title: "gbash (WASM)",
  description: "gbash running in the browser via WebAssembly inside a vendored terminal website.",
  openGraph: {
    title: "gbash (WASM)",
    description: "gbash running in the browser via WebAssembly inside a vendored terminal website.",
    type: "website",
  },
  twitter: {
    card: "summary_large_image",
    title: "gbash (WASM)",
    description: "gbash running in the browser via WebAssembly inside a vendored terminal website.",
  },
};

export const viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
  interactiveWidget: "resizes-content",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body className={`${GeistMono.variable} antialiased`}>
        {children}
      </body>
    </html>
  );
}
