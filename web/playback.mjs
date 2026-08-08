export function createPlaybackStarter({ video, connect, onError = () => {} }) {
  let pending = null;

  function start() {
    if (pending) return pending;

    video.muted = false;

    let playback;
    let connection;
    try {
      playback = Promise.resolve(video.play());
    } catch (error) {
      playback = Promise.reject(error);
    }
    try {
      connection = Promise.resolve(connect());
    } catch (error) {
      connection = Promise.reject(error);
    }

    pending = Promise.all([playback, connection]).catch((error) => {
      onError(error);
      throw error;
    });
    return pending;
  }

  return { start };
}
