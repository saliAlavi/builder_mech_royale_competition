#!/usr/bin/env python3
"""Render a mechsim replay: building footprints, zone circles, mech trajectories.

usage: python3 plot_replay.py replay_match0.jsonl [--world ../kit/arena/v2/world.json] [-o out.png]
       [--t0 SEC] [--t1 SEC] [--zoom-ext]   (zoom to the external bot's path)
"""
import argparse
import json
import os
import sys

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402
from matplotlib.patches import Circle, Polygon  # noqa: E402


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    ap = argparse.ArgumentParser()
    ap.add_argument("replay")
    ap.add_argument("--world", default=os.path.join(here, "..", "kit", "arena", "v2", "world.json"))
    ap.add_argument("-o", "--out", default=None)
    ap.add_argument("--t0", type=float, default=0)
    ap.add_argument("--t1", type=float, default=1e9)
    ap.add_argument("--zoom-ext", action="store_true")
    ap.add_argument("--stuck", default=None, help="results.json to overlay stuck spots for this match")
    a = ap.parse_args()

    meta = {}
    frames = []
    with open(a.replay) as f:
        for line in f:
            d = json.loads(line)
            if d.get("type") == "meta":
                meta = d
            elif a.t0 <= d["t"] <= a.t1:
                frames.append(d)
    if not frames:
        sys.exit("no frames")
    ext = set(meta.get("external", []))
    names = {int(k): v for k, v in meta.get("names", {}).items()}

    w = json.load(open(a.world))
    fig, ax = plt.subplots(figsize=(16, 11), dpi=110)
    colors = {"building": "#9a9a9a", "stadium": "#b08a60", "bridge": "#6fa0c8"}
    for p in w["prisms"]:
        r = p["ring"]
        pts = list(zip(r[0::2], r[1::2]))
        ax.add_patch(Polygon(pts, closed=True, fc=colors.get(p["kind"], "#999"), ec="#555", lw=0.3, alpha=0.75 if p["kind"] != "bridge" else 0.45))
        for h in p.get("holes", []):
            hp = list(zip(h[0::2], h[1::2]))
            ax.add_patch(Polygon(hp, closed=True, fc="white", ec="#555", lw=0.3))
    # zone circles at a few times
    seen = set()
    for fr in frames[:: max(1, len(frames) // 12)] + [frames[-1]]:
        z = fr["zone"]
        key = (round(z["center"][0]), round(z["center"][1]), round(z["radius"]))
        if key in seen:
            continue
        seen.add(key)
        ax.add_patch(Circle(z["center"], z["radius"], fill=False, ec="#3070ff", lw=0.8, alpha=0.6, ls="--"))
        ax.text(z["center"][0] + z["radius"] * 0.7, z["center"][1] + z["radius"] * 0.7, f"t={fr['t']:.0f}", color="#3070ff", fontsize=6, clip_on=True)

    tracks = {}
    for fr in frames:
        for m in fr["mechs"]:
            tr = tracks.setdefault(m["id"], {"x": [], "y": [], "alive": True, "last": None})
            if m["alive"]:
                tr["x"].append(m["pos"][0])
                tr["y"].append(m["pos"][1])
                tr["last"] = (m["pos"][0], m["pos"][1], fr["t"])
            elif tr["alive"]:
                tr["alive"] = False
                tr["death"] = tr["last"]
    cmap = plt.get_cmap("tab20")
    for mid, tr in sorted(tracks.items()):
        if not tr["x"]:
            continue
        isext = mid in ext
        c = "#ff2020" if isext else cmap(mid % 20)
        ax.plot(tr["x"], tr["y"], "-", color=c, lw=2.2 if isext else 0.8, alpha=1 if isext else 0.7, zorder=5 if isext else 3)
        ax.plot(tr["x"][0], tr["y"][0], "o", color=c, ms=6 if isext else 3, zorder=6)
        lab = f"{mid}" + (f" {names.get(mid, '')}" if isext else "")
        ax.text(tr["x"][0], tr["y"][0], lab, fontsize=7 if isext else 5, color=c, zorder=7, clip_on=True)
        if not tr["alive"] and tr.get("death"):
            dx, dy, dt = tr["death"]
            ax.plot(dx, dy, "x", color=c, ms=9 if isext else 5, mew=2, zorder=7)
            if isext:
                ax.text(dx, dy, f" died t={dt:.0f}", color=c, fontsize=8, zorder=8, clip_on=True)
    if a.stuck:
        res = json.load(open(a.stuck))
        mi = meta.get("match")
        for r in res:
            if r and r.get("match") == mi:
                for s in r["slots"]:
                    for sp in s.get("stuck_spots") or []:
                        ax.plot(sp[0], sp[1], "s", mfc="none", mec="#ff00ff", ms=10, mew=1.5, zorder=9)
    b = w["bounds"]
    if a.zoom_ext and ext:
        xs = [x for mid in ext for x in tracks.get(mid, {}).get("x", [])]
        ys = [y for mid in ext for y in tracks.get(mid, {}).get("y", [])]
        if xs:
            pad = 40
            ax.set_xlim(min(xs) - pad, max(xs) + pad)
            ax.set_ylim(min(ys) - pad, max(ys) + pad)
    else:
        ax.set_xlim(b["minX"], b["maxX"])
        ax.set_ylim(b["minY"], b["maxY"])
    ax.set_aspect("equal")
    ax.set_title(f"match {meta.get('match')} seed {meta.get('seed')}  t={frames[0]['t']:.0f}..{frames[-1]['t']:.0f}  (red = external)")
    out = a.out or os.path.splitext(a.replay)[0] + ".png"
    fig.subplots_adjust(left=0.05, right=0.98, top=0.95, bottom=0.05)
    fig.savefig(out)
    print(out)


if __name__ == "__main__":
    main()
