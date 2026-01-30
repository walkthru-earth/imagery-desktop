import { ReactNode } from 'react';
import { Header } from './Header';

interface MainLayoutProps {
  children: ReactNode;
  header?: ReactNode;
}

/**
 * Main application layout container
 * - Full-height flex container
 * - Fixed header at top
 * - Flexible content area below
 * - Optimized for desktop (min-width: 1024px)
 */
export function MainLayout({ children, header }: MainLayoutProps) {
  return (
    <div className="flex h-screen flex-col bg-background">
      {/* Header - Fixed at top */}
      <div className="flex-none">
        {header || <Header />}
      </div>

      {/* Content Area - Flexible, takes remaining height */}
      <div className="flex flex-1 overflow-hidden">
        {children}
      </div>
    </div>
  );
}
