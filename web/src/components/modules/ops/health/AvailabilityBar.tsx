'use client';

/**
 * 30-day availability bar — each day is a tiny colored segment.
 * Green  ≥ 90% success rate
 * Yellow ≥ 50%
 * Red    < 50%
 * Gray   no data
 */
export function AvailabilityBar({ values }: { values: number[] }) {
    if (!values || values.length === 0) return null;

    return (
        <div className="flex gap-px" title={values.map((v, i) => `D${i + 1}: ${v.toFixed(0)}%`).join('\n')}>
            {values.map((v, i) => {
                const color =
                    v >= 90 ? 'bg-emerald-400' :
                    v >= 50 ? 'bg-amber-400' :
                    v > 0   ? 'bg-red-400' :
                              'bg-muted';
                return (
                    <div
                        key={i}
                        className={`h-3 flex-1 rounded-[1px] ${color}`}
                        title={`D${i + 1}: ${v.toFixed(1)}%`}
                    />
                );
            })}
        </div>
    );
}
