import logoImg from '@/assets/images/logo-universal.png';

export function Header() {
  return (
    <header className="flex items-center gap-3 px-6 py-4 border-b border-border bg-card">
      <img
        src={logoImg}
        alt="Walkthru Earth"
        className="h-10 w-auto"
      />
      <div className="flex flex-col">
        <h1 className="text-xl font-semibold tracking-tight">
          Walkthru Earth
        </h1>
        <p className="text-sm text-muted-foreground">
          Historical Satellite Imagery Desktop
        </p>
      </div>
    </header>
  );
}
