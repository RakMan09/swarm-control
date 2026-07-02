import { useState } from "react";
import { sendCommand } from "../api";
import type { VehicleState } from "../types";

interface Props {
  vehicles: VehicleState[];
  selected: string | null;
  onSelect: (id: string) => void;
  onToast: (msg: string) => void;
}

export default function CommandBar({ vehicles, selected, onSelect, onToast }: Props) {
  const [busy, setBusy] = useState(false);

  async function send(type: string, payload?: Record<string, unknown>) {
    if (!selected) return;
    setBusy(true);
    try {
      await sendCommand({ vehicleId: selected, type, payload });
      onToast(`Sent ${type} to ${selected}`);
    } catch (e) {
      onToast(`Command failed: ${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="card">
      <h2>Command</h2>
      <div className="command-bar">
        <select value={selected ?? ""} onChange={(e) => onSelect(e.target.value)}>
          <option value="" disabled>
            Select a vehicle…
          </option>
          {vehicles
            .slice()
            .sort((a, b) => a.vehicleId.localeCompare(b.vehicleId))
            .map((v) => (
              <option key={v.vehicleId} value={v.vehicleId}>
                {v.vehicleId} ({v.status})
              </option>
            ))}
        </select>
        <div className="row">
          <button disabled={!selected || busy} onClick={() => send("RTB")}>
            Return to base
          </button>
          <button
            disabled={!selected || busy}
            onClick={() => send("reroute", { headingDeg: Math.floor(Math.random() * 360) })}
          >
            Reroute
          </button>
        </div>
        <button
          disabled={!selected || busy}
          onClick={() =>
            send("set_geofence", {
              polygon: [
                [37.4, -122.2],
                [37.6, -122.2],
                [37.6, -121.9],
                [37.4, -121.9],
              ],
            })
          }
        >
          Set geofence
        </button>
        <div className="hint">
          Commands publish over MQTT (QoS 1); delivery is confirmed by the vehicle ack.
        </div>
      </div>
    </div>
  );
}
