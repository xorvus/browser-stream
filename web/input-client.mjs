const OPEN = 1;
const DEFAULT_MOVE_INTERVAL = 1000 / 60;
const MAX_RECONNECT_DELAY = 5000;

export function inputWebSocketURL(locationObject = globalThis.location) {
  if (!locationObject || locationObject.protocol === "file:") return "ws://localhost:8080/input/ws";
  const protocol = locationObject.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${locationObject.host}/input/ws`;
}

export function createInputClient({
  url = inputWebSocketURL(),
  WebSocketImpl = globalThis.WebSocket,
  schedule = (callback, delay) => setTimeout(callback, delay),
  cancel = (timer) => clearTimeout(timer),
  onStatus = () => {},
  onError = () => {},
  moveInterval = DEFAULT_MOVE_INTERVAL,
  scheduleMove,
  cancelMove,
} = {}) {
  let socket = null;
  let stopped = true;
  let sequence = 0;
  let reconnectAttempt = 0;
  let reconnectTimer = null;
  let moveTimer = null;
  let pendingMove = null;

  const schedulePointerMove = scheduleMove || (typeof globalThis.requestAnimationFrame === "function"
    ? (callback) => globalThis.requestAnimationFrame(callback)
    : (callback) => schedule(callback, moveInterval));
  const cancelPointerMove = cancelMove || (typeof globalThis.cancelAnimationFrame === "function"
    ? (frame) => globalThis.cancelAnimationFrame(frame)
    : cancel);

  function ready() {
    return socket?.readyState === OPEN;
  }

  function sendNow(input) {
    if (!ready()) return false;
    sequence += 1;
    input.v = 1;
    input.seq = sequence;
    socket.send(JSON.stringify(input));
    return true;
  }

  function flushMove() {
    moveTimer = null;
    const move = pendingMove;
    pendingMove = null;
    if (move) sendNow(move);
  }

  function scheduleReconnect() {
    if (stopped || reconnectTimer) return;
    const delay = Math.min(250 * (2 ** reconnectAttempt), MAX_RECONNECT_DELAY);
    reconnectAttempt += 1;
    reconnectTimer = schedule(() => {
      reconnectTimer = null;
      open();
    }, delay);
  }

  function open() {
    if (stopped) return;
    const current = new WebSocketImpl(url);
    socket = current;
    current.onopen = () => {
      if (socket !== current) return;
      reconnectAttempt = 0;
      onStatus("connected");
    };
    current.onmessage = (event) => {
      if (socket !== current) return;
      try {
        const acknowledgement = JSON.parse(event.data);
        if (acknowledgement.error) onError(new Error(acknowledgement.error), acknowledgement);
      } catch (error) {
        onError(error);
      }
    };
    current.onerror = () => {
      if (socket === current) onStatus("error");
    };
    current.onclose = () => {
      if (socket !== current) return;
      socket = null;
      pendingMove = null;
      if (moveTimer) cancelPointerMove(moveTimer);
      moveTimer = null;
      onStatus("disconnected");
      scheduleReconnect();
    };
  }

  return {
    connect() {
      if (!stopped) return;
      stopped = false;
      open();
    },
    send(input) {
      if (!ready()) return false;
      if (input.type !== "move") return sendNow(input);
      pendingMove = input;
      if (!moveTimer) moveTimer = schedulePointerMove(flushMove);
      return true;
    },
    ready,
    close() {
      stopped = true;
      pendingMove = null;
      if (moveTimer) cancelPointerMove(moveTimer);
      if (reconnectTimer) cancel(reconnectTimer);
      moveTimer = null;
      reconnectTimer = null;
      const current = socket;
      socket = null;
      current?.close();
      onStatus("closed");
    },
  };
}
