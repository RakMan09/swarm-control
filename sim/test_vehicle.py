"""Determinism and fault-behaviour tests for the vehicle simulator.

Run with: python -m unittest discover -s sim
"""
import unittest

from vehicle import (
    FAULT_DRAIN,
    FAULT_NONE,
    FAULT_SILENT,
    FAULT_STRAY,
    Vehicle,
)

ORIGIN_LAT, ORIGIN_LON, AREA = 37.5, -122.05, 0.08


def run(seed_index, base_seed, steps=50, dt=1.0, fault=FAULT_NONE):
    v = Vehicle.spawn(seed_index, base_seed, ORIGIN_LAT, ORIGIN_LON, AREA, fault)
    out = []
    for _ in range(steps):
        v.step(dt)
        out.append((round(v.lat, 6), round(v.lon, 6), round(v.battery_pct, 3)))
    return out


class DeterminismTest(unittest.TestCase):
    def test_same_seed_is_reproducible(self):
        self.assertEqual(run(3, 42), run(3, 42))

    def test_different_seed_diverges(self):
        self.assertNotEqual(run(3, 42), run(4, 42))

    def test_different_base_seed_diverges(self):
        self.assertNotEqual(run(3, 42), run(3, 7))


class FaultBehaviourTest(unittest.TestCase):
    def test_drain_fault_drains_faster(self):
        normal = Vehicle.spawn(1, 42, ORIGIN_LAT, ORIGIN_LON, AREA, FAULT_NONE)
        drain = Vehicle.spawn(1, 42, ORIGIN_LAT, ORIGIN_LON, AREA, FAULT_DRAIN)
        for _ in range(30):
            normal.step(1.0)
            drain.step(1.0)
        self.assertLess(drain.battery_pct, normal.battery_pct)

    def test_stray_leaves_bounding_box(self):
        v = Vehicle.spawn(2, 42, ORIGIN_LAT, ORIGIN_LON, AREA, FAULT_STRAY)
        for _ in range(1500):  # heading east at >=12 m/s until clear of the fence
            v.step(1.0)
        # Default fence east edge is lon -121.90; a stray must exceed it.
        self.assertGreater(v.lon, -121.90)

    def test_silent_goes_dark(self):
        v = Vehicle.spawn(5, 42, ORIGIN_LAT, ORIGIN_LON, AREA, FAULT_SILENT)
        self.assertFalse(v.is_silent())
        for _ in range(60):
            v.step(1.0)
        self.assertTrue(v.is_silent())

    def test_rtb_sets_returning_status(self):
        v = Vehicle.spawn(6, 42, ORIGIN_LAT, ORIGIN_LON, AREA, FAULT_STRAY)
        state = v.apply_command("RTB", None, ORIGIN_LAT, ORIGIN_LON)
        self.assertEqual(state, "applied")
        self.assertEqual(v.status, "returning")
        self.assertEqual(v.fault, FAULT_NONE)


if __name__ == "__main__":
    unittest.main()
