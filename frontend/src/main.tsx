import React from "react";
import { createRoot } from "react-dom/client";
import "@/index.css";
import App from "@/App";
import { ThemeProvider } from "@/components/ThemeProvider";
import { ImageryProvider } from "@/contexts/ImageryContext";
import { PostHogProvider } from "@/providers/PostHogProvider";

const container = document.getElementById("root");

const root = createRoot(container!);

root.render(
  <React.StrictMode>
    <ThemeProvider defaultTheme="system" storageKey="walkthru-earth-theme">
      <ImageryProvider>
        <PostHogProvider>
          <App />
        </PostHogProvider>
      </ImageryProvider>
    </ThemeProvider>
  </React.StrictMode>
);
