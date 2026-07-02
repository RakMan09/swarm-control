export interface VehicleState {
  vehicleId: string;
  ts: number;
  lat: number;
  lon: number;
  headingDeg: number;
  speed: number;
  batteryPct: number;
  temp: number;
  status: string;
  lastSeen: number;
}

export interface Alert {
  id: string;
  vehicleId: string;
  type: "geofence" | "low_battery" | "lost_comms" | "anomaly" | string;
  severity: "info" | "warning" | "critical" | string;
  message: string;
  value?: number;
  lat?: number;
  lon?: number;
  ts: number;
  detectedAt: number;
}

export type WsEnvelope =
  | { type: "fleet"; data: VehicleState[] }
  | { type: "alert"; data: Alert };
