import type { ReactNode } from 'react';

export function BreathingElement({ children, className = '' }: { children: ReactNode; intensity?: number; duration?: number; className?: string }) {
  return <div className={className}>{children}</div>;
}
