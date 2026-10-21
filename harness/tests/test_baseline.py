import json

from capledger_harness.baseline import find_outliers


def test_flags_slow_node_but_not_small_wobble(examples):
    metrics = json.loads((examples / "samples" / "node_metrics.json").read_text())
    found = find_outliers(metrics)
    assert [(o.node, o.metric) for o in found] == [("gpu-009", "gemm_bf16_tflops")]
    assert 0.10 < found[0].relative_gap < 0.12


def test_lower_is_better_metrics():
    metrics = {f"n{i}": {"latency_us": 10.0 + 0.1 * (i % 3)} for i in range(8)}
    metrics["n7"]["latency_us"] = 13.0
    found = find_outliers(metrics, higher_is_better={"latency_us": False})
    assert [o.node for o in found] == ["n7"]


def test_needs_enough_peers():
    assert find_outliers({"a": {"x": 100}, "b": {"x": 50}, "c": {"x": 100}}) == []


def test_identical_peers_use_relative_gap_only():
    metrics = {f"n{i}": {"x": 100.0} for i in range(6)}
    metrics["n5"]["x"] = 97.0  # 3% worse: below the 5% practical threshold
    assert find_outliers(metrics) == []
    metrics["n5"]["x"] = 90.0
    assert [o.node for o in find_outliers(metrics)] == ["n5"]
