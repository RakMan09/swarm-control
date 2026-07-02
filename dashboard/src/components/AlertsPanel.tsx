import type { Alert } from "../types";

interface Props {
  alerts: Alert[];
  onSelect: (id: string) => void;
}

function severityClass(sev: string): string {
  if (sev === "critical") return "critical";
  if (sev === "warning") return "warning";
  return "info";
}

function ago(ms: number): string {
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  return `${m}m ago`;
}

export default function AlertsPanel({ alerts, onSelect }: Props) {
  return (
    <div className="card alerts">
      <h2>Alerts ({alerts.length})</h2>
      {alerts.length === 0 && <div className="hint">No alerts yet.</div>}
      {alerts.map((a) => (
        <div
          key={a.id}
          className={`alert ${severityClass(a.severity)}`}
          onClick={() => onSelect(a.vehicleId)}
          style={{ cursor: "pointer" }}
        >
          <div className="bar" />
          <div>
            <div className="body">{a.message}</div>
            <div className="meta">
              {a.type} · {a.vehicleId} · {ago(a.detectedAt)}
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}
