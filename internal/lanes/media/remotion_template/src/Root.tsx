import React from "react";
import { Composition } from "remotion";
import { Main, DURATION_SECONDS } from "./Main";

export const Root: React.FC = () => {
  return (
    <Composition
      id="Main"
      component={Main}
      durationInFrames={Math.max(1, Math.round(DURATION_SECONDS * 30))}
      fps={30}
      width={1920}
      height={1080}
    />
  );
};
