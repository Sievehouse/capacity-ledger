"""Execute an acceptance plan and record raw results.

Every task's command, exit code, timing, output and a SHA-256 of the output
are written as one JSON line, so results can be sealed into the evidence log
(``capledger evidence append --kind test-result``) and re-checked later.
"""

from __future__ import annotations

import hashlib
import json
import subprocess
import time
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable, Iterable

from .plan import Plan, Task


@dataclass
class Result:
    task_id: str
    stage: str
    kind: str
    nodes: list[str]
    command: str
    status: str  # ok | failed | timeout | skipped
    returncode: int | None
    started: str
    finished: str
    seconds: float
    stdout: str
    stderr: str
    stdout_sha256: str
    meta: dict


@dataclass
class Completed:
    returncode: int | None
    stdout: str
    stderr: str
    timed_out: bool = False


Executor = Callable[[Task], Completed]


def _now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def shell_executor(task: Task) -> Completed:
    """Run a task with the system shell. RDMA pairs start the server first."""
    server = None
    try:
        if task.server_command:
            server = subprocess.Popen(task.server_command, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            time.sleep(2)
        proc = subprocess.run(task.command, shell=True, capture_output=True, text=True, timeout=task.timeout_s)
        out, err = proc.stdout, proc.stderr
        if server is not None:
            s_out, s_err = server.communicate(timeout=30)
            out += "\n--- server ---\n" + s_out
            err += s_err
        return Completed(proc.returncode, out, err)
    except subprocess.TimeoutExpired as e:
        return Completed(None, e.stdout or "", e.stderr or "", timed_out=True)
    finally:
        if server is not None and server.poll() is None:
            server.kill()


def run_plan(
    plan: Plan,
    out_path: str | Path,
    *,
    dry_run: bool = False,
    stages: Iterable[str] | None = None,
    executor: Executor = shell_executor,
) -> list[Result]:
    wanted = set(stages) if stages else None
    results: list[Result] = []
    out_path = Path(out_path)
    with out_path.open("w", encoding="utf-8") as f:
        for task in plan.tasks:
            if wanted is not None and task.stage not in wanted:
                continue
            started = _now()
            t0 = time.monotonic()
            if dry_run:
                done = Completed(None, "", "")
                status = "skipped"
            else:
                done = executor(task)
                if done.timed_out:
                    status = "timeout"
                else:
                    status = "ok" if done.returncode == 0 else "failed"
            res = Result(
                task_id=task.id,
                stage=task.stage,
                kind=task.kind,
                nodes=list(task.nodes),
                command=task.command if not task.server_command else f"{task.server_command} & {task.command}",
                status=status,
                returncode=done.returncode,
                started=started,
                finished=_now(),
                seconds=round(time.monotonic() - t0, 3),
                stdout=done.stdout,
                stderr=done.stderr,
                stdout_sha256=hashlib.sha256(done.stdout.encode()).hexdigest(),
                meta=dict(task.meta),
            )
            results.append(res)
            f.write(json.dumps(asdict(res), sort_keys=True) + "\n")
            f.flush()
    return results


def load_results(path: str | Path) -> list[dict]:
    with open(path, encoding="utf-8") as f:
        return [json.loads(line) for line in f if line.strip()]
