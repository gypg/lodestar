/**
 * Heuristic image-capable model ids from the public model catalog.
 * Lodestar has no separate image flag on models; names follow common vendor patterns.
 */
const IMAGE_HINT =
    /dall-e|gpt-image|flux|stable-diffusion|sdxl|midjourney|imagen|kolors|cogview|wanx|ideogram|recraft/i;

export function filterImageModelNames(names: string[]): string[] {
    const hits = names.filter((n) => IMAGE_HINT.test(n));
    if (hits.length > 0) return [...new Set(hits)];
    return [...new Set(names)].slice(0, 30);
}