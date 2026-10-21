from capledger_harness.parsers import count_gpus, parse_csv, parse_nccl_output


def test_nccl_sample(examples):
    res = parse_nccl_output((examples / "samples" / "nccl_all_reduce_4node.txt").read_text())
    assert len(res.rows) == 14
    assert res.ranks == 32
    assert res.avg_busbw == 184.991
    assert res.busbw_at_max_size == 351.84
    assert res.peak_busbw == 353.25
    assert res.wrong_total == 0 and res.passed_validation


def test_nccl_wrong_results_and_na():
    text = (
        "   8388608   2097152  float  sum  -1  466.5  17.98  34.84  3  464.7  18.05  34.98  0\n"
        "  16777216   4194304  float  none -1  512.3  32.75  63.45  0  510.3  32.88  63.71  N/A\n"
        "# Out of bounds values : 3 FAILED\n"
    )
    res = parse_nccl_output(text)
    assert res.wrong_total == 3
    assert res.rows[1].ip_wrong is None
    assert not res.passed_validation


def test_nvidia_smi_csv(examples):
    text = (examples / "samples" / "nvidia-smi.csv").read_text()
    assert count_gpus(text) == 8
    assert count_gpus(text + "Unable to determine the device handle for GPU 0000:3B:00.0: Unknown Error\n") == 8
    rows = parse_csv(text, ["index", "uuid", "name", "driver", "pci"])
    assert rows[7]["pci"] == "00000000:DB:00.0"
