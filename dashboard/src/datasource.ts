// A DataSource feeds the dashboard either from the real backend (REST + live
// WebSocket) or from the in-browser DemoEngine (zero-backend demo mode). The UI
// is identical in both cases; only the wiring differs.
import {
  fetchAlerts,
  fetchFleet,
  sendCommand as apiSendCommand,
  wsUrl,
  type CommandRequest,
} from "./api";
import { DemoEngine } from "./demo/engine";
import type { Alert, VehicleState, WsEnvelope } from "./types";

export interface DataHandlers {
  onFleet: (fleet: VehicleState[]) => void;
  onAlert: (alert: Alert) => void;
  onStatus: (connected: boolean) => void;
}

export interface DataSource {
  readonly isDemo: boolean;
  initialFleet(): Promise<VehicleState[]>;
  initialAlerts(): Promise<Alert[]>;
  connect(handlers: DataHandlers): () => void; // returns a disconnect fn
  sendCommand(req: CommandRequest): Promise<unknown>;
}

class RealDataSource implements DataSource {
  readonly isDemo = false;

  initialFleet() {
    return fetchFleet();
  }
  initialAlerts() {
    return fetchAlerts();
  }
  sendCommand(req: CommandRequest) {
    return apiSendCommand(req);
  }

  connect(handlers: DataHandlers): () => void {
    let ws: WebSocket | null = null;
    let stop = false;
    let retry: ReturnType<typeof setTimeout>;

    const open = () => {
      ws = new WebSocket(wsUrl());
      ws.onopen = () => handlers.onStatus(true);
      ws.onclose = () => {
        handlers.onStatus(false);
        if (!stop) retry = setTimeout(open, 2000);
      };
      ws.onerror = () => ws?.close();
      ws.onmessage = (ev) => {
        let msg: WsEnvelope;
        try {
          msg = JSON.parse(ev.data);
        } catch {
          return;
        }
        if (msg.type === "fleet") handlers.onFleet(msg.data);
        else if (msg.type === "alert") handlers.onAlert(msg.data);
      };
    };
    open();
    return () => {
      stop = true;
      clearTimeout(retry);
      ws?.close();
    };
  }
}

class DemoDataSource implements DataSource {
  readonly isDemo = true;
  private engine = new DemoEngine();

  initialFleet() {
    return Promise.resolve(this.engine.snapshot());
  }
  initialAlerts() {
    return Promise.resolve([] as Alert[]);
  }
  sendCommand(req: CommandRequest) {
    return Promise.resolve(this.engine.applyCommand(req.vehicleId, req.type));
  }

  connect(handlers: DataHandlers): () => void {
    handlers.onStatus(true);
    this.engine.start({ onFleet: handlers.onFleet, onAlert: handlers.onAlert });
    return () => this.engine.stop();
  }
}

export const IS_DEMO = Boolean(import.meta.env.VITE_DEMO_MODE);

export function createDataSource(): DataSource {
  return IS_DEMO ? new DemoDataSource() : new RealDataSource();
}
