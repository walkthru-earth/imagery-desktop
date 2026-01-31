import { Monitor, Grid2X2, Settings } from 'lucide-react';
import logoImg from '@/assets/images/appicon.png';

export type ViewMode = "single" | "split";

export interface HeaderProps {
  viewMode?: ViewMode;
  onViewModeChange?: (mode: ViewMode) => void;
  onOpenSettings?: () => void;
}

export function Header({
  viewMode = "single",
  onViewModeChange,
  onOpenSettings,
}: HeaderProps) {
  return (
    <header className="flex items-center gap-4 px-6 h-14 border-b border-border bg-card">
      {/* Logo and Title */}
      <div className="flex items-center gap-3">
        <img
          src={logoImg}
          alt="Walkthru Earth"
          className="h-9 w-auto"
        />
        <h1 className="text-xl font-semibold tracking-tight">
          Walkthru Earth - Imagery Desktop
        </h1>
      </div>

      {/* Spacer */}
      <div className="flex-1" />

      {/* View Mode Toggle */}
      {onViewModeChange && (
        <div className="flex items-center gap-2">
          <button
            onClick={() => onViewModeChange("single")}
            className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
              viewMode === "single"
                ? "bg-primary text-primary-foreground shadow"
                : "bg-muted hover:bg-muted/80 text-muted-foreground"
            }`}
            title="Single View"
          >
            <Monitor className="h-4 w-4" />
            Single
          </button>
          <button
            onClick={() => onViewModeChange("split")}
            className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
              viewMode === "split"
                ? "bg-primary text-primary-foreground shadow"
                : "bg-muted hover:bg-muted/80 text-muted-foreground"
            }`}
            title="Split View"
          >
            <Grid2X2 className="h-4 w-4" />
            Split
          </button>
        </div>
      )}

      {/* Settings */}
      {onOpenSettings && (
        <button
          onClick={onOpenSettings}
          className="p-2 rounded-lg hover:bg-muted transition-colors"
          title="Settings"
        >
          <Settings className="h-4 w-4" />
        </button>
      )}
    </header>
  );
}
