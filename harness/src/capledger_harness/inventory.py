"""Describe the cluster under test."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path

import yaml

LAUNCHERS = ("slurm", "mpirun", "local")


@dataclass(frozen=True)
class Inventory:
    """What the acceptance plan needs to know about a cluster.

    launcher: how multi-node jobs start. ``slurm`` uses srun for everything;
    ``mpirun`` uses mpirun for multi-node jobs and ssh for per-node commands;
    ``local`` runs single-node commands on this machine (for development).
    """

    cluster: str
    launcher: str
    nodes: tuple[str, ...]
    gpus_per_node: int
    rdma_devices: tuple[str, ...] = ()
    nccl_tests_dir: str = "/opt/nccl-tests/build"
    burn_in_minutes: int = 480
    storage_paths: tuple[str, ...] = ()
    python: str = "python3"
    burnin_script: str = "harness/scripts/burnin.py"
    extra_env: dict[str, str] = field(default_factory=dict)

    @staticmethod
    def from_dict(d: dict) -> "Inventory":
        required = ("cluster", "launcher", "nodes", "gpus_per_node")
        missing = [k for k in required if k not in d]
        if missing:
            raise ValueError(f"inventory is missing: {', '.join(missing)}")
        known = set(Inventory.__dataclass_fields__)
        unknown = sorted(set(d) - known)
        if unknown:
            raise ValueError(f"inventory has unknown keys: {', '.join(unknown)}")
        nodes = tuple(str(n) for n in d["nodes"])
        if len(set(nodes)) != len(nodes) or not nodes:
            raise ValueError("inventory nodes must be non-empty and unique")
        if d["launcher"] not in LAUNCHERS:
            raise ValueError(f"launcher must be one of {LAUNCHERS}")
        if int(d["gpus_per_node"]) < 1:
            raise ValueError("gpus_per_node must be at least 1")
        if d["launcher"] == "local" and len(nodes) != 1:
            raise ValueError("the local launcher supports exactly one node")
        return Inventory(
            cluster=str(d["cluster"]),
            launcher=d["launcher"],
            nodes=nodes,
            gpus_per_node=int(d["gpus_per_node"]),
            rdma_devices=tuple(d.get("rdma_devices", ())),
            nccl_tests_dir=d.get("nccl_tests_dir", "/opt/nccl-tests/build"),
            burn_in_minutes=int(d.get("burn_in_minutes", 480)),
            storage_paths=tuple(d.get("storage_paths", ())),
            python=d.get("python", "python3"),
            burnin_script=d.get("burnin_script", "harness/scripts/burnin.py"),
            extra_env=dict(d.get("extra_env", {})),
        )

    @staticmethod
    def load(path: str | Path) -> "Inventory":
        with open(path, encoding="utf-8") as f:
            return Inventory.from_dict(yaml.safe_load(f) or {})
