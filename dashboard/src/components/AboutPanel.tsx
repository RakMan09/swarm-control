import { useState } from "react";

interface Props {
  demo: boolean;
}

// A short, collapsible orientation panel so a first-time visitor immediately
// understands what the console is and how to move around it.
export default function AboutPanel({ demo }: Props) {
  const [open, setOpen] = useState(true);

  return (
    <div className="card about">
      <h2 className="about-head" onClick={() => setOpen((v) => !v)}>
        About this console
        <span className="chev">{open ? "\u25be" : "\u25b8"}</span>
      </h2>
      {open && (
        <div className="about-body">
          <p>
            SwarmControl is a live mission-control console for a{" "}
            {demo ? "simulated" : ""} fleet of drones / rovers. Each vehicle
            streams telemetry in real time; the platform stores it, raises
            alerts, and lets you send commands the vehicle acknowledges.
          </p>
          <p className="about-nav-title">How to navigate</p>
          <ul>
            <li>
              <b>Map (left):</b> every dot is a vehicle, colored by status (red =
              active alert). Click one to select it and open its details; the
              faint line is its recent trail.
            </li>
            <li>
              <b>Fleet health (top right):</b> live counts — total, active,
              returning, low-battery, stale — and average battery.
            </li>
            <li>
              <b>Command:</b> pick a vehicle (or click it on the map) and send
              <i> Return to base</i>, <i>Reroute</i>, or <i>Set geofence</i>.
            </li>
            <li>
              <b>Alerts (bottom right):</b> live feed of geofence, low-battery,
              lost-comms, and anomaly events — click one to jump to that vehicle.
            </li>
            <li>
              <b>Top bar:</b> current vehicle count and the{" "}
              <span className="mono">LIVE</span> /{" "}
              <span className="mono">RECONNECTING</span> connection status.
            </li>
          </ul>
          {demo && (
            <p className="about-demo">
              You're viewing <b>demo mode</b>: the fleet is generated in your
              browser, so everything works with no backend.
            </p>
          )}
        </div>
      )}
    </div>
  );
}
