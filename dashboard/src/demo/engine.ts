// In-browser fleet simulator used for the zero-backend "demo mode" (e.g. the
// GitHub Pages deployment). It mirrors the behaviour of the Python simulators +
// Go alert engine closely enough to make the dashboard fully interactive with
// no server: vehicles move, faults trip geofence / low-battery / anomaly /
// lost-comms alerts, and commands are applied with an ack.
import type { Alert, VehicleState } from "../types";

const ORIGIN = { lat: 37.5, lon: -122.05 };
const AREA = 0.08;
const M_PER_DEG_LAT = 111_320;

// Geofence box (matches the backend default fence).
const FENCE = { minLat: 37.4, maxLat: 37.6, minLon: -122.2, maxLon: -121.9 };

const LOW_BATTERY = 15;
const LOST_COMMS_MS = 8_000;
const ANOMALY_K = 3;

type Fault = "none" | "stray" | "drain" | "silent" | "hot";
const FAULTS: Fault[] = ["stray", "drain", "silent", "hot"];

// Small deterministic PRNG so the demo looks the same across reloads.
function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

interface DemoVehicle {
  id: string;
  lat: number;
  lon: number;
  heading: number;
  speed: number;
  battery: number;
  temp: number;
  baseTemp: number;
  status: string;
  fault: Fault;
  rng: () => number;
  age: number;
  silentAfter: number;
  lastSeen: number;
  tempWindow: number[];
}

export interface DemoHandlers {
  onFleet: (fleet: VehicleState[]) => void;
  onAlert: (alert: Alert) => void;
}

export class DemoEngine {
  private vehicles: DemoVehicle[] = [];
  private active = new Set<string>(); // "id|type" edge-trigger dedup
  private lostFired = new Set<string>();
  private seq = 0;
  private timer: number | null = null;
  private handlers: DemoHandlers | null = null;

  constructor(count = 60, seed = 42) {
    const rng = mulberry32(seed);
    for (let i = 0; i < count; i++) {
      const vr = mulberry32(seed * 1_000_003 + i);
      let fault: Fault = "none";
      if (rng() < 0.12) fault = FAULTS[i % FAULTS.length];
      const baseTemp = 22 + vr() * 6;
      this.vehicles.push({
        id: `veh-${i.toString().padStart(5, "0")}`,
        lat: ORIGIN.lat + (vr() - 0.5) * AREA,
        lon: ORIGIN.lon + (vr() - 0.5) * AREA,
        heading: vr() * 360,
        speed: 5 + vr() * 10,
        battery: 70 + vr() * 30,
        temp: baseTemp,
        baseTemp,
        status: "active",
        fault,
        rng: vr,
        age: 0,
        silentAfter: 12 + vr() * 20,
        lastSeen: Date.now(),
        tempWindow: [],
      });
    }
  }

  start(handlers: DemoHandlers, tickMs = 1000): void {
    this.handlers = handlers;
    this.emitFleet();
    this.timer = window.setInterval(() => this.tick(tickMs / 1000), tickMs);
  }

  stop(): void {
    if (this.timer !== null) window.clearInterval(this.timer);
    this.timer = null;
  }

  snapshot(): VehicleState[] {
    const now = Date.now();
    return this.vehicles.map((v) => ({
      vehicleId: v.id,
      ts: now,
      lat: round(v.lat, 6),
      lon: round(v.lon, 6),
      headingDeg: round(((v.heading % 360) + 360) % 360, 2),
      speed: round(v.speed, 2),
      batteryPct: round(v.battery, 2),
      temp: round(v.temp, 2),
      status: v.status,
      lastSeen: v.lastSeen,
    }));
  }

  applyCommand(vehicleId: string, type: string): { id: string; state: string } {
    const v = this.vehicles.find((x) => x.id === vehicleId);
    const t = type.toLowerCase();
    if (v) {
      if (t === "rtb" || t === "return_to_base") {
        v.status = "returning";
        v.fault = "none";
        v.heading = bearing(v.lat, v.lon, ORIGIN.lat, ORIGIN.lon);
      } else if (t === "reroute") {
        v.heading = v.rng() * 360;
        v.status = "active";
      }
      v.lastSeen = Date.now();
      this.lostFired.delete(v.id);
    }
    return { id: `cmd-demo-${++this.seq}`, state: "applied" };
  }

  private tick(dt: number): void {
    const now = Date.now();
    for (const v of this.vehicles) {
      v.age += dt;

      // Silent vehicles stop reporting -> lost-comms.
      if (v.fault === "silent" && v.age >= v.silentAfter) {
        if (now - v.lastSeen > LOST_COMMS_MS && !this.lostFired.has(v.id)) {
          v.status = "offline";
          this.lostFired.add(v.id);
          this.fire(v, "lost_comms", "critical", `${v.id} lost comms (no telemetry)`, undefined);
        }
        continue; // no telemetry update while silent
      }

      // Motion.
      if (v.fault === "stray") {
        v.heading = 90;
        v.speed = Math.max(v.speed, 14);
      } else if (v.status !== "returning") {
        v.heading = (v.heading + (v.rng() - 0.5) * 25) % 360;
      }
      const dist = v.speed * dt;
      v.lat += (Math.cos(deg(v.heading)) * dist) / M_PER_DEG_LAT;
      v.lon += (Math.sin(deg(v.heading)) * dist) / (M_PER_DEG_LAT * Math.cos(deg(v.lat)) || 1e-9);

      // Battery.
      let drain = 0.15 + v.speed * 0.01;
      if (v.fault === "drain") drain *= 8;
      v.battery = Math.max(0, v.battery - drain * dt);
      if (v.battery < 4) v.battery = 100; // recharge/loop so the demo keeps running

      // Temperature (+ spikes for hot vehicles).
      v.temp = v.baseTemp + v.speed * 0.1 + (v.rng() - 0.5) * 0.6;
      if (v.fault === "hot" && v.rng() < 0.08) v.temp += 30 + v.rng() * 15;

      v.status = v.battery <= LOW_BATTERY ? "low_batt" : v.status === "returning" ? "returning" : "active";
      v.lastSeen = now;

      this.evaluate(v);
    }
    this.emitFleet();
  }

  private evaluate(v: DemoVehicle): void {
    // Geofence.
    const inside = v.lat >= FENCE.minLat && v.lat <= FENCE.maxLat && v.lon >= FENCE.minLon && v.lon <= FENCE.maxLon;
    this.transition(v, "geofence", !inside, "critical", `${v.id} left the geofence`);

    // Low battery.
    this.transition(v, "low_battery", v.battery <= LOW_BATTERY, "warning", `${v.id} battery low: ${v.battery.toFixed(1)}%`, v.battery);

    // Anomaly (rolling mean + k*stddev on temperature).
    const w = v.tempWindow;
    if (w.length >= 8) {
      const mean = w.reduce((a, b) => a + b, 0) / w.length;
      const std = Math.sqrt(w.reduce((a, b) => a + (b - mean) ** 2, 0) / w.length);
      if (std > 0 && v.temp > mean + ANOMALY_K * std) {
        this.fire(v, "anomaly", "warning", `${v.id} temp anomaly: ${v.temp.toFixed(1)} (mean ${mean.toFixed(1)})`, v.temp);
      }
    }
    w.push(v.temp);
    if (w.length > 30) w.shift();
  }

  private transition(v: DemoVehicle, type: string, cond: boolean, sev: string, msg: string, value?: number): void {
    const key = `${v.id}|${type}`;
    const was = this.active.has(key);
    if (cond) this.active.add(key);
    else this.active.delete(key);
    if (cond && !was) this.fire(v, type, sev, msg, value);
  }

  private fire(v: DemoVehicle, type: string, severity: string, message: string, value?: number): void {
    const now = Date.now();
    this.handlers?.onAlert({
      id: `a-demo-${++this.seq}`,
      vehicleId: v.id,
      type,
      severity,
      message,
      value,
      lat: v.lat,
      lon: v.lon,
      ts: now,
      detectedAt: now,
    });
  }

  private emitFleet(): void {
    this.handlers?.onFleet(this.snapshot());
  }
}

function deg(d: number): number {
  return (d * Math.PI) / 180;
}
function bearing(lat: number, lon: number, tlat: number, tlon: number): number {
  return ((Math.atan2(tlon - lon, tlat - lat) * 180) / Math.PI + 360) % 360;
}
function round(v: number, n: number): number {
  const p = 10 ** n;
  return Math.round(v * p) / p;
}
