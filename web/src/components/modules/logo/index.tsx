'use client';

import { motion } from 'motion/react';

interface LogoProps {
    size?: number | string;
    animate?: boolean;
}

const LOGO_DRAW_DURATION_S = 0.8;
const LOGO_STAGGER_S = 0.15;
const LOGO_FADE_DURATION_S = 0.6;

// Lodestar 标识：指南星 / 北极星（贴合产品名"Lodestar"=引路星）。
// 四芒星 + 内菱形 + 罗盘环 + 南北箭头，描边路径配合 pathLength 绘制动画。
const paths = [
    "M50 14 L58 42 L86 50 L58 58 L50 86 L42 58 L14 50 L42 42 Z",
    "M50 30 L62 50 L50 70 L38 50 Z",
    "M50 8 L47 20 M50 8 L53 20 M50 92 L47 80 M50 92 L53 80",
    "M8 50 L20 47 M8 50 L20 53 M92 50 L80 47 M92 50 L80 53",
    "M50 18 C68 18 82 32 82 50 C82 68 68 82 50 82 C32 82 18 68 18 50 C18 32 32 18 50 18",
];

export const LOGO_DRAW_END_MS = Math.round(
    ((paths.length - 1) * LOGO_STAGGER_S + LOGO_DRAW_DURATION_S) * 1000
);

export default function Logo({ size = 48, animate = false }: LogoProps) {
    const sizeValue = size === '100%' ? '100%' : size;

    if (animate) {
        const drawDuration = LOGO_DRAW_DURATION_S;
        const stagger = LOGO_STAGGER_S;
        const fadeDuration = LOGO_FADE_DURATION_S;

        const drawEndTime = (paths.length - 1) * stagger + drawDuration;
        const cycleDuration = drawEndTime + fadeDuration;

        return (
            <motion.svg
                viewBox="0 0 100 100"
                xmlns="http://www.w3.org/2000/svg"
                width={sizeValue}
                height={sizeValue}
                className="text-primary"
            >
                <motion.g
                    initial={{ opacity: 1 }}
                    animate={{ opacity: [1, 1, 0] }}
                    transition={{
                        duration: cycleDuration,
                        times: [0, drawEndTime / cycleDuration, 1],
                        ease: "easeInOut",
                        repeat: Infinity,
                    }}
                >
                    {paths.map((d, index) => {
                        const startTime = index * stagger;
                        const endTime = startTime + drawDuration;

                        return (
                            <motion.path
                                key={index}
                                d={d}
                                fill="none"
                                stroke="currentColor"
                                strokeWidth="6"
                                strokeLinecap="round"
                                initial={{ pathLength: 0, opacity: 0 }}
                                animate={{
                                    pathLength: [0, 0, 1, 1],
                                    opacity: [0, 0, 1, 1],
                                }}
                                transition={{
                                    pathLength: {
                                        duration: cycleDuration,
                                        times: [
                                            0,
                                            startTime / cycleDuration,
                                            endTime / cycleDuration,
                                            1,
                                        ],
                                        ease: "easeInOut",
                                        repeat: Infinity,
                                    },
                                    opacity: {
                                        duration: cycleDuration,
                                        times: [
                                            0,
                                            startTime / cycleDuration,
                                            endTime / cycleDuration,
                                            1,
                                        ],
                                        ease: "linear",
                                        repeat: Infinity,
                                    },
                                }}
                            />
                        );
                    })}
                </motion.g>
            </motion.svg>
        );
    }

    return (
        <motion.svg
            viewBox="0 0 100 100"
            xmlns="http://www.w3.org/2000/svg"
            width={sizeValue}
            height={sizeValue}
            className="text-primary"
        >
            {paths.map((d, index) => (
                <path
                    key={index}
                    d={d}
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="6"
                    strokeLinecap="round"
                />
            ))}
        </motion.svg>
    );
}
