"""Flag nodes that are slow compared with their peers.

Functional tests (does NCCL run, does DCGM pass) miss nodes that work but
underperform. This module compares each node's metric with the median of the
same metric across its peers, using the robust z-score of Iglewicz and Hoaglin:

    z = 0.6745 * (x - median) / MAD

A node is flagged only when it is both statistically unusual (|z| above the
threshold, in the bad direction) and practically slower (at least
``rel_threshold`` worse than the median). Requiring both avoids flagging a
0.5% wobble in a very uniform fleet, where MAD is tiny.
"""

from __future__ import annotations

import statistics
from dataclasses import dataclass


@dataclass(frozen=True)
class Outlier:
    node: str
    metric: str
    value: float
    median: float
    robust_z: float | None
    relative_gap: float  # positive = worse than the median, as a fraction
    reason: str


def robust_stats(values: list[float]) -> tuple[float, float]:
    """Return (median, median absolute deviation)."""
    med = statistics.median(values)
    mad = statistics.median([abs(v - med) for v in values])
    return med, mad


def clean_metrics(raw: dict) -> dict[str, dict[str, float]]:
    """Keep only node -> {metric: number} entries; skip comments and junk."""
    out: dict[str, dict[str, float]] = {}
    for node, values in raw.items():
        if str(node).startswith("_") or not isinstance(values, dict):
            continue
        nums = {k: float(v) for k, v in values.items() if isinstance(v, (int, float)) and not isinstance(v, bool)}
        if nums:
            out[str(node)] = nums
    return out


def find_outliers(
    metrics: dict[str, dict[str, float]],
    *,
    higher_is_better: dict[str, bool] | None = None,
    z_threshold: float = 3.5,
    rel_threshold: float = 0.05,
    min_peers: int = 4,
) -> list[Outlier]:
    """Return outliers for every metric with at least ``min_peers`` values.

    ``metrics`` maps node -> {metric name -> value}. Metrics default to
    higher-is-better; pass ``higher_is_better={"latency_us": False}`` for
    lower-is-better metrics.
    """
    higher_is_better = higher_is_better or {}
    metrics = clean_metrics(metrics)
    names = sorted({m for per_node in metrics.values() for m in per_node})
    out: list[Outlier] = []
    for name in names:
        values = {n: v[name] for n, v in metrics.items() if name in v}
        if len(values) < min_peers:
            continue
        med, mad = robust_stats(list(values.values()))
        if med == 0:
            continue
        better_high = higher_is_better.get(name, True)
        for node in sorted(values):
            x = values[node]
            gap = (med - x) / abs(med) if better_high else (x - med) / abs(med)
            if gap < rel_threshold:
                continue
            if mad == 0:
                out.append(Outlier(node, name, x, med, None, gap, f"{gap:.1%} worse than the peer median; peers are identical"))
                continue
            z = 0.6745 * (x - med) / mad
            bad_z = -z if better_high else z
            if bad_z >= z_threshold:
                out.append(Outlier(node, name, x, med, z, gap, f"{gap:.1%} worse than the peer median (robust z {z:+.1f})"))
    return out
