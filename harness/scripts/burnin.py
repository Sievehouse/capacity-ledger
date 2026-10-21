"""Simultaneous GPU and fabric burn-in (reference implementation).

Runs large GEMMs on every GPU while all-to-all traffic crosses the fabric at
the same time, because stressing the two separately misses failures that
only appear under combined thermal and electrical load. Every reporting
interval, rank 0 writes one JSON line with per-rank GEMM TFLOPS and all-to-all
bandwidth so slow GPUs and links stand out (see capledger_harness.baseline).

Launch one process per GPU, for example:

    srun --nodes=4 --ntasks-per-node=8 --gpus-per-node=8 python3 burnin.py --minutes 480
    torchrun --nnodes=4 --nproc-per-node=8 ... burnin.py --minutes 480

Status: reference code, not yet validated on hardware in this repository.
Watch the kernel log (capledger agent) during the run; XIDs during burn-in
are the most important output.
"""

from __future__ import annotations

import argparse
import json
import os
import socket
import statistics
import time


def _rank_env() -> tuple[int, int, int]:
    """Return (rank, world size, local rank) from torchrun or Slurm variables."""
    if "RANK" in os.environ:
        return int(os.environ["RANK"]), int(os.environ["WORLD_SIZE"]), int(os.environ.get("LOCAL_RANK", 0))
    if "SLURM_PROCID" in os.environ:
        return int(os.environ["SLURM_PROCID"]), int(os.environ["SLURM_NTASKS"]), int(os.environ.get("SLURM_LOCALID", 0))
    return 0, 1, 0


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("--minutes", type=float, default=480)
    p.add_argument("--gemm-size", type=int, default=8192)
    p.add_argument("--gemm-iters", type=int, default=20, help="GEMMs per step")
    p.add_argument("--a2a-mib", type=int, default=512, help="all-to-all buffer per rank (MiB)")
    p.add_argument("--report-every", type=float, default=60, help="seconds between report lines")
    p.add_argument("--dtype", choices=("bf16", "fp16", "fp32"), default="bf16")
    p.add_argument("--out", default="burnin-results.jsonl")
    args = p.parse_args()

    import torch
    import torch.distributed as dist

    rank, world, local_rank = _rank_env()
    os.environ.setdefault("RANK", str(rank))
    os.environ.setdefault("WORLD_SIZE", str(world))
    os.environ.setdefault("MASTER_ADDR", os.environ.get("MASTER_ADDR", "localhost"))
    os.environ.setdefault("MASTER_PORT", os.environ.get("MASTER_PORT", "29500"))
    torch.cuda.set_device(local_rank)
    dist.init_process_group(backend="nccl", rank=rank, world_size=world)

    dtype = {"bf16": torch.bfloat16, "fp16": torch.float16, "fp32": torch.float32}[args.dtype]
    n = args.gemm_size
    a = torch.randn(n, n, device="cuda", dtype=dtype)
    b = torch.randn(n, n, device="cuda", dtype=dtype)
    c = torch.empty(n, n, device="cuda", dtype=dtype)

    elem = 2  # bfloat16 bytes
    count = (args.a2a_mib * 1024 * 1024 // elem) // world * world
    send = torch.randn(count, device="cuda", dtype=torch.bfloat16)
    recv = torch.empty_like(send)

    gemm_stream, comm_stream = torch.cuda.Stream(), torch.cuda.Stream()
    deadline = time.time() + args.minutes * 60
    next_report = time.time() + args.report_every
    tflops, gbps = [], []

    while time.time() < deadline:
        g0, g1 = torch.cuda.Event(enable_timing=True), torch.cuda.Event(enable_timing=True)
        c0, c1 = torch.cuda.Event(enable_timing=True), torch.cuda.Event(enable_timing=True)
        with torch.cuda.stream(gemm_stream):
            g0.record()
            for _ in range(args.gemm_iters):
                torch.mm(a, b, out=c)
            g1.record()
        with torch.cuda.stream(comm_stream):
            c0.record()
            dist.all_to_all_single(recv, send)
            c1.record()
        torch.cuda.synchronize()
        gemm_s = g0.elapsed_time(g1) / 1000
        comm_s = c0.elapsed_time(c1) / 1000
        tflops.append(2 * n**3 * args.gemm_iters / gemm_s / 1e12)
        # Bytes each rank sends to other ranks per all-to-all.
        gbps.append(count * elem * (world - 1) / world / comm_s / 1e9)

        if time.time() >= next_report:
            mine = {"host": socket.gethostname(), "rank": rank, "local_rank": local_rank,
                    "gemm_tflops_median": statistics.median(tflops), "gemm_tflops_min": min(tflops),
                    "a2a_gbps_median": statistics.median(gbps), "a2a_gbps_min": min(gbps), "steps": len(tflops)}
            gathered = [None] * world
            dist.all_gather_object(gathered, mine)
            if rank == 0:
                with open(args.out, "a", encoding="utf-8") as f:
                    f.write(json.dumps({"ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "ranks": gathered}) + "\n")
            tflops, gbps = [], []
            next_report = time.time() + args.report_every

    dist.barrier()
    dist.destroy_process_group()


if __name__ == "__main__":
    main()
