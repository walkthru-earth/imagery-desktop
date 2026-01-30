/**
 * Type declarations for @maplibre/maplibre-gl-compare
 *
 * This library doesn't ship with TypeScript definitions,
 * so we define them here.
 */

declare module "@maplibre/maplibre-gl-compare" {
  import type { Map as MapLibreMap } from "maplibre-gl";

  export interface CompareOptions {
    /**
     * Orientation of the compare slider
     * @default "vertical"
     */
    orientation?: "vertical" | "horizontal";

    /**
     * Enable swipe on mousemove
     * @default false
     */
    mousemove?: boolean;
  }

  export default class Compare {
    /**
     * Create a new Compare instance
     *
     * @param beforeMap - The left/before map instance
     * @param afterMap - The right/after map instance
     * @param container - CSS selector or HTML element for the container
     * @param options - Compare options
     */
    constructor(
      beforeMap: MapLibreMap,
      afterMap: MapLibreMap,
      container: string | HTMLElement,
      options?: CompareOptions
    );

    /**
     * Set the slider position
     *
     * @param x - Position from 0 to 1 (0 = left/top, 1 = right/bottom)
     */
    setSlider(x: number): void;

    /**
     * Remove the compare instance and clean up
     */
    remove(): void;

    /**
     * Add an event listener
     *
     * @param type - Event type (e.g., 'slideend')
     * @param listener - Event listener function
     */
    on(type: string, listener: (e: any) => void): this;

    /**
     * Remove an event listener
     *
     * @param type - Event type
     * @param listener - Event listener function
     */
    off(type: string, listener: (e: any) => void): this;

    /**
     * Fire an event
     *
     * @param type - Event type
     */
    fire(type: string, data?: any): this;
  }
}
