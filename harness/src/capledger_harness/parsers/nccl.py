"""Parse the text output of NVIDIA nccl-tests (all_reduce_perf, alltoall_perf, ...).

A data line has 13 columns:

    size count type redop root | time algbw busbw #wrong (out-of-place) | time algbw busbw #wrong (in-place)

Some collectives print ``N/A`` in the #wrong columns. Bandwidths are GB/s,
times are microseconds.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

_AVG_RE = re.compile(r"#\s*Avg bus bandwidth\s*:\s*([0-9.]+)")
_RANK_RE = re.compile(r"^#\s+Rank\s+\d+\s", re.MULTILINE)
_OOB_RE = re.compile(r"#\s*Out of bounds values\s*:\s*(\d+)")


@dataclass(frozen=True)
class NcclRow:
    size_bytes: int
    count: int
    dtype: str
    redop: str
    root: int
    oop_time_us: float
    oop_algbw: float
    oop_busbw: float
    oop_wrong: int | None
    ip_time_us: float
    ip_algbw: float
    ip_busbw: float
    ip_wrong: int | None


@dataclass(frozen=True)
class NcclResult:
    rows: tuple[NcclRow, ...]
    avg_busbw: float | None
    out_of_bounds: int | None
    ranks: int | None

    @property
    def wrong_total(self) -> int:
        return sum((r.oop_wrong or 0) + (r.ip_wrong or 0) for r in self.rows)

    @property
    def peak_busbw(self) -> float:
        return max((max(r.oop_busbw, r.ip_busbw) for r in self.rows), default=0.0)

    @property
    def busbw_at_max_size(self) -> float:
        """Out-of-place bus bandwidth at the largest message size tested."""
        if not self.rows:
            return 0.0
        return max(self.rows, key=lambda r: r.size_bytes).oop_busbw

    @property
    def passed_validation(self) -> bool:
        return self.wrong_total == 0 and (self.out_of_bounds in (None, 0))


def _wrong(token: str) -> int | None:
    return None if token.upper() == "N/A" else int(token)


def parse_nccl_output(text: str) -> NcclResult:
    rows: list[NcclRow] = []
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        tok = stripped.split()
        if len(tok) < 13 or not tok[0].isdigit() or not tok[1].isdigit():
            continue
        try:
            rows.append(
                NcclRow(
                    size_bytes=int(tok[0]),
                    count=int(tok[1]),
                    dtype=tok[2],
                    redop=tok[3],
                    root=int(tok[4]),
                    oop_time_us=float(tok[5]),
                    oop_algbw=float(tok[6]),
                    oop_busbw=float(tok[7]),
                    oop_wrong=_wrong(tok[8]),
                    ip_time_us=float(tok[9]),
                    ip_algbw=float(tok[10]),
                    ip_busbw=float(tok[11]),
                    ip_wrong=_wrong(tok[12]),
                )
            )
        except ValueError:
            continue
    avg = _AVG_RE.search(text)
    oob = _OOB_RE.search(text)
    ranks = len(_RANK_RE.findall(text)) or None
    return NcclResult(
        rows=tuple(rows),
        avg_busbw=float(avg.group(1)) if avg else None,
        out_of_bounds=int(oob.group(1)) if oob else None,
        ranks=ranks,
    )
