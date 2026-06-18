'use client';

import { motion } from 'motion/react';

interface LogoProps {
    size?: number | string;
    animate?: boolean;
}

const LOGO_DRAW_DURATION_S = 0.8;
const LOGO_STAGGER_S = 0.15;
const LOGO_FADE_DURATION_S = 0.6;

// GGZERO 标识：六瓣雪花（贴合冬日主题，替代上游章鱼图案）。
// 三条主轴穿过中心 + 各瓣分枝，描边路径配合 pathLength 绘制动画。
const paths = [
    "M50 12 L50 88",
    "M17 31 L83 69",
    "M83 31 L17 69",
    "M50 26 L43 19 M50 26 L57 19 M50 74 L43 81 M50 74 L57 81",
    "M28 33 L20 30 M28 33 L25 41 M72 67 L80 70 M72 67 L75 59",
    "M72 33 L80 30 M72 33 L75 41 M28 67 L20 70 M28 67 L25 59",
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
