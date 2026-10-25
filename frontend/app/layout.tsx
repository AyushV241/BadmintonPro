import type { Metadata } from "next";
import { DM_Sans, Geist_Mono } from "next/font/google";
import { ColorSchemeScript, UIProvider } from "@/components/ui";
import "./globals.css";

const dmSans = DM_Sans({
  variable: "--font-dm-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "BadmintonPro",
  description: "Badminton club and match management",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    // The colour-scheme script sets an attribute on <html> before React
    // hydrates, so React shouldn't flag that difference.
    <html
      lang="en"
      className={`${dmSans.variable} ${geistMono.variable} h-full antialiased`}
      suppressHydrationWarning
    >
      <body className="min-h-full flex flex-col">
        <ColorSchemeScript />
        <UIProvider>{children}</UIProvider>
      </body>
    </html>
  );
}
