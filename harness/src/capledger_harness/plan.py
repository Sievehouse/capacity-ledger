"""Build an acceptance-test plan from an inventory.

Stages run in order, from cheap and local to expensive and cluster-wide, so a
broken node is caught before it can spoil a multi-hour burn-in:

1. audit       inventory, driver and topology facts per node
2. single_node DCGM diagnostics per node
3. rdma        per-rail RDMA bandwidth between neighbouring nodes
4. collectives nccl-tests all-reduce and all-to-all at growing world sizes
5. burn_in     GPUs and fabric stressed at the same time for hours
6. storage     sequential read and write throughput per node

The plan only contains commands; nothing runs until ``runner.run_plan``.
"""

from __future__ import annotations

import shlex
from dataclasses import asdict, dataclass, field

from .inventory import Inventory

STAGES = ("audit", "single_node", "rdma", "collectives", "burn_in", "storage")

AUDIT_COMMAND = (
    "nvidia-smi --query-gpu=index,uuid,name,driver_version,pci.bus_id --format=csv,noheader; "
    "echo '--- topo'; nvidia-smi topo -m; "
    "echo '--- kernel'; uname -r; "
    "echo '--- rdma'; (ibstat 2>/dev/null || ibv_devinfo 2>/dev/null || echo 'no RDMA tools found')"
)


@dataclass(frozen=True)
class Task:
    id: str
    stage: str
    kind: str
    nodes: tuple[str, ...]
    command: str
    server_command: str | None = None
    timeout_s: int = 900
    meta: dict = field(default_factory=dict)


@dataclass
class Plan:
    cluster: str
    gpus_per_node: int
    nodes: tuple[str, ...]
    tasks: list[Task]

    def to_dict(self) -> dict:
        return {
            "apiVersion": "capacity-ledger/v1alpha1",
            "kind": "AcceptancePlan",
            "cluster": self.cluster,
            "gpusPerNode": self.gpus_per_node,
            "nodes": list(self.nodes),
            "tasks": [asdict(t) | {"nodes": list(t.nodes)} for t in self.tasks],
        }

    @staticmethod
    def from_dict(d: dict) -> "Plan":
        tasks = [Task(**(t | {"nodes": tuple(t["nodes"])})) for t in d["tasks"]]
        return Plan(cluster=d["cluster"], gpus_per_node=d["gpusPerNode"], nodes=tuple(d["nodes"]), tasks=tasks)

    def stage(self, name: str) -> list[Task]:
        return [t for t in self.tasks if t.stage == name]


def per_node(inv: Inventory, node: str, cmd: str) -> str:
    """Run a shell command on one node."""
    if inv.launcher == "slurm":
        return f"srun --nodes=1 --nodelist={node} --ntasks=1 bash -lc {shlex.quote(cmd)}"
    if inv.launcher == "mpirun":
        return f"ssh {node} {shlex.quote(cmd)}"
    return f"bash -lc {shlex.quote(cmd)}"


def multi_node(inv: Inventory, nodes: tuple[str, ...], cmd: str, *, ranks_per_node: int) -> str:
    """Run one rank per GPU across several nodes."""
    if inv.launcher == "slurm":
        return (
            f"srun --nodes={len(nodes)} --nodelist={','.join(nodes)} "
            f"--ntasks-per-node={ranks_per_node} --gpus-per-node={inv.gpus_per_node} {cmd}"
        )
    if inv.launcher == "mpirun":
        hosts = ",".join(f"{n}:{ranks_per_node}" for n in nodes)
        env = " ".join(f"-x {k}={shlex.quote(v)}" for k, v in sorted(inv.extra_env.items()))
        return f"mpirun -np {len(nodes) * ranks_per_node} -H {hosts} --bind-to none -x NCCL_DEBUG=WARN {env} {cmd}".replace("  ", " ")
    raise ValueError("multi-node stages need the slurm or mpirun launcher")


def world_sizes(n: int) -> list[int]:
    """Powers of two from 2 up to n, plus n itself."""
    sizes, s = [], 2
    while s <= n:
        sizes.append(s)
        s *= 2
    if n >= 2 and n not in sizes:
        sizes.append(n)
    return sizes


def build_plan(inv: Inventory) -> Plan:
    tasks: list[Task] = []
    nodes = inv.nodes

    for n in nodes:
        tasks.append(Task(f"audit:{n}", "audit", "audit", (n,), per_node(inv, n, AUDIT_COMMAND), timeout_s=300))
    for n in nodes:
        tasks.append(Task(f"dcgm:{n}", "single_node", "dcgm", (n,), per_node(inv, n, "dcgmi diag -r 3"), timeout_s=3600))

    if inv.rdma_devices and len(nodes) > 1 and inv.launcher != "local":
        pairs = [(nodes[i], nodes[(i + 1) % len(nodes)]) for i in range(len(nodes))]
        if len(nodes) == 2:
            pairs = pairs[:1]
        for server, client in pairs:
            for dev in inv.rdma_devices:
                base = f"ib_write_bw -d {dev} --report_gbits -F -q 4 -D 10"
                tasks.append(
                    Task(
                        f"rdma:{dev}:{server}->{client}",
                        "rdma",
                        "rdma_pair",
                        (server, client),
                        per_node(inv, client, f"{base} {server}"),
                        server_command=per_node(inv, server, base),
                        timeout_s=120,
                        meta={"device": dev},
                    )
                )

    if len(nodes) > 1 and inv.launcher != "local":
        for size in world_sizes(len(nodes)):
            group = nodes[:size]
            for op, binary in (("all_reduce", "all_reduce_perf"), ("alltoall", "alltoall_perf")):
                cmd = f"{inv.nccl_tests_dir}/{binary} -b 8 -e 8G -f 2 -g 1"
                tasks.append(
                    Task(
                        f"nccl:{op}:{size}n",
                        "collectives",
                        "nccl",
                        group,
                        multi_node(inv, group, cmd, ranks_per_node=inv.gpus_per_node),
                        timeout_s=1800,
                        meta={"op": op, "world_nodes": size},
                    )
                )
        burn = f"{inv.python} {inv.burnin_script} --minutes {inv.burn_in_minutes} --out burnin-{inv.cluster}.jsonl"
        tasks.append(
            Task(
                "burn_in:all",
                "burn_in",
                "burnin",
                nodes,
                multi_node(inv, nodes, burn, ranks_per_node=inv.gpus_per_node),
                timeout_s=inv.burn_in_minutes * 60 + 900,
                meta={"minutes": inv.burn_in_minutes},
            )
        )

    for path in inv.storage_paths:
        for n in nodes:
            for rw in ("write", "read"):
                fio = (
                    f"fio --name=seq{rw} --directory={shlex.quote(path)} --rw={rw} --bs=1M --size=8G "
                    "--numjobs=8 --direct=1 --group_reporting --output-format=json"
                )
                tasks.append(Task(f"storage:{rw}:{n}:{path}", "storage", "fio", (n,), per_node(inv, n, fio), timeout_s=1800, meta={"rw": rw, "path": path}))

    return Plan(cluster=inv.cluster, gpus_per_node=inv.gpus_per_node, nodes=nodes, tasks=tasks)
