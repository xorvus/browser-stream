const ICE_GATHERING_TIMEOUT = 10000;

export function offerURL(profile) {
  return profile && profile !== "full" ? `/offer?profile=${encodeURIComponent(profile)}` : "/offer";
}

// prefersDataSaver reads the browser's own data-saver signal so a phone on a
// metered connection starts in the cheap profile instead of pulling a full-rate
// stream first and only then being switched down by hand.
export function prefersDataSaver(connection = globalThis.navigator?.connection) {
  if (!connection) return false;
  if (connection.saveData) return true;
  return connection.effectiveType === "slow-2g" || connection.effectiveType === "2g" || connection.effectiveType === "3g";
}

/**
 * createStreamSession owns the peer connection lifecycle. Switching delivery
 * profile means renegotiating from scratch, because the server answers with a
 * different codec and a different encoder, so the session has to be able to
 * tear itself down and rebuild without the page reloading.
 */
export function createStreamSession({
  PeerConnection = globalThis.RTCPeerConnection,
  fetch: fetchImpl = (...args) => globalThis.fetch(...args),
  onTrack = () => {},
  onState = () => {},
  gatheringTimeout = ICE_GATHERING_TIMEOUT,
  schedule = (callback, delay) => setTimeout(callback, delay),
  cancel = (timer) => clearTimeout(timer),
} = {}) {
  let peer = null;
  let profile = "full";

  function close() {
    const current = peer;
    peer = null;
    current?.close();
  }

  async function waitForGathering(connection) {
    if (connection.iceGatheringState === "complete") return;
    await new Promise((resolve, reject) => {
      const timer = schedule(() => {
        connection.removeEventListener("icegatheringstatechange", complete);
        reject(new Error("ICE gathering timed out"));
      }, gatheringTimeout);
      function complete() {
        if (connection.iceGatheringState !== "complete") return;
        cancel(timer);
        connection.removeEventListener("icegatheringstatechange", complete);
        resolve();
      }
      connection.addEventListener("icegatheringstatechange", complete);
    });
  }

  async function connect(nextProfile = profile) {
    close();
    profile = nextProfile;

    const connection = new PeerConnection();
    peer = connection;
    connection.addTransceiver("video", { direction: "recvonly" });
    connection.addTransceiver("audio", { direction: "recvonly" });
    connection.ontrack = (event) => {
      if (peer === connection) onTrack(event);
    };
    connection.onconnectionstatechange = () => {
      if (peer === connection) onState(connection.connectionState);
    };

    const offer = await connection.createOffer();
    await connection.setLocalDescription(offer);
    await waitForGathering(connection);

    const response = await fetchImpl(offerURL(profile), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(connection.localDescription),
    });
    if (!response.ok) throw new Error(await response.text());
    await connection.setRemoteDescription(await response.json());
    return connection;
  }

  return {
    connect,
    close,
    get profile() {
      return profile;
    },
    get connectionState() {
      return peer?.connectionState ?? "closed";
    },
    getStats() {
      return peer ? peer.getStats() : Promise.resolve(new Map());
    },
  };
}
