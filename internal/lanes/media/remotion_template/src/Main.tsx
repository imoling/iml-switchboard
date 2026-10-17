import React from "react";
import { AbsoluteFill, interpolate, useCurrentFrame, useVideoConfig } from "remotion";

// 动画总时长（秒）。Root.tsx 用它算 durationInFrames，改时长只改这里。
export const DURATION_SECONDS = 8;

// 占位动画：标题淡入。按描述改写这个组件即可，保持默认导出名 Main。
export const Main: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const opacity = interpolate(frame, [0, fps], [0, 1], { extrapolateRight: "clamp" });
  return (
    <AbsoluteFill style={{ backgroundColor: "#0f172a", justifyContent: "center", alignItems: "center" }}>
      <h1 style={{ color: "white", fontSize: 120, fontFamily: "sans-serif", opacity }}>iML Switchboard</h1>
    </AbsoluteFill>
  );
};
