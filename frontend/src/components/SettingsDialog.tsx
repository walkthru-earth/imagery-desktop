import * as React from "react";
import { useState, useEffect } from "react";
import { X, FolderOpen, Save, Plus, Trash2, Globe, FileCode } from "lucide-react";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { api } from "@/services/api";
import { useTheme } from "@/components/ThemeProvider";
import iconSvg from "@/assets/images/icon.svg";

interface UserSettings {
  downloadPath: string;
  cacheMaxSizeMB: number;
  cacheTTLDays: number;
  defaultZoom: number;
  defaultSource: string;
  defaultCenterLat: number;
  defaultCenterLon: number;
  customSources: CustomSource[];
  dateFilterPatterns: DateFilterPattern[];
  defaultDatePattern: string;
  theme: string;
  showTileGrid: boolean;
  showCoordinates: boolean;
  autoOpenDownloadDir: boolean;
  downloadZoomStrategy: "current" | "fixed";
  downloadFixedZoom: number;
}

interface CustomSource {
  name: string;
  type: "wmts" | "wms" | "xyz" | "tms";
  url: string;
  attribution?: string;
  maxZoom?: number;
  minZoom?: number;
  enabled: boolean;
}

interface DateFilterPattern {
  name: string;
  pattern: string;
  enabled: boolean;
}

export interface SettingsDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

export function SettingsDialog({ isOpen, onClose }: SettingsDialogProps) {
  const { theme, setTheme } = useTheme();
  const [settings, setSettings] = useState<UserSettings | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [activeTab, setActiveTab] = useState<"general" | "sources" | "dates" | "about">("general");
  const [appVersion, setAppVersion] = useState("...");

  // New custom source form
  const [showAddSource, setShowAddSource] = useState(false);
  const [newSourceName, setNewSourceName] = useState("");
  const [newSourceType, setNewSourceType] = useState<"wmts" | "wms" | "xyz" | "tms">("wmts");
  const [newSourceURL, setNewSourceURL] = useState("");
  const [newSourceAttribution, setNewSourceAttribution] = useState("");

  // New date pattern form
  const [showAddPattern, setShowAddPattern] = useState(false);
  const [newPatternName, setNewPatternName] = useState("");
  const [newPatternRegex, setNewPatternRegex] = useState("");

  // Load settings
  useEffect(() => {
    if (isOpen) {
      loadSettings();
      // Fetch app version
      if ((window as any).go?.main?.App?.GetAppVersion) {
        (window as any).go.main.App.GetAppVersion().then(setAppVersion);
      }
    }
  }, [isOpen]);

  const loadSettings = async () => {
    setIsLoading(true);
    try {
      const loadedSettings = await (window as any).go.main.App.GetSettings();
      setSettings(loadedSettings);
    } catch (error) {
      console.error("Failed to load settings:", error);
    } finally {
      setIsLoading(false);
    }
  };

  const handleSelectFolder = async () => {
    try {
      const path = await api.selectDownloadFolder();
      if (path && settings) {
        setSettings({ ...settings, downloadPath: path });
      }
    } catch (error) {
      console.error("Failed to select folder:", error);
    }
  };

  const handleSave = async () => {
    if (!settings) return;

    setIsSaving(true);
    try {
      await (window as any).go.main.App.SaveSettings(settings);
      setTimeout(() => {
        onClose();
      }, 500);
    } catch (error) {
      console.error("Failed to save settings:", error);
      alert(`Failed to save settings: ${error}`);
    } finally {
      setIsSaving(false);
    }
  };

  const handleOpenFolder = async () => {
    try {
      await api.openDownloadFolder();
    } catch (error) {
      console.error("Failed to open download folder:", error);
    }
  };

  const handleAddCustomSource = async () => {
    if (!settings || !newSourceName || !newSourceURL) return;

    try {
      const newSource: CustomSource = {
        name: newSourceName,
        type: newSourceType,
        url: newSourceURL,
        attribution: newSourceAttribution,
        maxZoom: 18,
        minZoom: 0,
        enabled: true,
      };

      await (window as any).go.main.App.AddCustomSource(newSource);

      // Reload settings
      await loadSettings();

      // Reset form
      setNewSourceName("");
      setNewSourceURL("");
      setNewSourceAttribution("");
      setShowAddSource(false);
    } catch (error) {
      console.error("Failed to add custom source:", error);
      alert(`Failed to add source: ${error}`);
    }
  };

  const handleRemoveSource = async (name: string) => {
    try {
      await (window as any).go.main.App.RemoveCustomSource(name);
      await loadSettings();
    } catch (error) {
      console.error("Failed to remove source:", error);
    }
  };

  const handleAddDatePattern = async () => {
    if (!newPatternName || !newPatternRegex) return;

    try {
      await (window as any).go.main.App.AddDateFilterPattern({
        name: newPatternName,
        pattern: newPatternRegex,
        enabled: true,
      });

      await loadSettings();
      setNewPatternName("");
      setNewPatternRegex("");
      setShowAddPattern(false);
    } catch (error) {
      console.error("Failed to add pattern:", error);
      alert(`Failed to add pattern: ${error}`);
    }
  };

  const handleRemovePattern = async (name: string) => {
    try {
      await (window as any).go.main.App.RemoveDateFilterPattern(name);
      await loadSettings();
    } catch (error) {
      console.error("Failed to remove pattern:", error);
    }
  };

  if (!isOpen) return null;
  if (isLoading || !settings) {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm">
        <Card className="w-full max-w-3xl mx-4">
          <CardContent className="p-12 text-center">
            <p className="text-muted-foreground">Loading settings...</p>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm">
      <Card className="w-full max-w-4xl mx-4 max-h-[90vh] flex flex-col">
        <CardHeader className="flex flex-row items-center justify-between border-b shrink-0">
          <div>
            <h2 className="text-xl font-semibold">Settings</h2>
            <p className="text-sm text-muted-foreground mt-1">
              Configure application preferences
            </p>
          </div>
          <Button variant="ghost" size="sm" onClick={onClose}>
            <X className="h-4 w-4" />
          </Button>
        </CardHeader>

        {/* Tabs */}
        <div className="flex gap-1 px-6 pt-4 border-b shrink-0">
          <button
            onClick={() => setActiveTab("general")}
            className={`px-4 py-2 text-sm font-medium rounded-t-lg transition-colors ${
              activeTab === "general"
                ? "bg-background text-foreground border-t border-x"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            General
          </button>
          <button
            onClick={() => setActiveTab("sources")}
            className={`px-4 py-2 text-sm font-medium rounded-t-lg transition-colors ${
              activeTab === "sources"
                ? "bg-background text-foreground border-t border-x"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            Custom Sources ({settings.customSources.length})
          </button>
          <button
            onClick={() => setActiveTab("dates")}
            className={`px-4 py-2 text-sm font-medium rounded-t-lg transition-colors ${
              activeTab === "dates"
                ? "bg-background text-foreground border-t border-x"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            Date Filters ({settings.dateFilterPatterns.length})
          </button>
          <button
            onClick={() => setActiveTab("about")}
            className={`px-4 py-2 text-sm font-medium rounded-t-lg transition-colors ${
              activeTab === "about"
                ? "bg-background text-foreground border-t border-x"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            About
          </button>
        </div>

        <CardContent className="p-6 space-y-6 overflow-y-auto flex-1">
          {activeTab === "general" && (
            <>
              {/* Download Location */}
              <div className="space-y-3">
                <label className="text-sm font-medium">Download Location</label>
                <div className="flex gap-2">
                  <div className="flex-1 p-3 border rounded-lg bg-muted/50 text-sm font-mono break-all">
                    {settings.downloadPath || "No folder selected"}
                  </div>
                  <Button variant="outline" onClick={handleSelectFolder} disabled={isSaving}>
                    Browse
                  </Button>
                  <Button
                    variant="outline"
                    onClick={handleOpenFolder}
                    disabled={isSaving || !settings.downloadPath}
                    title="Open download folder"
                  >
                    <FolderOpen className="h-4 w-4" />
                  </Button>
                </div>
              </div>

              {/* Cache Settings */}
              <div className="space-y-3 border-t pt-4">
                <label className="text-sm font-medium">Cache Settings (Requires Restart)</label>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <label className="text-xs text-muted-foreground">Max Cache Size (MB)</label>
                    <input
                      type="number"
                      min="50"
                      max="10000"
                      value={settings.cacheMaxSizeMB}
                      onChange={(e) =>
                        setSettings({ ...settings, cacheMaxSizeMB: parseInt(e.target.value) || 250 })
                      }
                      className="w-full px-3 py-2 border rounded-lg bg-background text-sm"
                    />
                  </div>
                  <div className="space-y-2">
                    <label className="text-xs text-muted-foreground">Cache TTL (days)</label>
                    <input
                      type="number"
                      min="1"
                      max="365"
                      value={settings.cacheTTLDays}
                      onChange={(e) =>
                        setSettings({ ...settings, cacheTTLDays: parseInt(e.target.value) || 30 })
                      }
                      className="w-full px-3 py-2 border rounded-lg bg-background text-sm"
                    />
                  </div>
                </div>
              </div>

              {/* Default Map Settings */}
              <div className="space-y-3 border-t pt-4">
                <label className="text-sm font-medium">Default Map Settings</label>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <label className="text-xs text-muted-foreground">Default Zoom Level</label>
                    <input
                      type="number"
                      min="1"
                      max="19"
                      value={settings.defaultZoom}
                      onChange={(e) =>
                        setSettings({ ...settings, defaultZoom: parseInt(e.target.value) || 10 })
                      }
                      className="w-full px-3 py-2 border rounded-lg bg-background text-sm"
                    />
                  </div>
                  <div className="space-y-2">
                    <label className="text-xs text-muted-foreground">Default Source</label>
                    <select
                      value={settings.defaultSource}
                      onChange={(e) => setSettings({ ...settings, defaultSource: e.target.value })}
                      className="w-full px-3 py-2 border rounded-lg bg-background text-sm"
                    >
                      <option value="esri">Esri Wayback</option>
                      <option value="google">Google Earth</option>
                      {settings.customSources.map((source) => (
                        <option key={source.name} value={source.name}>
                          {source.name}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>
              </div>

              {/* Default Download Settings */}
              <div className="space-y-3 border-t pt-4">
                <label className="text-sm font-medium">Default Download Settings</label>
                <div className="space-y-4">
                  <div className="space-y-2">
                     <label className="text-xs text-muted-foreground block">Download Zoom Strategy</label>
                     <div className="flex gap-4">
                       <label className="flex items-center gap-2 cursor-pointer">
                         <input
                           type="radio"
                           name="zoomStrategy"
                           checked={settings.downloadZoomStrategy === "current"}
                           onChange={() => setSettings({ ...settings, downloadZoomStrategy: "current" })}
                           className="w-4 h-4 text-primary border-border focus:ring-primary"
                         />
                         <span className="text-sm">Use Current Map Zoom</span>
                       </label>
                       <label className="flex items-center gap-2 cursor-pointer">
                         <input
                           type="radio"
                           name="zoomStrategy"
                           checked={settings.downloadZoomStrategy === "fixed" || !settings.downloadZoomStrategy}
                           onChange={() => setSettings({ ...settings, downloadZoomStrategy: "fixed" })}
                           className="w-4 h-4 text-primary border-border focus:ring-primary"
                         />
                         <span className="text-sm">Use Fixed Zoom Level</span>
                       </label>
                     </div>
                  </div>

                  {(settings.downloadZoomStrategy === "fixed" || !settings.downloadZoomStrategy) && (
                    <div className="space-y-2">
                      <div className="flex justify-between">
                        <label className="text-xs text-muted-foreground">Fixed Zoom Level</label>
                        <span className="text-xs font-mono">{settings.downloadFixedZoom || 19}</span>
                      </div>
                      <input
                        type="range"
                        min="1"
                        max="20"
                        step="1"
                        value={settings.downloadFixedZoom || 19}
                        onChange={(e) => setSettings({ ...settings, downloadFixedZoom: parseInt(e.target.value) })}
                        className="w-full h-2 bg-secondary rounded-lg appearance-none cursor-pointer accent-primary"
                      />
                      <p className="text-xs text-muted-foreground">
                        Always download imagery at this zoom level, regardless of map view.
                        Zoom 19 is high resolution (~30cm/pixel).
                      </p>
                    </div>
                  )}
                </div>
              </div>

              {/* UI Preferences */}
              <div className="space-y-3 border-t pt-4">
                <label className="text-sm font-medium">UI Preferences</label>
                <div className="space-y-2">
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={settings.autoOpenDownloadDir}
                      onChange={(e) =>
                        setSettings({ ...settings, autoOpenDownloadDir: e.target.checked })
                      }
                      className="w-4 h-4 rounded border-border accent-primary"
                    />
                    <span className="text-sm">Auto-open download folder after export</span>
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={settings.showTileGrid}
                      onChange={(e) => setSettings({ ...settings, showTileGrid: e.target.checked })}
                      className="w-4 h-4 rounded border-border accent-primary"
                    />
                    <span className="text-sm">Show tile grid overlay</span>
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={settings.showCoordinates}
                      onChange={(e) =>
                        setSettings({ ...settings, showCoordinates: e.target.checked })
                      }
                      className="w-4 h-4 rounded border-border accent-primary"
                    />
                    <span className="text-sm">Show coordinates overlay</span>
                  </label>
                </div>
              </div>

              {/* Appearance */}
              <div className="space-y-3 border-t pt-4">
                <label className="text-sm font-medium">Appearance</label>
                <div className="grid grid-cols-3 gap-3">
                  <button
                    onClick={() => {
                      setTheme("light");
                      if (settings) setSettings({ ...settings, theme: "light" });
                    }}
                    className={`flex items-center justify-center gap-2 p-3 rounded-lg border text-sm font-medium transition-all ${
                      theme === "light"
                        ? "border-primary bg-primary/5 text-primary ring-1 ring-primary"
                        : "hover:bg-muted/50"
                    }`}
                  >
                    <span className="w-4 h-4 rounded-full border bg-white" />
                    Light
                  </button>
                  <button
                    onClick={() => {
                      setTheme("dark");
                      if (settings) setSettings({ ...settings, theme: "dark" });
                    }}
                    className={`flex items-center justify-center gap-2 p-3 rounded-lg border text-sm font-medium transition-all ${
                      theme === "dark"
                        ? "border-primary bg-primary/5 text-primary ring-1 ring-primary"
                        : "hover:bg-muted/50"
                    }`}
                  >
                    <span className="w-4 h-4 rounded-full border bg-slate-950" />
                    Dark
                  </button>
                  <button
                    onClick={() => {
                      setTheme("system");
                      if (settings) setSettings({ ...settings, theme: "system" });
                    }}
                    className={`flex items-center justify-center gap-2 p-3 rounded-lg border text-sm font-medium transition-all ${
                      theme === "system"
                        ? "border-primary bg-primary/5 text-primary ring-1 ring-primary"
                        : "hover:bg-muted/50"
                    }`}
                  >
                    <span className="w-4 h-4 rounded-full border bg-linear-to-br from-white to-slate-950" />
                    System
                  </button>
                </div>
              </div>

            </>
          )}

          {activeTab === "sources" && (
            <>
              {/* Custom Sources List */}
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <label className="text-sm font-medium">Custom Imagery Sources</label>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setShowAddSource(!showAddSource)}
                  >
                    <Plus className="h-4 w-4 mr-2" />
                    Add Source
                  </Button>
                </div>

                {/* Add Source Form */}
                {showAddSource && (
                  <Card className="p-4 space-y-3 bg-muted/50">
                    <div className="space-y-2">
                      <label className="text-xs font-medium">Source Name</label>
                      <input
                        type="text"
                        value={newSourceName}
                        onChange={(e) => setNewSourceName(e.target.value)}
                        placeholder="e.g., Sentinel-2 Cloudless"
                        className="w-full px-3 py-2 border rounded-lg bg-background text-sm"
                      />
                    </div>
                    <div className="grid grid-cols-2 gap-3">
                      <div className="space-y-2">
                        <label className="text-xs font-medium">Type</label>
                        <select
                          value={newSourceType}
                          onChange={(e) =>
                            setNewSourceType(e.target.value as "wmts" | "wms" | "xyz" | "tms")
                          }
                          className="w-full px-3 py-2 border rounded-lg bg-background text-sm"
                        >
                          <option value="wmts">WMTS</option>
                          <option value="wms">WMS</option>
                          <option value="xyz">XYZ Tiles</option>
                          <option value="tms">TMS</option>
                        </select>
                      </div>
                      <div className="space-y-2">
                        <label className="text-xs font-medium">Attribution</label>
                        <input
                          type="text"
                          value={newSourceAttribution}
                          onChange={(e) => setNewSourceAttribution(e.target.value)}
                          placeholder="© Data Provider"
                          className="w-full px-3 py-2 border rounded-lg bg-background text-sm"
                        />
                      </div>
                    </div>
                    <div className="space-y-2">
                      <label className="text-xs font-medium">URL</label>
                      <input
                        type="text"
                        value={newSourceURL}
                        onChange={(e) => setNewSourceURL(e.target.value)}
                        placeholder="https://tiles.maps.eox.at/wmts/1.0.0/WMTSCapabilities.xml"
                        className="w-full px-3 py-2 border rounded-lg bg-background text-sm font-mono"
                      />
                      <p className="text-xs text-muted-foreground">
                        WMTS: Capabilities XML URL • XYZ/TMS: Tile template with {"{x}"}, {"{y}"},{" "}
                        {"{z}"}
                      </p>
                    </div>
                    <div className="flex gap-2">
                      <Button size="sm" onClick={handleAddCustomSource} className="flex-1">
                        <Plus className="h-4 w-4 mr-2" />
                        Add Source
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setShowAddSource(false)}
                      >
                        Cancel
                      </Button>
                    </div>
                  </Card>
                )}

                {/* Sources List */}
                <div className="space-y-2">
                  {settings.customSources.length === 0 ? (
                    <div className="p-8 text-center text-sm text-muted-foreground border rounded-lg">
                      <Globe className="h-12 w-12 mx-auto mb-3 opacity-50" />
                      <p>No custom sources added yet</p>
                      <p className="text-xs mt-1">
                        Add WMTS, WMS, or XYZ tile sources to extend imagery options
                      </p>
                    </div>
                  ) : (
                    settings.customSources.map((source) => (
                      <Card key={source.name} className="p-4">
                        <div className="flex items-start justify-between">
                          <div className="flex-1">
                            <div className="flex items-center gap-2">
                              <h4 className="font-medium">{source.name}</h4>
                              <span className="text-xs px-2 py-0.5 rounded bg-primary/10 text-primary font-mono">
                                {source.type.toUpperCase()}
                              </span>
                              {source.enabled && (
                                <span className="text-xs px-2 py-0.5 rounded bg-green-500/10 text-green-600">
                                  Active
                                </span>
                              )}
                            </div>
                            <p className="text-xs text-muted-foreground font-mono mt-1 break-all">
                              {source.url}
                            </p>
                            {source.attribution && (
                              <p className="text-xs text-muted-foreground mt-1">
                                {source.attribution}
                              </p>
                            )}
                          </div>
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => handleRemoveSource(source.name)}
                            className="text-destructive hover:text-destructive"
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      </Card>
                    ))
                  )}
                </div>
              </div>
            </>
          )}

          {activeTab === "dates" && (
            <>
              {/* Date Patterns List */}
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <label className="text-sm font-medium">Date Filter Patterns (Regex)</label>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setShowAddPattern(!showAddPattern)}
                  >
                    <Plus className="h-4 w-4 mr-2" />
                    Add Pattern
                  </Button>
                </div>

                <p className="text-xs text-muted-foreground">
                  Use regex patterns to filter available dates. Active patterns will be applied when
                  browsing imagery dates.
                </p>

                {/* Add Pattern Form */}
                {showAddPattern && (
                  <Card className="p-4 space-y-3 bg-muted/50">
                    <div className="space-y-2">
                      <label className="text-xs font-medium">Pattern Name</label>
                      <input
                        type="text"
                        value={newPatternName}
                        onChange={(e) => setNewPatternName(e.target.value)}
                        placeholder="e.g., Recent 5 Years"
                        className="w-full px-3 py-2 border rounded-lg bg-background text-sm"
                      />
                    </div>
                    <div className="space-y-2">
                      <label className="text-xs font-medium">Regex Pattern</label>
                      <input
                        type="text"
                        value={newPatternRegex}
                        onChange={(e) => setNewPatternRegex(e.target.value)}
                        placeholder="^20(2[0-9]|1[5-9])-"
                        className="w-full px-3 py-2 border rounded-lg bg-background text-sm font-mono"
                      />
                      <p className="text-xs text-muted-foreground">
                        Examples: ^202[0-9]- (2020s), ^[0-9]{"{4}"}-0[6-8]- (Summer months)
                      </p>
                    </div>
                    <div className="flex gap-2">
                      <Button size="sm" onClick={handleAddDatePattern} className="flex-1">
                        <Plus className="h-4 w-4 mr-2" />
                        Add Pattern
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setShowAddPattern(false)}
                      >
                        Cancel
                      </Button>
                    </div>
                  </Card>
                )}

                {/* Patterns List */}
                <div className="space-y-2">
                  {settings.dateFilterPatterns.length === 0 ? (
                    <div className="p-8 text-center text-sm text-muted-foreground border rounded-lg">
                      <FileCode className="h-12 w-12 mx-auto mb-3 opacity-50" />
                      <p>No date filter patterns defined</p>
                    </div>
                  ) : (
                    settings.dateFilterPatterns.map((pattern) => (
                      <Card key={pattern.name} className="p-4">
                        <div className="flex items-start justify-between">
                          <div className="flex-1">
                            <div className="flex items-center gap-2">
                              <h4 className="font-medium">{pattern.name}</h4>
                              {pattern.enabled && (
                                <span className="text-xs px-2 py-0.5 rounded bg-green-500/10 text-green-600">
                                  Active
                                </span>
                              )}
                            </div>
                            <p className="text-xs text-muted-foreground font-mono mt-1">
                              {pattern.pattern}
                            </p>
                          </div>
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => handleRemovePattern(pattern.name)}
                            className="text-destructive hover:text-destructive"
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      </Card>
                    ))
                  )}
                </div>
              </div>
            </>
          )}


          {activeTab === "about" && (
            <div className="flex flex-col items-center justify-center py-8 space-y-6 text-center">
              <div className="w-24 h-24 p-4 bg-muted/30 rounded-2xl border flex items-center justify-center">
                <img src={iconSvg} alt="Walkthru Earth" className="w-20 h-20" />
              </div>

              <div className="space-y-2">
                <h3 className="text-2xl font-bold tracking-tight">Walkthru Earth</h3>
                <p className="text-muted-foreground text-lg">Imagery Desktop</p>
              </div>

              <div className="py-2 px-4 rounded-full bg-muted border text-sm font-mono">
                v{appVersion}
              </div>

              <div className="max-w-md text-sm text-muted-foreground leading-relaxed">
                Advanced satellite imagery visualization and analysis tool. 
                Seamlessly download and process imagery from multiple providers.
              </div>

              <div className="pt-6 border-t w-full max-w-sm">
                <div className="flex justify-center gap-6 text-sm">
                  <button 
                    onClick={() => (window as any).runtime.BrowserOpenURL("mailto:hi@walkthru.earth")}
                    className="text-primary hover:underline flex items-center gap-2 cursor-pointer bg-transparent border-0 p-0"
                  >
                    <span className="w-1.5 h-1.5 rounded-full bg-green-500" />
                    hi@walkthru.earth
                  </button>
                  <span className="text-muted-foreground">|</span>
                  <button 
                    onClick={() => (window as any).runtime.BrowserOpenURL("https://walkthru.earth")}
                    className="text-muted-foreground hover:text-foreground transition-colors cursor-pointer bg-transparent border-0 p-0"
                  >
                    walkthru.earth
                  </button>
                </div>
                <p className="text-xs text-muted-foreground mt-6">
                  © {new Date().getFullYear()} Walkthru Earth. All rights reserved.
                </p>
              </div>
            </div>
          )}
        </CardContent>

        {/* Action Buttons */}
        <div className="flex gap-3 border-t p-6 shrink-0">
          <Button onClick={onClose} variant="outline" className="flex-1" disabled={isSaving}>
            Cancel
          </Button>
          <Button onClick={handleSave} className="flex-1" disabled={isLoading || isSaving}>
            <Save className="h-4 w-4 mr-2" />
            {isSaving ? "Saving..." : "Save Settings"}
          </Button>
        </div>
      </Card>
    </div>
  );
}
