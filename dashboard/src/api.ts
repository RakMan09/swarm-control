import type { Alert, VehicleState } from "./types";

const API_BASE: string =
  (import.meta.env.VITE_API_BASE as string) || "http://localhost:8081";

const WS_URL: string =
  (import.meta.env.VITE_WS_URL as string) ||
  API_BASE.replace(/^http/, "ws") + "/ws";

export function wsUrl(): string {
  return WS_URL;
}

export async function fetchFleet(): Promise<VehicleState[]> {
  const r = await fetch(`${API_BASE}/api/fleet`);
  if (!r.ok) throw new Error(`fleet: ${r.status}`);
  return r.json();
}

export async function fetchAlerts(): Promise<Alert[]> {
  const r = await fetch(`${API_BASE}/api/alerts`);
  if (!r.ok) throw new Error(`alerts: ${r.status}`);
  return r.json();
}

export interface CommandRequest {
  vehicleId: string;
  type: string;
  payload?: Record<string, unknown>;
}

export async function sendCommand(req: CommandRequest): Promise<unknown> {
  const r = await fetch(`${API_BASE}/api/commands`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  if (!r.ok) throw new Error(`command failed: ${r.status}`);
  return r.json();
}

export interface HistoryPoint {
  ts: number;
  lat: number;
  lon: number;
  speed: number;
  batteryPct: number;
  temp: number;
}

export async function fetchHistory(
  vehicleId: string,
  window = "24h",
  resolution = "1m",
): Promise<{ points: HistoryPoint[]; queryMs: number; resolution: string }> {
  const r = await fetch(
    `${API_BASE}/api/vehicles/${encodeURIComponent(vehicleId)}/history?window=${window}&resolution=${resolution}`,
  );
  if (!r.ok) throw new Error(`history: ${r.status}`);
  return r.json();
}
