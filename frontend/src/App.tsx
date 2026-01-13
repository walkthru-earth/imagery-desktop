import { useEffect, useState, useRef, useCallback, useMemo } from "react";
import maplibregl from "maplibre-gl";
import {
  GetTileInfo,
  GetEsriLayers,
  DownloadEsriImagery,
  DownloadEsriImageryRange,
  DownloadGoogleEarthImagery,
  DownloadGoogleEarthHistoricalImagery,
  DownloadGoogleEarthHistoricalImageryRange,
  SelectDownloadFolder,
  GetDownloadPath,
  OpenDownloadFolder,
  GetEsriTileURL,
  GetGoogleEarthTileURL,
  GetGoogleEarthDatesForArea,
  GetGoogleEarthHistoricalTileURL,
} from "../wailsjs/go/main/App";
import { EventsOn } from "../wailsjs/runtime/runtime";
import { Button } from "@/components/ui/button";
import { Slider } from "@/components/ui/slider";
import { Progress } from "@/components/ui/progress";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  MapIcon,
  Download,
  FolderOpen,
  Trash2,
  Layers,
  Calendar,
  ZoomIn,
  CalendarRange,
  ScrollText,
  Copy,
  Check,
  Eye,
  EyeOff,
  Grid3X3,
  FileImage,
  Files,
} from "lucide-react";

interface BoundingBox {
  south: number;
  west: number;
  north: number;
  east: number;
}

interface TileInfo {
  tileCount: number;
  zoomLevel: number;
  resolution: number;
  estSizeMB: number;
}

interface DownloadProgress {
  downloaded: number;
  total: number;
  percent: number;
  status: string;
}

interface AvailableDate {
  date: string;
  source: string;
}

interface GEAvailableDate {
  date: string;
  epoch: number;
  hexDate: string;
}

function App() {
  const mapContainer = useRef<HTMLDivElement>(null);
  const map = useRef<maplibregl.Map | null>(null);
  const [mapLoaded, setMapLoaded] = useState(false);
  const [bbox, setBbox] = useState<BoundingBox | null>(null);
  const [zoom, setZoom] = useState(17);
  const [tileInfo, setTileInfo] = useState<TileInfo | null>(null);
  const [availableDates, setAvailableDates] = useState<AvailableDate[]>([]);
  const [dateRangeMode, setDateRangeMode] = useState(false);
  const [dateRangeIndex, setDateRangeIndex] = useState<[number, number]>([0, 0]);
  const [selectedDateIndex, setSelectedDateIndex] = useState(0);
  const [isDrawing, setIsDrawing] = useState(false);
  const [drawStart, setDrawStart] = useState<maplibregl.LngLat | null>(null);
  const [downloadProgress, setDownloadProgress] =
    useState<DownloadProgress | null>(null);
  const [isDownloading, setIsDownloading] = useState(false);
  const [downloadPath, setDownloadPath] = useState("");
  const [activeSource, setActiveSource] = useState<"esri" | "google">("esri");
  const [logs, setLogs] = useState<string[]>([]);
  const [copied, setCopied] = useState(false);
  const [currentTileURL, setCurrentTileURL] = useState<string | null>(null);
  const [selectionMode, setSelectionMode] = useState<"draw" | "viewport">("viewport");

  // Google Earth historical dates
  const [geDates, setGeDates] = useState<GEAvailableDate[]>([]);
  const [geSelectedDateIndex, setGeSelectedDateIndex] = useState(0);
  const [geLoadingDates, setGeLoadingDates] = useState(false);
  const [geDateRangeMode, setGeDateRangeMode] = useState(false);
  const [geDateRangeIndex, setGeDateRangeIndex] = useState<[number, number]>([0, 0]);

  // Layer visibility and opacity controls
  const [osmVisible, setOsmVisible] = useState(true);
  const [osmOpacity, setOsmOpacity] = useState(100);
  const [imageryVisible, setImageryVisible] = useState(true);
  const [imageryOpacity, setImageryOpacity] = useState(100);

  // Download format: "tiles" = individual tiles only, "geotiff" = merged GeoTIFF only, "both" = keep both
  const [downloadFormat, setDownloadFormat] = useState<"tiles" | "geotiff" | "both">("geotiff");

  // Get selected dates based on mode
  const selectedDates = useMemo(() => {
    if (availableDates.length === 0) return [];
    if (dateRangeMode) {
      const [start, end] = dateRangeIndex;
      return availableDates.slice(Math.min(start, end), Math.max(start, end) + 1);
    }
    return [availableDates[selectedDateIndex]];
  }, [availableDates, dateRangeMode, dateRangeIndex, selectedDateIndex]);

  // Get selected GE dates based on mode
  const selectedGeDates = useMemo(() => {
    if (geDates.length === 0) return [];
    if (geDateRangeMode) {
      const [start, end] = geDateRangeIndex;
      return geDates.slice(Math.min(start, end), Math.max(start, end) + 1);
    }
    return [geDates[geSelectedDateIndex]];
  }, [geDates, geDateRangeMode, geDateRangeIndex, geSelectedDateIndex]);

  // Add log entry
  const addLog = useCallback((message: string) => {
    const timestamp = new Date().toLocaleTimeString();
    setLogs((prev) => [...prev, `[${timestamp}] ${message}`]);
  }, []);

  // Copy logs to clipboard
  const copyLogs = useCallback(async () => {
    const text = logs.join("\n");
    await navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [logs]);

  // Initialize map
  useEffect(() => {
    if (!mapContainer.current || map.current) return;

    const mapInstance = new maplibregl.Map({
      container: mapContainer.current,
      style: {
        version: 8,
        sources: {
          osm: {
            type: "raster",
            tiles: ["https://tile.openstreetmap.org/{z}/{x}/{y}.png"],
            tileSize: 256,
            attribution: "&copy; OpenStreetMap contributors",
          },
        },
        layers: [
          {
            id: "osm",
            type: "raster",
            source: "osm",
          },
        ],
      },
      center: [31.2357, 30.0444],
      zoom: 10,
    });

    map.current = mapInstance;

    mapInstance.addControl(new maplibregl.NavigationControl(), "top-right");

    // Sync map zoom to slider
    mapInstance.on("zoomend", () => {
      const mapZoom = Math.round(mapInstance.getZoom());
      setZoom(Math.max(10, Math.min(21, mapZoom)));
    });

    // Add bbox rectangle source and layer after load
    mapInstance.on("load", () => {
      mapInstance.addSource("bbox", {
        type: "geojson",
        data: {
          type: "Feature",
          properties: {},
          geometry: {
            type: "Polygon",
            coordinates: [[]],
          },
        },
      });

      mapInstance.addLayer({
        id: "bbox-fill",
        type: "fill",
        source: "bbox",
        paint: {
          "fill-color": "#3b82f6",
          "fill-opacity": 0.2,
        },
      });

      mapInstance.addLayer({
        id: "bbox-line",
        type: "line",
        source: "bbox",
        paint: {
          "line-color": "#3b82f6",
          "line-width": 2,
        },
      });

      setMapLoaded(true);
      addLog("Map initialized");
    });

    return () => {
      mapInstance.remove();
      map.current = null;
    };
  }, [addLog]);

  // Update bbox on map (defined before effects that use it)
  const updateBboxOnMap = useCallback((bounds: BoundingBox) => {
    if (!map.current || !mapLoaded) return;
    const source = map.current.getSource("bbox") as maplibregl.GeoJSONSource;
    if (!source) return;

    source.setData({
      type: "Feature",
      properties: {},
      geometry: {
        type: "Polygon",
        coordinates: [
          [
            [bounds.west, bounds.south],
            [bounds.east, bounds.south],
            [bounds.east, bounds.north],
            [bounds.west, bounds.north],
            [bounds.west, bounds.south],
          ],
        ],
      },
    });
  }, [mapLoaded]);

  // Handle mouse events for drawing (only when in draw mode)
  useEffect(() => {
    if (!map.current || !mapLoaded) return;
    if (selectionMode !== "draw") return;

    const mapInstance = map.current;

    const handleMouseDown = (e: maplibregl.MapMouseEvent) => {
      if (e.originalEvent.button !== 0) return;
      // Disable map dragging while drawing
      mapInstance.dragPan.disable();
      setIsDrawing(true);
      setDrawStart(e.lngLat);
    };

    const handleMouseMove = (e: maplibregl.MapMouseEvent) => {
      if (!isDrawing || !drawStart) return;

      const bounds: BoundingBox = {
        south: Math.min(drawStart.lat, e.lngLat.lat),
        north: Math.max(drawStart.lat, e.lngLat.lat),
        west: Math.min(drawStart.lng, e.lngLat.lng),
        east: Math.max(drawStart.lng, e.lngLat.lng),
      };

      updateBboxOnMap(bounds);
    };

    const handleMouseUp = (e: maplibregl.MapMouseEvent) => {
      // Re-enable map dragging
      mapInstance.dragPan.enable();

      if (!isDrawing || !drawStart) return;

      const bounds: BoundingBox = {
        south: Math.min(drawStart.lat, e.lngLat.lat),
        north: Math.max(drawStart.lat, e.lngLat.lat),
        west: Math.min(drawStart.lng, e.lngLat.lng),
        east: Math.max(drawStart.lng, e.lngLat.lng),
      };

      setBbox(bounds);
      setIsDrawing(false);
      setDrawStart(null);
      addLog(`Area selected: ${bounds.north.toFixed(4)}, ${bounds.west.toFixed(4)} to ${bounds.south.toFixed(4)}, ${bounds.east.toFixed(4)}`);
    };

    mapInstance.on("mousedown", handleMouseDown);
    mapInstance.on("mousemove", handleMouseMove);
    mapInstance.on("mouseup", handleMouseUp);

    return () => {
      mapInstance.off("mousedown", handleMouseDown);
      mapInstance.off("mousemove", handleMouseMove);
      mapInstance.off("mouseup", handleMouseUp);
    };
  }, [mapLoaded, isDrawing, drawStart, selectionMode, addLog, updateBboxOnMap]);

  // Update bbox from viewport when in viewport mode (no visual overlay)
  useEffect(() => {
    if (!map.current || !mapLoaded || selectionMode !== "viewport") return;

    const mapInstance = map.current;

    const updateBboxFromViewport = () => {
      const bounds = mapInstance.getBounds();
      const newBbox: BoundingBox = {
        south: bounds.getSouth(),
        west: bounds.getWest(),
        north: bounds.getNorth(),
        east: bounds.getEast(),
      };
      setBbox(newBbox);
      // Don't show visual overlay in viewport mode - bbox is the whole canvas
    };

    // Clear any existing bbox overlay when switching to viewport mode
    const source = mapInstance.getSource("bbox") as maplibregl.GeoJSONSource;
    if (source) {
      source.setData({
        type: "Feature",
        properties: {},
        geometry: {
          type: "Polygon",
          coordinates: [[]],
        },
      });
    }

    // Initial update
    updateBboxFromViewport();

    // Update on map move
    mapInstance.on("moveend", updateBboxFromViewport);

    return () => {
      mapInstance.off("moveend", updateBboxFromViewport);
    };
  }, [mapLoaded, selectionMode]);

  // Update tile info when bbox or zoom changes
  useEffect(() => {
    if (!bbox) {
      setTileInfo(null);
      return;
    }

    // Only show bbox overlay in draw mode
    if (selectionMode === "draw") {
      updateBboxOnMap(bbox);
    }
    GetTileInfo(bbox, zoom).then(setTileInfo);
  }, [bbox, zoom, updateBboxOnMap, selectionMode]);

  // Update imagery preview layer when date changes
  // Update imagery preview layer when date changes or source changes
  useEffect(() => {
    if (!map.current || !mapLoaded) return;
    
    // Cleanup function to remove existing layer if source changes
    const cleanupLayer = () => {
      const mapInstance = map.current!;
      if (mapInstance.getLayer("imagery-layer")) {
        mapInstance.removeLayer("imagery-layer");
      }
      if (mapInstance.getSource("imagery-source")) {
        mapInstance.removeSource("imagery-source");
      }
    };

    if (activeSource === "esri") {
      if (availableDates.length === 0) return;
      const selectedDate = availableDates[selectedDateIndex]?.date;
      if (!selectedDate) return;

      GetEsriTileURL(selectedDate).then((tileURL) => {
        if (!map.current) return;
        cleanupLayer();

        const mapInstance = map.current;
        setCurrentTileURL(tileURL);
        addLog(`Loading Esri imagery for ${selectedDate}`);

        mapInstance.addSource("imagery-source", {
          type: "raster",
          tiles: [tileURL],
          tileSize: 256,
          attribution: "&copy; Esri World Imagery Wayback",
        });

        mapInstance.addLayer({
          id: "imagery-layer",
          type: "raster",
          source: "imagery-source",
          paint: { "raster-opacity": 1 },
        }, "bbox-fill"); // Insert below the bbox layer

        addLog(`Esri layer loaded`);
      }).catch(err => addLog(`Failed to load Esri imagery: ${err}`));

    } else if (activeSource === "google") {
      // Use historical tile URL if a date is selected, otherwise use current imagery
      const loadGoogleEarthLayer = async () => {
        if (!map.current) return;
        cleanupLayer();

        const mapInstance = map.current;
        let tileURL: string;
        let dateLabel: string;

        if (geDates.length > 0 && geSelectedDateIndex < geDates.length) {
          const selectedGEDate = geDates[geSelectedDateIndex];
          tileURL = await GetGoogleEarthHistoricalTileURL(selectedGEDate.hexDate, selectedGEDate.epoch);
          dateLabel = selectedGEDate.date;
        } else {
          tileURL = await GetGoogleEarthTileURL();
          dateLabel = "current";
        }

        setCurrentTileURL(tileURL);
        addLog(`Loading Google Earth imagery (${dateLabel})`);

        mapInstance.addSource("imagery-source", {
          type: "raster",
          tiles: [tileURL],
          tileSize: 256,
          attribution: "Google Earth",
        });

        mapInstance.addLayer({
          id: "imagery-layer",
          type: "raster",
          source: "imagery-source",
          paint: { "raster-opacity": 1 },
        }, "bbox-fill");

        addLog(`Google Earth layer loaded`);
      };

      loadGoogleEarthLayer().catch(err => addLog(`Failed to load Google Earth: ${err}`));
    }

  }, [mapLoaded, selectedDateIndex, availableDates, activeSource, geDates, geSelectedDateIndex, addLog]);

  // Load available Esri dates
  useEffect(() => {
    GetEsriLayers().then((dates) => {
      if (dates && dates.length > 0) {
        setAvailableDates(dates);
        setSelectedDateIndex(0);
        setDateRangeIndex([0, Math.min(4, dates.length - 1)]);
      }
    });
  }, []);

  // Load Google Earth dates when source is Google and bbox is available
  useEffect(() => {
    if (activeSource !== "google" || !bbox) {
      return;
    }

    setGeLoadingDates(true);
    addLog("Fetching Google Earth historical dates...");

    GetGoogleEarthDatesForArea(bbox, zoom)
      .then((dates) => {
        if (dates && dates.length > 0) {
          setGeDates(dates);
          setGeSelectedDateIndex(0);
          setGeDateRangeIndex([0, Math.min(4, dates.length - 1)]);
          addLog(`Found ${dates.length} Google Earth historical dates`);
        } else {
          setGeDates([]);
          addLog("No historical dates found for this area");
        }
      })
      .catch((err) => {
        addLog(`Failed to fetch GE dates: ${err}`);
        setGeDates([]);
      })
      .finally(() => {
        setGeLoadingDates(false);
      });
  }, [activeSource, bbox, zoom, addLog]);

  // Load download path
  useEffect(() => {
    GetDownloadPath().then(setDownloadPath);
  }, []);

  // Subscribe to download progress events
  useEffect(() => {
    const unsubscribe = EventsOn("download-progress", (progress: DownloadProgress) => {
      setDownloadProgress(progress);
      if (progress.percent >= 100) {
        setIsDownloading(false);
      }
    });

    return () => {
      unsubscribe();
    };
  }, []);

  // Subscribe to backend log events
  useEffect(() => {
    const unsubscribe = EventsOn("log", (message: string) => {
      addLog(message);
    });

    return () => {
      unsubscribe();
    };
  }, [addLog]);

  // Apply OSM layer visibility and opacity changes
  useEffect(() => {
    if (!map.current || !mapLoaded) return;
    const mapInstance = map.current;

    if (mapInstance.getLayer("osm")) {
      mapInstance.setLayoutProperty("osm", "visibility", osmVisible ? "visible" : "none");
      mapInstance.setPaintProperty("osm", "raster-opacity", osmOpacity / 100);
    }
  }, [mapLoaded, osmVisible, osmOpacity]);

  // Apply imagery layer visibility and opacity changes
  useEffect(() => {
    if (!map.current || !mapLoaded) return;
    const mapInstance = map.current;

    // Small delay to ensure layer is added before applying properties
    const applyStyles = () => {
      if (mapInstance.getLayer("imagery-layer")) {
        mapInstance.setLayoutProperty("imagery-layer", "visibility", imageryVisible ? "visible" : "none");
        mapInstance.setPaintProperty("imagery-layer", "raster-opacity", imageryOpacity / 100);
      }
    };

    // Apply immediately and also after a short delay for newly created layers
    applyStyles();
    const timer = setTimeout(applyStyles, 100);
    return () => clearTimeout(timer);
  }, [mapLoaded, imageryVisible, imageryOpacity, currentTileURL]);

  const handleClearSelection = () => {
    setBbox(null);
    if (map.current && mapLoaded) {
      const source = map.current.getSource("bbox") as maplibregl.GeoJSONSource;
      if (source) {
        source.setData({
          type: "Feature",
          properties: {},
          geometry: {
            type: "Polygon",
            coordinates: [[]],
          },
        });
      }
    }
  };

  const handleSelectFolder = async () => {
    const path = await SelectDownloadFolder();
    if (path) {
      setDownloadPath(path);
    }
  };

  const handleDownload = async () => {
    // For Esri, check selectedDates; for Google, either dates or current
    if (!bbox) return;
    if (activeSource === "esri" && selectedDates.length === 0) return;

    setIsDownloading(true);
    setDownloadProgress({ downloaded: 0, total: 0, percent: 0, status: "Starting..." });

    try {
      if (activeSource === "google") {
        if (geDates.length > 0) {
          if (geDateRangeMode && selectedGeDates.length > 1) {
            // Bulk download for date range
            const dateInfos = selectedGeDates.map(d => ({
              date: d.date,
              hexDate: d.hexDate,
              epoch: d.epoch,
            }));
            await DownloadGoogleEarthHistoricalImageryRange(bbox, zoom, dateInfos, downloadFormat);
          } else {
            // Single date download
            const selectedGEDate = selectedGeDates[0];
            await DownloadGoogleEarthHistoricalImagery(
              bbox,
              zoom,
              selectedGEDate.hexDate,
              selectedGEDate.epoch,
              selectedGEDate.date,
              downloadFormat
            );
          }
        } else {
          // Download current imagery
          await DownloadGoogleEarthImagery(bbox, zoom, downloadFormat);
        }
      } else {
        if (dateRangeMode && selectedDates.length > 1) {
          // Bulk download for date range
          const dates = selectedDates.map(d => d.date);
          await DownloadEsriImageryRange(bbox, zoom, dates, downloadFormat);
        } else {
          // Single date download
          await DownloadEsriImagery(bbox, zoom, selectedDates[0].date, downloadFormat);
        }
      }
    } catch (error) {
      console.error("Download failed:", error);
      setIsDownloading(false);
    }
  };

  const formatResolution = (meters: number) => {
    if (meters < 1) {
      return `${(meters * 100).toFixed(1)} cm/px`;
    }
    return `${meters.toFixed(2)} m/px`;
  };

  const formatDateLabel = (index: number) => {
    if (index >= 0 && index < availableDates.length) {
      return availableDates[index].date;
    }
    return "";
  };

  // Group dates by year for timeline visualization
  const datesByYear = useMemo(() => {
    const years: Map<number, { startIndex: number; endIndex: number; count: number }> = new Map();
    availableDates.forEach((d, i) => {
      const year = parseInt(d.date.split("-")[0]);
      if (!years.has(year)) {
        years.set(year, { startIndex: i, endIndex: i, count: 1 });
      } else {
        const entry = years.get(year)!;
        entry.endIndex = i;
        entry.count++;
      }
    });
    return Array.from(years.entries()).sort((a, b) => b[0] - a[0]); // newest first
  }, [availableDates]);

  // Group GE dates by year for timeline visualization
  const geDatesByYear = useMemo(() => {
    const years: Map<number, { startIndex: number; endIndex: number; count: number }> = new Map();
    geDates.forEach((d, i) => {
      const year = parseInt(d.date.split("-")[0]);
      if (!years.has(year)) {
        years.set(year, { startIndex: i, endIndex: i, count: 1 });
      } else {
        const entry = years.get(year)!;
        entry.endIndex = i;
        entry.count++;
      }
    });
    return Array.from(years.entries()).sort((a, b) => b[0] - a[0]); // newest first
  }, [geDates]);

  return (
    <div className="flex h-screen bg-background">
      {/* Sidebar */}
      <aside className="w-80 border-r flex flex-col">
        <div className="p-4 border-b">
          <h1 className="text-lg font-semibold flex items-center gap-2">
            <MapIcon className="h-5 w-5" />
            Imagery Downloader
          </h1>
          <p className="text-sm text-muted-foreground">
            Download georeferenced satellite imagery
          </p>
        </div>

        <div className="flex-1 overflow-y-auto p-4 space-y-4">
          {/* Source Selection */}
          <Tabs value={activeSource} onValueChange={(v) => setActiveSource(v as "esri" | "google")}>
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="esri">Esri Wayback</TabsTrigger>
              <TabsTrigger value="google">Google Earth</TabsTrigger>
            </TabsList>
          </Tabs>

          {/* Layer Controls */}
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm flex items-center gap-2">
                <Layers className="h-4 w-4" />
                Layer Controls
              </CardTitle>
              <CardDescription>Toggle visibility and adjust opacity</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {/* OSM Base Layer */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <button
                      onClick={() => setOsmVisible(!osmVisible)}
                      className="p-1 hover:bg-muted rounded"
                      title={osmVisible ? "Hide OSM" : "Show OSM"}
                    >
                      {osmVisible ? <Eye className="h-4 w-4" /> : <EyeOff className="h-4 w-4 text-muted-foreground" />}
                    </button>
                    <Label className="text-xs">OSM Basemap</Label>
                  </div>
                  <span className="text-xs text-muted-foreground">{osmOpacity}%</span>
                </div>
                <Slider
                  value={[osmOpacity]}
                  onValueChange={([v]) => setOsmOpacity(v)}
                  min={0}
                  max={100}
                  step={5}
                  disabled={!osmVisible}
                />
              </div>

              {/* Imagery Layer */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <button
                      onClick={() => setImageryVisible(!imageryVisible)}
                      className="p-1 hover:bg-muted rounded"
                      title={imageryVisible ? "Hide Imagery" : "Show Imagery"}
                    >
                      {imageryVisible ? <Eye className="h-4 w-4" /> : <EyeOff className="h-4 w-4 text-muted-foreground" />}
                    </button>
                    <Label className="text-xs">
                      {activeSource === "esri" ? "Esri Imagery" : "Google Earth"}
                    </Label>
                  </div>
                  <span className="text-xs text-muted-foreground">{imageryOpacity}%</span>
                </div>
                <Slider
                  value={[imageryOpacity]}
                  onValueChange={([v]) => setImageryOpacity(v)}
                  min={0}
                  max={100}
                  step={5}
                  disabled={!imageryVisible}
                />
              </div>
            </CardContent>
          </Card>

          <Separator />

          {/* Selection Mode & Bounding Box Info */}
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm flex items-center gap-2">
                <Layers className="h-4 w-4" />
                Selection
              </CardTitle>
              <CardDescription className="flex items-center justify-between">
                <span>{selectionMode === "viewport" ? "Using map canvas" : "Draw custom area"}</span>
                <div className="flex items-center gap-2">
                  <Label htmlFor="selection-mode" className="text-xs">Draw</Label>
                  <Switch
                    id="selection-mode"
                    checked={selectionMode === "draw"}
                    onCheckedChange={(checked) => setSelectionMode(checked ? "draw" : "viewport")}
                  />
                </div>
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-2">
              {bbox && (
                <div className="text-xs space-y-1 font-mono">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">North:</span>
                    <span>{bbox.north.toFixed(6)}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">South:</span>
                    <span>{bbox.south.toFixed(6)}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">East:</span>
                    <span>{bbox.east.toFixed(6)}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">West:</span>
                    <span>{bbox.west.toFixed(6)}</span>
                  </div>
                </div>
              )}
              {selectionMode === "draw" && (
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full"
                  onClick={handleClearSelection}
                  disabled={!bbox}
                >
                  <Trash2 className="h-4 w-4 mr-2" />
                  Clear Selection
                </Button>
              )}
            </CardContent>
          </Card>

          {/* Zoom Level */}
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm flex items-center gap-2">
                <ZoomIn className="h-4 w-4" />
                Zoom Level: {zoom}
              </CardTitle>
              <CardDescription>
                {tileInfo && formatResolution(tileInfo.resolution)}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Slider
                value={[zoom]}
                onValueChange={([v]) => setZoom(v)}
                min={10}
                max={21}
                step={1}
              />
              {tileInfo && (
                <div className="mt-2 text-xs space-y-1">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Tiles:</span>
                    <span>{tileInfo.tileCount.toLocaleString()}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Est. Size:</span>
                    <span>{tileInfo.estSizeMB.toFixed(1)} MB</span>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>

          {/* Date Selection with Slider */}
          {activeSource === "esri" && availableDates.length > 0 && (
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm flex items-center gap-2">
                  {dateRangeMode ? (
                    <CalendarRange className="h-4 w-4" />
                  ) : (
                    <Calendar className="h-4 w-4" />
                  )}
                  Historical Imagery
                </CardTitle>
                <CardDescription className="flex items-center justify-between">
                  <span>{availableDates.length} dates from {datesByYear.length > 0 ? datesByYear[datesByYear.length - 1][0] : ""} to {datesByYear.length > 0 ? datesByYear[0][0] : ""}</span>
                  <div className="flex items-center gap-2">
                    <Label htmlFor="range-mode" className="text-xs">Range</Label>
                    <Switch
                      id="range-mode"
                      checked={dateRangeMode}
                      onCheckedChange={setDateRangeMode}
                    />
                  </div>
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                {dateRangeMode ? (
                  <>
                    {/* Selected range display */}
                    <div className="text-center py-2 bg-muted rounded-lg">
                      <p className="text-lg font-semibold tabular-nums">
                        {formatDateLabel(Math.min(...dateRangeIndex))} — {formatDateLabel(Math.max(...dateRangeIndex))}
                      </p>
                      <p className="text-xs text-muted-foreground mt-1">
                        {selectedDates.length} dates selected
                      </p>
                    </div>

                    {/* Date Range Slider */}
                    <div className="space-y-2">
                      <Slider
                        value={dateRangeIndex}
                        onValueChange={(v) => setDateRangeIndex(v as [number, number])}
                        min={0}
                        max={availableDates.length - 1}
                        step={1}
                      />
                      <div className="flex justify-between text-xs text-muted-foreground">
                        <span>{datesByYear.length > 0 ? datesByYear[0][0] : ""}</span>
                        <span>{datesByYear.length > 0 ? datesByYear[datesByYear.length - 1][0] : ""}</span>
                      </div>
                    </div>
                  </>
                ) : (
                  <>
                    {/* Selected date display */}
                    <div className="text-center py-2 bg-muted rounded-lg">
                      <p className="text-2xl font-semibold tabular-nums">
                        {formatDateLabel(selectedDateIndex)}
                      </p>
                      <p className="text-xs text-muted-foreground mt-1">
                        {selectedDateIndex + 1} of {availableDates.length}
                      </p>
                    </div>

                    {/* Single Date Slider */}
                    <div className="space-y-2">
                      <Slider
                        value={[selectedDateIndex]}
                        onValueChange={([v]) => setSelectedDateIndex(v)}
                        min={0}
                        max={availableDates.length - 1}
                        step={1}
                      />
                      <div className="flex justify-between text-xs text-muted-foreground">
                        <span>{datesByYear.length > 0 ? datesByYear[0][0] : ""}</span>
                        <span>{datesByYear.length > 0 ? datesByYear[datesByYear.length - 1][0] : ""}</span>
                      </div>
                    </div>
                  </>
                )}

                {/* Year quick-jump buttons */}
                <div className="space-y-2">
                  <p className="text-xs text-muted-foreground">Jump to year:</p>
                  <div className="flex flex-wrap gap-1">
                    {datesByYear.map(([year, info]) => {
                      const isSelected = dateRangeMode
                        ? (info.startIndex <= Math.max(...dateRangeIndex) && info.endIndex >= Math.min(...dateRangeIndex))
                        : (selectedDateIndex >= info.startIndex && selectedDateIndex <= info.endIndex);
                      return (
                        <button
                          key={year}
                          className={`px-2 py-1 text-xs rounded transition-colors ${
                            isSelected
                              ? "bg-primary text-primary-foreground"
                              : "bg-muted hover:bg-muted-foreground/20"
                          }`}
                          onClick={() => {
                            if (dateRangeMode) {
                              setDateRangeIndex([info.startIndex, info.endIndex]);
                            } else {
                              setSelectedDateIndex(info.startIndex);
                            }
                          }}
                        >
                          {year}
                        </button>
                      );
                    })}
                  </div>
                </div>
              </CardContent>
            </Card>
          )}

          {/* Google Earth Date Selection */}
          {activeSource === "google" && (
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm flex items-center gap-2">
                  {geDateRangeMode ? (
                    <CalendarRange className="h-4 w-4" />
                  ) : (
                    <Calendar className="h-4 w-4" />
                  )}
                  Historical Imagery
                </CardTitle>
                {geDates.length > 0 && (
                  <CardDescription className="flex items-center justify-between">
                    <span>{geDates.length} dates from {geDatesByYear.length > 0 ? geDatesByYear[geDatesByYear.length - 1][0] : ""} to {geDatesByYear.length > 0 ? geDatesByYear[0][0] : ""}</span>
                    <div className="flex items-center gap-2">
                      <Label htmlFor="ge-range-mode" className="text-xs">Range</Label>
                      <Switch
                        id="ge-range-mode"
                        checked={geDateRangeMode}
                        onCheckedChange={setGeDateRangeMode}
                      />
                    </div>
                  </CardDescription>
                )}
              </CardHeader>
              <CardContent className="space-y-4">
                {geLoadingDates ? (
                  <div className="text-center py-4 text-muted-foreground text-sm">
                    Loading historical dates...
                  </div>
                ) : geDates.length > 0 ? (
                  <>
                    {geDateRangeMode ? (
                      <>
                        {/* Selected range display */}
                        <div className="text-center py-2 bg-muted rounded-lg">
                          <p className="text-lg font-semibold tabular-nums">
                            {geDates[Math.min(...geDateRangeIndex)]?.date} — {geDates[Math.max(...geDateRangeIndex)]?.date}
                          </p>
                          <p className="text-xs text-muted-foreground mt-1">
                            {selectedGeDates.length} dates selected
                          </p>
                        </div>

                        {/* Date Range Slider */}
                        <div className="space-y-2">
                          <Slider
                            value={geDateRangeIndex}
                            onValueChange={(v) => setGeDateRangeIndex(v as [number, number])}
                            min={0}
                            max={geDates.length - 1}
                            step={1}
                          />
                          <div className="flex justify-between text-xs text-muted-foreground">
                            <span>{geDatesByYear.length > 0 ? geDatesByYear[0][0] : ""}</span>
                            <span>{geDatesByYear.length > 0 ? geDatesByYear[geDatesByYear.length - 1][0] : ""}</span>
                          </div>
                        </div>
                      </>
                    ) : (
                      <>
                        {/* Selected date display */}
                        <div className="text-center py-2 bg-muted rounded-lg">
                          <p className="text-2xl font-semibold tabular-nums">
                            {geDates[geSelectedDateIndex]?.date || ""}
                          </p>
                          <p className="text-xs text-muted-foreground mt-1">
                            {geSelectedDateIndex + 1} of {geDates.length}
                          </p>
                        </div>

                        {/* Timeline slider */}
                        <div className="space-y-2">
                          <Slider
                            value={[geSelectedDateIndex]}
                            onValueChange={([v]) => setGeSelectedDateIndex(v)}
                            min={0}
                            max={geDates.length - 1}
                            step={1}
                          />
                          <div className="flex justify-between text-xs text-muted-foreground">
                            <span>{geDatesByYear.length > 0 ? geDatesByYear[0][0] : ""}</span>
                            <span>{geDatesByYear.length > 0 ? geDatesByYear[geDatesByYear.length - 1][0] : ""}</span>
                          </div>
                        </div>
                      </>
                    )}

                    {/* Year quick-jump buttons */}
                    <div className="space-y-2">
                      <p className="text-xs text-muted-foreground">Jump to year:</p>
                      <div className="flex flex-wrap gap-1">
                        {geDatesByYear.map(([year, info]) => {
                          const isSelected = geDateRangeMode
                            ? (info.startIndex <= Math.max(...geDateRangeIndex) && info.endIndex >= Math.min(...geDateRangeIndex))
                            : (geSelectedDateIndex >= info.startIndex && geSelectedDateIndex <= info.endIndex);
                          return (
                            <button
                              key={year}
                              className={`px-2 py-1 text-xs rounded transition-colors ${
                                isSelected
                                  ? "bg-primary text-primary-foreground"
                                  : "bg-muted hover:bg-muted-foreground/20"
                              }`}
                              onClick={() => {
                                if (geDateRangeMode) {
                                  setGeDateRangeIndex([info.startIndex, info.endIndex]);
                                } else {
                                  setGeSelectedDateIndex(info.startIndex);
                                }
                              }}
                            >
                              {year}
                            </button>
                          );
                        })}
                      </div>
                    </div>
                  </>
                ) : (
                  <div className="text-center py-4 text-muted-foreground text-sm">
                    {bbox
                      ? "No historical dates found for this area"
                      : "Pan/zoom map to load dates"}
                  </div>
                )}
              </CardContent>
            </Card>
          )}

          {/* Download Path */}
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm flex items-center gap-2">
                <FolderOpen className="h-4 w-4" />
                Download Location
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <p className="text-xs font-mono truncate" title={downloadPath}>
                {downloadPath || "Not set"}
              </p>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  className="flex-1"
                  onClick={handleSelectFolder}
                >
                  Change
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => OpenDownloadFolder()}
                >
                  Open
                </Button>
              </div>

              {/* Download Format Toggle */}
              <div className="space-y-2 pt-2 border-t">
                <Label className="text-xs text-muted-foreground">Output Format</Label>
                <div className="flex gap-1">
                  <button
                    onClick={() => setDownloadFormat("tiles")}
                    className={`flex-1 flex items-center justify-center gap-1.5 px-2 py-1.5 text-xs rounded transition-colors ${
                      downloadFormat === "tiles"
                        ? "bg-primary text-primary-foreground"
                        : "bg-muted hover:bg-muted-foreground/20"
                    }`}
                    title="Keep individual tiles only"
                  >
                    <Grid3X3 className="h-3 w-3" />
                    Tiles
                  </button>
                  <button
                    onClick={() => setDownloadFormat("geotiff")}
                    className={`flex-1 flex items-center justify-center gap-1.5 px-2 py-1.5 text-xs rounded transition-colors ${
                      downloadFormat === "geotiff"
                        ? "bg-primary text-primary-foreground"
                        : "bg-muted hover:bg-muted-foreground/20"
                    }`}
                    title="Merged GeoTIFF only"
                  >
                    <FileImage className="h-3 w-3" />
                    GeoTIFF
                  </button>
                  <button
                    onClick={() => setDownloadFormat("both")}
                    className={`flex-1 flex items-center justify-center gap-1.5 px-2 py-1.5 text-xs rounded transition-colors ${
                      downloadFormat === "both"
                        ? "bg-primary text-primary-foreground"
                        : "bg-muted hover:bg-muted-foreground/20"
                    }`}
                    title="Keep both tiles and GeoTIFF"
                  >
                    <Files className="h-3 w-3" />
                    Both
                  </button>
                </div>
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Download Button & Progress */}
        <div className="p-4 border-t space-y-3">
          {downloadProgress && isDownloading && (
            <div className="space-y-2">
              <Progress value={downloadProgress.percent} />
              <p className="text-xs text-center text-muted-foreground">
                {downloadProgress.status}
              </p>
            </div>
          )}
          <Button
            className="w-full"
            size="lg"
            onClick={handleDownload}
            disabled={!bbox || (activeSource === "esri" && selectedDates.length === 0) || isDownloading}
          >
            <Download className="h-4 w-4 mr-2" />
            {isDownloading
              ? "Downloading..."
              : activeSource === "google"
                ? geDates.length > 0
                  ? geDateRangeMode && selectedGeDates.length > 1
                    ? `Download ${selectedGeDates.length} GE Dates`
                    : `Download GE (${selectedGeDates[0]?.date})`
                  : "Download Google Earth"
                : dateRangeMode && selectedDates.length > 1
                  ? `Download ${selectedDates.length} Dates`
                  : "Download Esri Imagery"
            }
          </Button>

          {/* Logs Button */}
          <Dialog>
            <DialogTrigger asChild>
              <Button variant="outline" size="sm" className="w-full">
                <ScrollText className="h-4 w-4 mr-2" />
                View Logs ({logs.length})
              </Button>
            </DialogTrigger>
            <DialogContent className="max-w-2xl max-h-[80vh]">
              <DialogHeader>
                <DialogTitle>Application Logs</DialogTitle>
                <DialogDescription>
                  Activity log for this session
                </DialogDescription>
              </DialogHeader>
              <div className="space-y-2">
                <div className="bg-muted rounded p-3 max-h-[50vh] overflow-y-auto font-mono text-xs">
                  {logs.length === 0 ? (
                    <p className="text-muted-foreground">No logs yet</p>
                  ) : (
                    logs.map((log, i) => (
                      <div key={i} className="py-0.5">{log}</div>
                    ))
                  )}
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full"
                  onClick={copyLogs}
                >
                  {copied ? (
                    <>
                      <Check className="h-4 w-4 mr-2" />
                      Copied!
                    </>
                  ) : (
                    <>
                      <Copy className="h-4 w-4 mr-2" />
                      Copy to Clipboard
                    </>
                  )}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      </aside>

      {/* Map */}
      <main className="flex-1 relative">
        <div ref={mapContainer} className="absolute inset-0" />
        {selectionMode === "draw" && (
          <div className="absolute bottom-4 left-4 bg-background/90 px-3 py-1.5 rounded text-xs">
            Click and drag to select an area
          </div>
        )}
      </main>
    </div>
  );
}

export default App;
