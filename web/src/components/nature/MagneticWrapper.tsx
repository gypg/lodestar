import type { ReactNode } from 'react';

export function MagneticWrapper({ children, className = '' }: { children: ReactNode; intensity?: number; scale?: number; className?: string }) {
  return <div className={className}>{children}</div>;
}
