export function createPlaybackStarter({ video, connect, onError = () => {} }) {
  let pending = null;
  let connection = null;

  function start() {
    if (pending) return pending;

    video.muted = false;

    let playback;
    try {
      playback = Promise.resolve(video.play());
    } catch (error) {
      playback = Promise.reject(error);
    }
    if (!connection) {
      try {
        connection = Promise.resolve(connect()).catch((error) => {
          connection = null;
          throw error;
        });
      } catch (error) {
        connection = Promise.reject(error);
        connection = connection.catch((connectionError) => {
          connection = null;
          throw connectionError;
        });
      }
    }

    pending = Promise.all([playback, connection]).catch((error) => {
      pending = null;
      onError(error);
      throw error;
    }).finally(() => {
      pending = null;
    });
    return pending;
  }

  return { start };
}
