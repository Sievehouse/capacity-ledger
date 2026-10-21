import math
import random

import pytest

from capledger_harness.localize import localize, simulate


def oracle(bad):
    return lambda group: not (set(group) & bad)


@pytest.mark.parametrize("seed", range(25))
def test_exact_oracle_finds_every_bad_node(seed):
    rng = random.Random(seed)
    n = rng.choice([8, 17, 64, 100])
    nodes = [f"n{i}" for i in range(n)]
    k = rng.randint(0, 3)
    bad = set(rng.sample(nodes, k))
    res = localize(nodes, oracle(bad))
    assert set(res.bad) == bad
    assert res.unresolved == ()
    # Binary splitting: about two tests per level per bad node, plus slack.
    bound = 1 + 2 * k * math.ceil(math.log2(n)) + 2 * k
    assert res.tests <= bound, (res.tests, bound)


def test_healthy_cluster_needs_one_test():
    res = localize([f"n{i}" for i in range(32)], oracle(set()))
    assert (res.bad, res.tests, res.rounds) == ((), 1, 1)


def test_all_bad_is_reported_unresolved_not_guessed():
    nodes = ["a", "b", "c", "d"]
    res = localize(nodes, oracle(set(nodes)))
    assert res.bad == ()
    assert set(res.unresolved) == set(nodes)


@pytest.mark.parametrize("seed", range(10))
def test_noisy_simulation_is_correct(seed):
    out = simulate(n_nodes=64, n_bad=2, degradation=0.20, noise=0.015, seed=seed)
    assert out["correct"], out
    assert out["logical_tests"] < out["all_pairs_tests_for_comparison"] / 50
