type ParticleBackgroundProps = {
  count?: number;
  minOpacity?: number;
  maxOpacity?: number;
};

// Particle background is currently disabled (returns null); props kept in the
// signature as the public contract for the two call sites that still pass them.
// eslint-disable-next-line @typescript-eslint/no-unused-vars
export function ParticleBackground(_props?: ParticleBackgroundProps) {
  return null;
}