"""Plot a simulator replay: building footprints, zone circles, mech trajectories.
usage: python3 tools/plotrep.py replay.jsonl out.png [world.json]"""
import json, sys
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.patches import Polygon, Circle

rep, out = sys.argv[1], sys.argv[2]
wpath = sys.argv[3] if len(sys.argv) > 3 else "kit/arena/v2/world.json"
w = json.load(open(wpath))
fig, ax = plt.subplots(figsize=(14, 10))
for p in w["prisms"]:
    r = p["ring"]
    pts = list(zip(r[0::2], r[1::2]))
    col = {"building": "#999", "stadium": "#b9a", "bridge": "#9ab"}.get(p["kind"], "#aaa")
    ax.add_patch(Polygon(pts, closed=True, fc=col, ec="none", alpha=0.6))
lines = [json.loads(l) for l in open(rep)]
frames = [l for l in lines if l.get("type") != "meta"]
tracks, deaths = {}, {}
for f in frames:
    for m in f["mechs"]:
        if m["alive"]:
            tracks.setdefault(m["id"], []).append(m["pos"][:2])
        elif m["id"] not in deaths and m["id"] in tracks:
            deaths[m["id"]] = (tracks[m["id"]][-1], f["t"])
zs = {}
for f in frames:
    z = f["zone"]; zs[(round(z["center"][0]), round(z["center"][1]), round(z["radius"]))] = z
for z in list(zs.values())[:: max(1, len(zs) // 8)]:
    ax.add_patch(Circle(z["center"], z["radius"], fill=False, ec="#4af", lw=0.6))
cmap = plt.get_cmap("tab20")
for i, (mid, pts) in enumerate(sorted(tracks.items())):
    xs, ys = zip(*pts)
    ax.plot(xs, ys, "-", color=cmap(i % 20), lw=1.2)
    ax.plot(xs[0], ys[0], "o", color=cmap(i % 20), ms=4)
    if mid in deaths:
        (x, y), t = deaths[mid]
        ax.plot(x, y, "x", color="k", ms=7)
        ax.text(x, y, f"{mid}@{t:.0f}", fontsize=7)
ax.set_xlim(w["bounds"]["minX"], w["bounds"]["maxX"]); ax.set_ylim(w["bounds"]["minY"], w["bounds"]["maxY"])
ax.set_aspect("equal"); ax.set_title(rep.split("/")[-1])
fig.tight_layout(); fig.savefig(out, dpi=80)
