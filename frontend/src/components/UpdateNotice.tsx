import { useState, useEffect } from "react";
import { X, Download, ExternalLink } from "lucide-react";
import { Button } from "@/components/ui/button";
import { BrowserOpenURL } from "../../wailsjs/runtime";

interface UpdateNoticeProps {
  currentVersion: string;
}

interface GitHubRelease {
  tag_name: string;
  html_url: string;
  published_at: string;
}

function compareVersions(current: string, latest: string): number {
  // Remove 'v' prefix if present
  const cleanCurrent = current.replace(/^v/, "");
  const cleanLatest = latest.replace(/^v/, "");

  const currentParts = cleanCurrent.split(".").map((n) => parseInt(n, 10) || 0);
  const latestParts = cleanLatest.split(".").map((n) => parseInt(n, 10) || 0);

  // Pad arrays to same length
  while (currentParts.length < 3) currentParts.push(0);
  while (latestParts.length < 3) latestParts.push(0);

  for (let i = 0; i < 3; i++) {
    if (latestParts[i] > currentParts[i]) return 1; // Update available
    if (latestParts[i] < currentParts[i]) return -1; // Current is newer
  }
  return 0; // Same version
}

export function UpdateNotice({ currentVersion }: UpdateNoticeProps) {
  const [latestRelease, setLatestRelease] = useState<GitHubRelease | null>(null);
  const [dismissed, setDismissed] = useState(false);
  const [checked, setChecked] = useState(false);

  useEffect(() => {
    // Check if already dismissed this session
    const dismissedVersion = sessionStorage.getItem("update-dismissed");
    if (dismissedVersion) {
      setDismissed(true);
      setChecked(true);
      return;
    }

    // Fetch latest release from GitHub
    fetch("https://api.github.com/repos/walkthru-earth/imagery-desktop/releases/latest")
      .then((res) => {
        if (!res.ok) throw new Error("Failed to fetch");
        return res.json();
      })
      .then((data: GitHubRelease) => {
        setLatestRelease(data);
        setChecked(true);
      })
      .catch((err) => {
        console.log("[UpdateNotice] Failed to check for updates:", err);
        setChecked(true);
      });
  }, []);

  const handleDismiss = () => {
    setDismissed(true);
    if (latestRelease) {
      sessionStorage.setItem("update-dismissed", latestRelease.tag_name);
    }
  };

  const handleDownload = () => {
    BrowserOpenURL("https://walkthru.earth/software/imagery-desktop");
  };

  // Don't render until we've checked
  if (!checked) return null;

  // Don't render if dismissed
  if (dismissed) return null;

  // Don't render if no release found or current version is empty
  if (!latestRelease || !currentVersion) return null;

  // Check if update is available
  const comparison = compareVersions(currentVersion, latestRelease.tag_name);
  if (comparison <= 0) return null; // No update available or current is newer

  return (
    <div className="fixed top-12 left-0 right-0 z-50 flex justify-center px-4 py-2 pointer-events-none">
      <div className="bg-primary text-primary-foreground rounded-lg shadow-lg px-4 py-2 flex items-center gap-3 pointer-events-auto max-w-lg">
        <Download className="h-4 w-4 shrink-0" />
        <div className="flex-1 text-sm">
          <span className="font-medium">Update available!</span>{" "}
          <span className="opacity-90">
            {latestRelease.tag_name} is now available (you have v{currentVersion})
          </span>
        </div>
        <Button
          variant="secondary"
          size="sm"
          onClick={handleDownload}
          className="shrink-0 gap-1"
        >
          Download
          <ExternalLink className="h-3 w-3" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={handleDismiss}
          className="shrink-0 h-6 w-6 hover:bg-primary-foreground/20"
        >
          <X className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}
