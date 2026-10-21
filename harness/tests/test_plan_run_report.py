import json

import pytest

from capledger_harness.contract_convert import convert
from capledger_harness.inventory import Inventory
from capledger_harness.plan import Plan, build_plan, world_sizes
from capledger_harness.report import Criteria, build_report, to_markdown
from capledger_harness.runner import Completed, load_results, run_plan


def test_world_sizes():
    assert world_sizes(4) == [2, 4]
    assert world_sizes(6) == [2, 4, 6]
    assert world_sizes(1) == []


def test_plan_from_example_inventory(examples):
    inv = Inventory.load(examples / "inventory" / "hgx-4-node.yaml")
    plan = build_plan(inv)
    assert len(plan.stage("audit")) == 4
    assert len(plan.stage("single_node")) == 4
    assert len(plan.stage("rdma")) == 4 * 8  # ring of 4 pairs x 8 rails
    assert [t.id for t in plan.stage("collectives")] == ["nccl:all_reduce:2n", "nccl:alltoall:2n", "nccl:all_reduce:4n", "nccl:alltoall:4n"]
    assert len(plan.stage("burn_in")) == 1
    assert len(plan.stage("storage")) == 8
    four = plan.stage("collectives")[2]
    assert "--nodelist=gpu-001,gpu-002,gpu-003,gpu-004" in four.command and "--ntasks-per-node=8" in four.command
    # Plans survive a JSON round trip.
    again = Plan.from_dict(json.loads(json.dumps(plan.to_dict())))
    assert [t.id for t in again.tasks] == [t.id for t in plan.tasks]


def test_inventory_validation():
    with pytest.raises(ValueError):
        Inventory.from_dict({"cluster": "x", "launcher": "slurm", "nodes": ["a", "a"], "gpus_per_node": 8})
    with pytest.raises(ValueError):
        Inventory.from_dict({"cluster": "x", "launcher": "slurm", "nodes": ["a"], "gpus_per_node": 8, "typo": 1})


def fake_executor(examples):
    nccl = (examples / "samples" / "nccl_all_reduce_4node.txt").read_text()
    smi = (examples / "samples" / "nvidia-smi.csv").read_text()

    def run(task):
        if task.kind == "audit":
            # gpu-003 shows only 7 GPUs.
            out = smi if task.nodes[0] != "gpu-003" else "\n".join(smi.splitlines()[:7])
            return Completed(0, out, "")
        if task.kind == "dcgm":
            return Completed(1 if task.nodes[0] == "gpu-002" else 0, "", "")
        if task.kind == "nccl":
            return Completed(0, nccl, "")
        return Completed(0, "ok", "")

    return run


def test_run_and_report_end_to_end(tmp_path, examples):
    inv = Inventory.load(examples / "inventory" / "hgx-4-node.yaml")
    plan = build_plan(inv)
    out = tmp_path / "results.jsonl"
    run_plan(plan, out, stages=["audit", "single_node", "collectives"], executor=fake_executor(examples))
    results = load_results(out)
    assert {r["stage"] for r in results} == {"audit", "single_node", "collectives"}

    crit = Criteria(nccl_min_busbw_gbps={"all_reduce": {2: 340, 4: 360}})
    rep = build_report(results, crit, cluster=inv.cluster, expected_gpus=8)
    verdicts = {row["node"]: row["verdict"] for row in rep["nodes"]}
    assert verdicts["gpu-002"] == "reject"  # DCGM failed
    assert verdicts["gpu-003"] == "reject"  # a GPU is missing
    # 351.84 GB/s misses the 360 floor for 4 nodes, so every node in that group needs review.
    assert verdicts["gpu-001"] == "review" and verdicts["gpu-004"] == "review"
    assert rep["summary"] == {"accept": 0, "review": 2, "reject": 2, "skipped_tasks": 0}
    md = to_markdown(rep)
    assert "| gpu-003 | reject | only 7 of 8 GPUs visible; nccl:all_reduce:4n" in md


def test_dry_run_records_commands(tmp_path, examples):
    plan = build_plan(Inventory.load(examples / "inventory" / "hgx-4-node.yaml"))
    results = run_plan(plan, tmp_path / "r.jsonl", dry_run=True)
    assert all(r.status == "skipped" for r in results)
    assert any("ib_write_bw" in r.command for r in results)
    # Nothing ran, so nothing may be accepted.
    rep = build_report(load_results(tmp_path / "r.jsonl"), Criteria(), cluster="x", expected_gpus=8)
    assert {row["verdict"] for row in rep["nodes"]} == {"review"}


def test_yaml_contract_matches_json(examples):
    data, warnings = convert((examples / "contracts" / "hgx-16-node.yaml").read_text())
    assert warnings == []
    assert data == json.loads((examples / "contracts" / "hgx-16-node.json").read_text())


def test_unquoted_decimal_is_fixed_with_warning():
    data, warnings = convert("credits:\n  tiers:\n    - {availabilityBelow: 99.9, creditPercent: 5}\n")
    assert data["credits"]["tiers"][0]["availabilityBelow"] == "99.9"
    assert len(warnings) == 1
