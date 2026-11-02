import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "RepoAtlas — Explore any codebase visually",
  description:
    "GitHubの公開Pythonリポジトリを静的解析し、ファイル間依存グラフと最初に読むべき重要ファイルを可視化するWebアプリケーション。",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="ja">
      <body>{children}</body>
    </html>
  );
}
