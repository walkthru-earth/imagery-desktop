import { createContext, useContext, useReducer, ReactNode } from "react";
import type { AvailableDate, GEAvailableDate } from "@/types";

// ===================
// State Types
// ===================

export type ViewMode = "single" | "split";
export type ImagerySource = "esri" | "google";
export type MapKey = "single" | "left" | "right";

export interface MapState {
  source: ImagerySource;
  geDates: GEAvailableDate[];
  dateIndex: number;
  styleLoaded: boolean;
}

export interface ImageryState {
  viewMode: ViewMode;
  esriDates: AvailableDate[]; // Shared across all maps
  esriDatesLoading: boolean; // True when fetching viewport-based dates
  geDatesLoading: boolean; // True when fetching Google Earth dates
  maps: {
    single: MapState;
    left: MapState;
    right: MapState;
  };
  layers: {
    imagery: { visible: boolean; opacity: number };
    bbox: { visible: boolean; opacity: number };
  };
  // Shared map position (synced across all views)
  mapPosition: {
    center: [number, number]; // [lon, lat]
    zoom: number;
    isLoaded: boolean; // True once loaded from settings
  };
}

// ===================
// Actions
// ===================

export type ImageryAction =
  | { type: "SET_VIEW_MODE"; mode: ViewMode }
  | { type: "SET_ESRI_DATES"; dates: AvailableDate[] }
  | { type: "SET_ESRI_DATES_LOADING"; loading: boolean }
  | { type: "SET_MAP_SOURCE"; map: MapKey; source: ImagerySource }
  | { type: "UPDATE_GE_DATES"; map: MapKey; dates: GEAvailableDate[] }
  | { type: "SET_DATE_INDEX"; map: MapKey; index: number }
  | { type: "SET_STYLE_LOADED"; map: MapKey; loaded: boolean }
  | {
      type: "SET_LAYER_VISIBILITY";
      layer: "imagery" | "bbox";
      visible: boolean;
    }
  | { type: "SET_LAYER_OPACITY"; layer: "imagery" | "bbox"; opacity: number }
  | { type: "SET_MAP_POSITION"; center: [number, number]; zoom: number }
  | { type: "SET_GE_DATES_LOADING"; loading: boolean };

// ===================
// Initial State
// ===================

const initialMapState: MapState = {
  source: "esri",
  geDates: [],
  dateIndex: 0,
  styleLoaded: false,
};

const initialState: ImageryState = {
  viewMode: "single",
  esriDates: [],
  esriDatesLoading: false,
  geDatesLoading: false,
  maps: {
    single: { ...initialMapState },
    left: { ...initialMapState },
    right: { ...initialMapState, source: "google" },
  },
  layers: {
    imagery: { visible: true, opacity: 1 },
    bbox: { visible: false, opacity: 0.2 },
  },
  mapPosition: {
    center: [31.2219, 30.0621], // Zamalek, Cairo, Egypt [lon, lat]
    zoom: 15,
    isLoaded: false,
  },
};

// ===================
// Reducer
// ===================

function imageryReducer(
  state: ImageryState,
  action: ImageryAction
): ImageryState {
  console.log("[ImageryReducer] Action:", action.type, action);

  switch (action.type) {
    case "SET_VIEW_MODE":
      console.log("[ImageryReducer] Setting view mode to:", action.mode);
      return {
        ...state,
        viewMode: action.mode,
      };

    case "SET_ESRI_DATES":
      console.log("[ImageryReducer] Setting Esri dates, count:", action.dates.length);
      return {
        ...state,
        esriDates: action.dates,
      };

    case "SET_ESRI_DATES_LOADING":
      return {
        ...state,
        esriDatesLoading: action.loading,
      };

    case "SET_GE_DATES_LOADING":
      return {
        ...state,
        geDatesLoading: action.loading,
      };

    case "SET_MAP_SOURCE":
      console.log("[ImageryReducer] Setting map source:", action.map, "to", action.source);
      return {
        ...state,
        maps: {
          ...state.maps,
          [action.map]: {
            ...state.maps[action.map],
            source: action.source,
            dateIndex: 0, // Reset index when changing source
          },
        },
      };

    case "UPDATE_GE_DATES":
      console.log("[ImageryReducer] Updating GE dates for:", action.map, "count:", action.dates.length);
      // Keep the same index if possible, otherwise clamp to valid range
      const currentIndex = state.maps[action.map].dateIndex;
      const newIndex = Math.min(currentIndex, Math.max(0, action.dates.length - 1));
      return {
        ...state,
        maps: {
          ...state.maps,
          [action.map]: {
            ...state.maps[action.map],
            geDates: action.dates,
            dateIndex: newIndex, // Preserve index or clamp to valid range
          },
        },
      };

    case "SET_DATE_INDEX":
      console.log("[ImageryReducer] Setting date index for:", action.map, "to:", action.index);
      return {
        ...state,
        maps: {
          ...state.maps,
          [action.map]: {
            ...state.maps[action.map],
            dateIndex: action.index,
          },
        },
      };

    case "SET_STYLE_LOADED":
      return {
        ...state,
        maps: {
          ...state.maps,
          [action.map]: {
            ...state.maps[action.map],
            styleLoaded: action.loaded,
          },
        },
      };

    case "SET_LAYER_VISIBILITY":
      return {
        ...state,
        layers: {
          ...state.layers,
          [action.layer]: {
            ...state.layers[action.layer],
            visible: action.visible,
          },
        },
      };

    case "SET_LAYER_OPACITY":
      return {
        ...state,
        layers: {
          ...state.layers,
          [action.layer]: {
            ...state.layers[action.layer],
            opacity: action.opacity,
          },
        },
      };

    case "SET_MAP_POSITION":
      return {
        ...state,
        mapPosition: {
          center: action.center,
          zoom: action.zoom,
          isLoaded: true,
        },
      };

    default:
      return state;
  }
}

// ===================
// Context
// ===================

interface ImageryContextType {
  state: ImageryState;
  dispatch: React.Dispatch<ImageryAction>;
}

const ImageryContext = createContext<ImageryContextType | undefined>(undefined);

// ===================
// Provider
// ===================

export function ImageryProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(imageryReducer, initialState);

  return (
    <ImageryContext.Provider value={{ state, dispatch }}>
      {children}
    </ImageryContext.Provider>
  );
}

// ===================
// Hook
// ===================

export function useImageryContext() {
  const context = useContext(ImageryContext);
  if (!context) {
    throw new Error("useImageryContext must be used within ImageryProvider");
  }
  return context;
}

// ===================
// Helper Selectors
// ===================

/**
 * Get the current date for a specific map
 */
export function getCurrentDate(
  state: ImageryState,
  map: MapKey
): AvailableDate | GEAvailableDate | null {
  const mapState = state.maps[map];

  if (mapState.source === "esri") {
    return state.esriDates[mapState.dateIndex] || null;
  } else {
    return mapState.geDates[mapState.dateIndex] || null;
  }
}

/**
 * Get all available dates for a specific map
 */
export function getAvailableDates(
  state: ImageryState,
  map: MapKey
): Array<AvailableDate | GEAvailableDate> {
  const mapState = state.maps[map];

  if (mapState.source === "esri") {
    return state.esriDates;
  } else {
    return mapState.geDates;
  }
}
