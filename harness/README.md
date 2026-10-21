# capledger-harness

The Python half of Capacity Ledger: plans and runs cluster acceptance tests with standard tools, parses their output, compares nodes with their peers and localises faulty nodes.

```bash
pip install -e "harness[dev]"

capledger-harness plan --inventory ../examples/inventory/hgx-4-node.yaml --out plan.json
capledger-harness run --plan plan.json --out results.jsonl --dry-run
capledger-harness report --results results.jsonl --cluster example-hgx-32gpu --expected-gpus 8 \
  --criteria ../examples/criteria/hgx-h100-400g.yaml --metrics ../examples/samples/node_metrics.json \
  --out report.json --markdown report.md
capledger-harness outliers --metrics ../examples/samples/node_metrics.json
capledger-harness localize-sim --nodes 64 --bad 2
capledger-harness parse-nccl ../examples/samples/nccl_all_reduce_4node.txt
capledger-harness convert-contract --in ../examples/contracts/hgx-16-node.yaml --out contract.json
```

Seal results into the evidence log with the Go CLI:

```bash
capledger evidence append --log evidence.jsonl --key keys/capledger.key.pem --kind acceptance-report --payload report.json
```

`scripts/burnin.py` needs PyTorch on GPU nodes (`pip install -e "harness[burnin]"`). It is reference code and has not yet been validated on hardware in this repository.

Tests: `pytest -q` (no GPUs needed; tool output is simulated).
