import { useEffect, useMemo, useRef, useState } from "react";
import { createDataSource, type DataSource } from "./datasource";
import type { Alert, VehicleState } from "./types";
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
  const dsRef = useRef<DataSource | null>(null);
  if (dsRef.current === null) dsRef.current = createDataSource();
  const ds = dsRef.current;

  const applyFleet = (list: VehicleState[]) => {
    setVehicles((prev) => {
      const next = { ...prev };
      for (const v of list) {
        next[v.vehicleId] = v;
        const t = trails.current[v.vehicleId] ?? [];
        t.push([v.lat, v.lon]);
        if (t.length > MAX_TRAIL) t.shift();
        trails.current[v.vehicleId] = t;
      }
      return next;
    });
  };

  // Initial snapshot.
  useEffect(() => {
    ds.initialFleet()
      .then((list) => {
        const map: Record<string, VehicleState> = {};
        for (const v of list) map[v.vehicleId] = v;
        setVehicles(map);
      })
      .catch(() => {});
    ds.initialAlerts()
      .then((a) => setAlerts(a.slice(0, MAX_ALERTS)))
      .catch(() => {});
  }, [ds]);

  // Live stream (WebSocket for the real backend, or the demo engine).
  useEffect(() => {
    const disconnect = ds.connect({
      onFleet: applyFleet,
      onAlert: (a) => setAlerts((prev) => [a, ...prev].slice(0, MAX_ALERTS)),
      onStatus: setConnected,
    });
    return disconnect;
  }, [ds]);

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 3000);
    return () => clearTimeout(t);
  }, [toast]);

  const sendCommand = async (vehicleId: string, type: string, payload?: Record<string, unknown>) => {
    try {
      await ds.sendCommand({ vehicleId, type, payload });
      setToast(`Sent ${type} to ${vehicleId}`);
    } catch (e) {
      setToast(`Command failed: ${(e as Error).message}`);
    }
  };

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
        {ds.isDemo && <span className="demo-badge">DEMO MODE · in-browser simulation</span>}
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
            onSend={(type, payload) => sendCommand(selected as string, type, payload)}
          />
          <AlertsPanel alerts={alerts} onSelect={setSelected} />
        </div>
      </div>
    </div>
  );
}
