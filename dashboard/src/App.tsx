import { useEffect, useMemo, useRef, useState } from "react";
import { fetchAlerts, fetchFleet, wsUrl } from "./api";
import type { Alert, VehicleState, WsEnvelope } from "./types";
import AlertsPanel from "./components/AlertsPanel";
import CommandBar from "./components/CommandBar";
import FleetHealth from "./components/FleetHealth";
import FleetMap from "./components/FleetMap";

const MAX_TRAIL = 30;
const MAX_ALERTS = 200;
const ALERT_ACTIVE_MS = 30_000; // highlight a vehicle for 30s after an alert

export default function App() {
  const [vehicles, setVehicles] = useState<Record<string, VehicleState>>({});
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [connected, setConnected] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const trails = useRef<Record<string, [number, number][]>>({});

  // Initial REST snapshot.
  useEffect(() => {
    fetchFleet()
      .then((list) => {
        const map: Record<string, VehicleState> = {};
        for (const v of list) map[v.vehicleId] = v;
        setVehicles(map);
      })
      .catch(() => {});
    fetchAlerts()
      .then((a) => setAlerts(a.slice(0, MAX_ALERTS)))
      .catch(() => {});
  }, []);

  // Live websocket stream with auto-reconnect.
  useEffect(() => {
    let ws: WebSocket | null = null;
    let stop = false;
    let retry: ReturnType<typeof setTimeout>;

    const connect = () => {
      ws = new WebSocket(wsUrl());
      ws.onopen = () => setConnected(true);
      ws.onclose = () => {
        setConnected(false);
        if (!stop) retry = setTimeout(connect, 2000);
      };
      ws.onerror = () => ws?.close();
      ws.onmessage = (ev) => {
        let msg: WsEnvelope;
        try {
          msg = JSON.parse(ev.data);
        } catch {
          return;
        }
        if (msg.type === "fleet") {
          setVehicles((prev) => {
            const next = { ...prev };
            for (const v of msg.data) {
              next[v.vehicleId] = v;
              const t = trails.current[v.vehicleId] ?? [];
              t.push([v.lat, v.lon]);
              if (t.length > MAX_TRAIL) t.shift();
              trails.current[v.vehicleId] = t;
            }
            return next;
          });
        } else if (msg.type === "alert") {
          setAlerts((prev) => [msg.data, ...prev].slice(0, MAX_ALERTS));
        }
      };
    };
    connect();
    return () => {
      stop = true;
      clearTimeout(retry);
      ws?.close();
    };
  }, []);

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 3000);
    return () => clearTimeout(t);
  }, [toast]);

  const vehicleList = useMemo(() => Object.values(vehicles), [vehicles]);

  const alerting = useMemo(() => {
    const now = Date.now();
    const s = new Set<string>();
    for (const a of alerts) {
      if (now - a.detectedAt < ALERT_ACTIVE_MS) s.add(a.vehicleId);
    }
    return s;
  }, [alerts]);

  return (
    <div className="app">
      <div className="topbar">
        <span className="brand-dot" />
        <h1>SwarmControl</h1>
        <span className="hint">mission control · simulated fleet telemetry</span>
        <div className="conn">
          <span>{vehicleList.length} vehicles</span>
          <span className={`pill ${connected ? "live" : "down"}`}>
            {connected ? "LIVE" : "RECONNECTING"}
          </span>
        </div>
      </div>

      <div className="layout">
        <div className="map-wrap">
          <FleetMap
            vehicles={vehicleList}
            trails={trails.current}
            alerting={alerting}
            selected={selected}
            onSelect={setSelected}
          />
          {toast && <div className="toast">{toast}</div>}
          <div className="legend">
            <div>
              <span className="dot" style={{ background: "#4f8cff" }} />
              active
            </div>
            <div>
              <span className="dot" style={{ background: "#a55cff" }} />
              returning
            </div>
            <div>
              <span className="dot" style={{ background: "#f5a623" }} />
              low battery
            </div>
            <div>
              <span className="dot" style={{ background: "#ff5470" }} />
              alerting
            </div>
          </div>
        </div>

        <div className="sidebar">
          <FleetHealth vehicles={vehicleList} alertCount={alerting.size} />
          <CommandBar
            vehicles={vehicleList}
            selected={selected}
            onSelect={setSelected}
            onToast={setToast}
          />
          <AlertsPanel alerts={alerts} onSelect={setSelected} />
        </div>
      </div>
    </div>
  );
}
