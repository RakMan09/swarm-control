import type { VehicleState } from "../types";

interface Props {
  vehicles: VehicleState[];
  alertCount: number;
}

export default function FleetHealth({ vehicles, alertCount }: Props) {
  const total = vehicles.length;
  const now = Date.now();
  const active = vehicles.filter((v) => v.status === "active" || v.status === "ok").length;
  const lowBatt = vehicles.filter((v) => v.batteryPct <= 15).length;
  const returning = vehicles.filter((v) => v.status === "returning").length;
  const stale = vehicles.filter((v) => now - v.lastSeen > 10000).length;
  const avgBatt = total ? vehicles.reduce((s, v) => s + v.batteryPct, 0) / total : 0;

  const stats: [string, number | string][] = [
    ["Vehicles", total],
    ["Active", active],
    ["Returning", returning],
    ["Low batt", lowBatt],
    ["Stale", stale],
    ["Alerts", alertCount],
  ];

  return (
    <div className="card">
      <h2>Fleet health</h2>
      <div className="health-grid">
        {stats.map(([label, n]) => (
          <div className="stat" key={label}>
            <div className="n">{n}</div>
            <div className="l">{label}</div>
          </div>
        ))}
      </div>
      <div className="hint" style={{ marginTop: 10 }}>
        avg battery {avgBatt.toFixed(1)}%
      </div>
    </div>
  );
}
