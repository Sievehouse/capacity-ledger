from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[2]


@pytest.fixture
def examples() -> Path:
    return REPO / "examples"
