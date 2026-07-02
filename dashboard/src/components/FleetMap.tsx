import { CircleMarker, MapContainer, Polyline, Popup, TileLayer } from "react-leaflet";
import type { VehicleState } from "../types";

const STATUS_COLOR: Record<string, string> = {
  active: "#4f8cff",
  ok: "#35c46b",
  idle: "#8b93b0",
  returning: "#a55cff",
  low_batt: "#f5a623",
  fault: "#ff5470",
  offline: "#5b6480",
};

function colorFor(v: VehicleState, alerting: boolean): string {
  if (alerting) return "#ff5470";
  return STATUS_COLOR[v.status] ?? "#4f8cff";
}

interface Props {
  vehicles: VehicleState[];
  trails: Record<string, [number, number][]>;
  alerting: Set<string>;
  selected: string | null;
  onSelect: (id: string) => void;
}

export default function FleetMap({ vehicles, trails, alerting, selected, onSelect }: Props) {
  return (
    <MapContainer center={[37.5, -122.05]} zoom={12} preferCanvas>
      <TileLayer
        attribution="&copy; OpenStreetMap"
        url="https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png"
      />
      {vehicles.map((v) => {
        const isAlerting = alerting.has(v.vehicleId);
        const isSelected = selected === v.vehicleId;
        const trail = trails[v.vehicleId];
        return (
          <div key={v.vehicleId}>
            {trail && trail.length > 1 && (
              <Polyline positions={trail} pathOptions={{ color: colorFor(v, isAlerting), weight: 1.5, opacity: 0.5 }} />
            )}
            <CircleMarker
              center={[v.lat, v.lon]}
              radius={isSelected ? 9 : 6}
              pathOptions={{
                color: isAlerting ? "#ff5470" : "#0b1020",
                weight: isSelected ? 3 : 1,
                fillColor: colorFor(v, isAlerting),
                fillOpacity: 0.9,
              }}
              eventHandlers={{ click: () => onSelect(v.vehicleId) }}
            >
              <Popup>
                <b>{v.vehicleId}</b>
                <br />
                status: {v.status}
                <br />
                battery: {v.batteryPct.toFixed(1)}%
                <br />
                speed: {v.speed.toFixed(1)} m/s
                <br />
                temp: {v.temp.toFixed(1)}°C
                <br />
                heading: {v.headingDeg.toFixed(0)}°
              </Popup>
            </CircleMarker>
          </div>
        );
      })}
    </MapContainer>
  );
}
