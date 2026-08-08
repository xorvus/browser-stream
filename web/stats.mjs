export function deriveVideoFPS(previous, report) {
  let fps = Number.isFinite(report.framesPerSecond) ? report.framesPerSecond : null;
  if (fps === null && previous && report.timestamp > previous.timestamp && report.framesDecoded >= previous.framesDecoded) {
    fps = (report.framesDecoded - previous.framesDecoded) * 1000 / (report.timestamp - previous.timestamp);
  }
  if (fps === null && previous && Number.isFinite(previous.fps)) fps = previous.fps;
  return {
    timestamp: report.timestamp,
    framesDecoded: report.framesDecoded,
    fps,
  };
}
