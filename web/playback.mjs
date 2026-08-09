/**
 * clearTracks empties a MediaStream in place.
 *
 * Reconnecting must not assign a new stream to video.srcObject: that restarts
 * the media element's load algorithm and aborts any play() still in flight.
 * Since start() deliberately calls play() before connect(), replacing the
 * stream made that first play() reject, and the rejection handler mutes the
 * video, leaving a perfectly good stream showing a muted sound control.
 */
export function clearTracks(stream) {
  for (const track of stream.getTracks()) {
    stream.removeTrack(track);
    track.stop();
  }
}

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
