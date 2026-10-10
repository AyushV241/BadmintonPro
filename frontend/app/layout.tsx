import type { Metadata } from "next";
import { DM_Sans, Geist_Mono } from "next/font/google";
import { UIProvider } from "@/components/ui";
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
    <html
      lang="en"
      className={`${dmSans.variable} ${geistMono.variable} h-full antialiased`}
    >
      <body className="min-h-full flex flex-col">
        <UIProvider>{children}</UIProvider>
      </body>
    </html>
  );
}
