import { Monitor, Grid2X2, Settings } from 'lucide-react';
import logoImg from '@/assets/images/logo-universal.png';
import { ThemeToggle } from '@/components/ThemeToggle';

export type ViewMode = "single" | "split";

export interface HeaderProps {
  viewMode?: ViewMode;
  onViewModeChange?: (mode: ViewMode) => void;
  showExportOptions?: boolean;
  isRangeMode?: boolean;
  exportOptions?: {
    mergedGeotiff: boolean;
    tiles: boolean;
    mp4: boolean;
    gif: boolean;
  };
  onExportOptionsChange?: (options: {
    mergedGeotiff?: boolean;
    tiles?: boolean;
    mp4?: boolean;
    gif?: boolean;
  }) => void;
  onExport?: () => void;
  onOpenSettings?: () => void;
}

export function Header({
  viewMode = "single",
  onViewModeChange,
  showExportOptions = false,
  isRangeMode = false,
  exportOptions,
  onExportOptionsChange,
  onExport,
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
          Walkthru Earth
        </h1>
      </div>

      {/* Theme Toggle and Settings */}
      <div className={onViewModeChange ? "flex items-center gap-2" : "ml-auto flex items-center gap-2"}>
        <ThemeToggle />
        {onOpenSettings && (
          <button
            onClick={onOpenSettings}
            className="p-2 rounded-lg hover:bg-muted transition-colors"
            title="Settings"
          >
            <Settings className="h-4 w-4" />
          </button>
        )}
      </div>

      {/* View Mode Toggle - Global Control */}
      {onViewModeChange && (
        <div className="flex items-center gap-2 ml-auto">
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

      {/* Export Options - Visible for single view when imagery is active */}
      {showExportOptions && exportOptions && onExportOptionsChange && onExport && (
        <div className="flex items-center gap-4 ml-4 pl-4 border-l border-border">
          {isRangeMode ? (
            // Range mode: Show format options for timelapse export
            <div className="flex items-center gap-3">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={exportOptions.mergedGeotiff}
                  onChange={(e) => onExportOptionsChange({ mergedGeotiff: e.target.checked })}
                  className="w-4 h-4 rounded border-border accent-primary"
                />
                <span className="text-sm font-medium">GeoTIFF</span>
              </label>
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={exportOptions.tiles}
                  onChange={(e) => onExportOptionsChange({ tiles: e.target.checked })}
                  className="w-4 h-4 rounded border-border accent-primary"
                />
                <span className="text-sm font-medium">Tiles</span>
              </label>
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={exportOptions.mp4}
                  onChange={(e) => onExportOptionsChange({ mp4: e.target.checked })}
                  className="w-4 h-4 rounded border-border accent-primary"
                />
                <span className="text-sm font-medium">MP4</span>
              </label>
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={exportOptions.gif}
                  onChange={(e) => onExportOptionsChange({ gif: e.target.checked })}
                  className="w-4 h-4 rounded border-border accent-primary"
                />
                <span className="text-sm font-medium">GIF</span>
              </label>
            </div>
          ) : (
            // Single date mode: Simple export button (GeoTIFF only)
            <span className="text-sm text-muted-foreground">Export as GeoTIFF</span>
          )}
          <button
            onClick={onExport}
            className="px-5 py-2 rounded-lg text-sm font-medium bg-primary text-primary-foreground hover:bg-primary/90 transition-colors shadow"
          >
            Export
          </button>
        </div>
      )}
    </header>
  );
}
