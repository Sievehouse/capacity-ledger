"""Convert a human-authored YAML contract spec to the JSON the engine reads.

Decimal fields must stay exact. YAML turns an unquoted ``99.9`` into a float,
so this converter turns floats in decimal fields back into their literal text
and warns; quoting them in the YAML avoids the warning.
"""

from __future__ import annotations

import json
from pathlib import Path

import yaml

DECIMAL_FIELDS = {"availabilityBelow", "monthlyFee"}


def _fix(node, warnings: list[str], path: str = ""):
    if isinstance(node, dict):
        out = {}
        for k, v in node.items():
            here = f"{path}.{k}" if path else str(k)
            if k in DECIMAL_FIELDS and isinstance(v, (int, float)) and not isinstance(v, bool):
                warnings.append(f"{here}: unquoted number {v!r} converted to a string; quote it in the YAML")
                v = repr(v) if isinstance(v, float) else str(v)
            out[k] = _fix(v, warnings, here)
        return out
    if isinstance(node, list):
        return [_fix(v, warnings, f"{path}[{i}]") for i, v in enumerate(node)]
    return node


def convert(yaml_text: str) -> tuple[dict, list[str]]:
    data = yaml.safe_load(yaml_text)
    if not isinstance(data, dict):
        raise ValueError("contract YAML must be a mapping")
    warnings: list[str] = []
    return _fix(data, warnings), warnings


def convert_file(src: str | Path, dst: str | Path) -> list[str]:
    data, warnings = convert(Path(src).read_text(encoding="utf-8"))
    Path(dst).write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
    return warnings
