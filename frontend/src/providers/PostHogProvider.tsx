import { useEffect } from 'react';
import posthog from 'posthog-js';
import { PostHogProvider as PHProvider } from 'posthog-js/react';

export function PostHogProvider({ children }: { children: React.ReactNode }) {
  useEffect(() => {
    // Only init if key is present
    const apiKey = import.meta.env.VITE_POSTHOG_KEY;
    const apiHost = import.meta.env.VITE_POSTHOG_HOST || 'https://eu.i.posthog.com';

    if (!apiKey) {
      console.warn("[PostHog] No API key found, skipping initialization");
      return;
    }

    posthog.init(apiKey, {
      api_host: apiHost,
      person_profiles: 'identified_only', // Privacy friendly
      capture_pageview: true, // We want to capture screen views
      capture_pageleave: true,
      persistence: 'localStorage', // Persistence is fine for a desktop app
      // Disable autocapture for text inputs to prevent sensitive data leak
      autocapture: {
        dom_event_allowlist: ['click', 'change', 'submit'], // Only capture these events
        element_allowlist: ['a', 'button', 'form', 'input', 'select', 'textarea', 'label'], // Elements to capture
      },
      // Explicitly disable capturing input values
      mask_all_element_attributes: true, 
      mask_all_text: true
    });
  }, []);

  return <PHProvider client={posthog}>{children}</PHProvider>;
}
