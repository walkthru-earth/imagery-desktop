import * as React from "react";
import { useState, useEffect } from "react";
import { ChevronDown, Satellite, Globe } from "lucide-react";

interface CustomSource {
  name: string;
  type: string;
  enabled: boolean;
}

interface SourceOption {
  value: string;
  label: string;
  icon: React.ReactNode;
  category: "built-in" | "custom";
}

interface SourceSelectorProps {
  value: string;
  onChange: (source: string) => void;
  size?: "sm" | "md";
  className?: string;
}

export function SourceSelector({ value, onChange, size = "sm", className = "" }: SourceSelectorProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [customSources, setCustomSources] = useState<CustomSource[]>([]);

  // Load custom sources from settings
  useEffect(() => {
    loadCustomSources();
  }, []);

  const loadCustomSources = async () => {
    try {
      const settings = await (window as any).go.main.App.GetSettings();
      if (settings?.customSources) {
        setCustomSources(settings.customSources.filter((s: CustomSource) => s.enabled));
      }
    } catch (error) {
      console.error("Failed to load custom sources:", error);
    }
  };

  // Build options list
  const options: SourceOption[] = [
    {
      value: "esri",
      label: "Esri Wayback",
      icon: <Satellite className="h-4 w-4" />,
      category: "built-in",
    },
    {
      value: "google",
      label: "Google Earth",
      icon: <Globe className="h-4 w-4" />,
      category: "built-in",
    },
    ...customSources.map((source) => ({
      value: source.name,
      label: source.name,
      icon: <Globe className="h-4 w-4" />,
      category: "custom" as const,
    })),
  ];

  const selectedOption = options.find((opt) => opt.value === value) || options[0];

  const handleSelect = (optionValue: string) => {
    onChange(optionValue);
    setIsOpen(false);
  };

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      if (!target.closest(".source-selector")) {
        setIsOpen(false);
      }
    };

    if (isOpen) {
      document.addEventListener("click", handleClickOutside);
      return () => document.removeEventListener("click", handleClickOutside);
    }
  }, [isOpen]);

  const buttonClasses = size === "sm"
    ? "px-3 py-1.5 text-sm"
    : "px-4 py-2.5 text-base";

  return (
    <div className={`relative source-selector ${className}`}>
      {/* Selected Source Button */}
      <button
        onClick={() => setIsOpen(!isOpen)}
        className={`
          flex items-center gap-2 rounded-lg border border-border
          bg-background hover:bg-muted transition-colors
          ${buttonClasses}
          ${isOpen ? "ring-2 ring-primary" : ""}
        `}
      >
        {selectedOption.icon}
        <span className="font-medium">{selectedOption.label}</span>
        <ChevronDown className={`h-4 w-4 transition-transform ${isOpen ? "rotate-180" : ""}`} />
      </button>

      {/* Dropdown Menu */}
      {isOpen && (
        <div className="absolute top-full left-0 mt-2 w-full min-w-[200px] z-50">
          <div className="bg-background border border-border rounded-lg shadow-lg overflow-hidden">
            {/* Built-in Sources */}
            <div className="p-1">
              <div className="px-2 py-1.5 text-xs font-semibold text-muted-foreground">
                Built-in Sources
              </div>
              {options
                .filter((opt) => opt.category === "built-in")
                .map((option) => (
                  <button
                    key={option.value}
                    onClick={() => handleSelect(option.value)}
                    className={`
                      w-full flex items-center gap-2 px-3 py-2 rounded-md text-left
                      transition-colors
                      ${
                        option.value === value
                          ? "bg-primary text-primary-foreground"
                          : "hover:bg-muted"
                      }
                    `}
                  >
                    {option.icon}
                    <span className="font-medium text-sm">{option.label}</span>
                    {option.value === value && (
                      <span className="ml-auto text-xs">✓</span>
                    )}
                  </button>
                ))}
            </div>

            {/* Custom Sources */}
            {customSources.length > 0 && (
              <>
                <div className="border-t border-border my-1" />
                <div className="p-1">
                  <div className="px-2 py-1.5 text-xs font-semibold text-muted-foreground">
                    Custom Sources
                  </div>
                  {options
                    .filter((opt) => opt.category === "custom")
                    .map((option) => (
                      <button
                        key={option.value}
                        onClick={() => handleSelect(option.value)}
                        className={`
                          w-full flex items-center gap-2 px-3 py-2 rounded-md text-left
                          transition-colors
                          ${
                            option.value === value
                              ? "bg-primary text-primary-foreground"
                              : "hover:bg-muted"
                          }
                        `}
                      >
                        {option.icon}
                        <span className="font-medium text-sm">{option.label}</span>
                        {option.value === value && (
                          <span className="ml-auto text-xs">✓</span>
                        )}
                      </button>
                    ))}
                </div>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
