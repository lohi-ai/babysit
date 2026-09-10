import importlib.util
from pathlib import Path


MODULE = Path(__file__).parent / "ticket-system" / "run_autopilot_v2_eval.py"
SPEC = importlib.util.spec_from_file_location("run_autopilot_v2_eval", MODULE)
assert SPEC and SPEC.loader
EVAL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(EVAL)


def test_classify_cases_separates_local_and_live_coverage():
    report = EVAL.classify_cases(
        [
            {"name": "P1", "binary_test": ["explain"]},
            {"name": "P2"},
            {"name": EVAL.LIVE_ONLY_CASE},
        ]
    )

    assert report["fixture_count"] == 3
    assert report["local"]["binary"] == ["P1"]
    assert report["local"]["wiring"] == ["P2"]
    assert report["local"]["unavailable"] == [EVAL.LIVE_ONLY_CASE]
    assert report["live"]["eligible"] == ["P2", EVAL.LIVE_ONLY_CASE]
    assert report["live"]["executed"] == []
    assert report["live"]["provider_usage"] is None


def test_contract_audit_marks_legacy_oracles():
    report = EVAL.classify_cases([])

    assert report["contract_audit"]["legacy_oracle_rows"] == [
        "P2-inline-new-feature-dirty",
        "P4-implement-unplanned",
        "P5-implement-unapproved",
    ]


def test_cli_rejects_missing_evaluation_set(tmp_path, monkeypatch, capsys):
    monkeypatch.setattr(
        "sys.argv",
        ["run_autopilot_v2_eval.py", "--eval-set", str(tmp_path / "missing.json")],
    )

    try:
        EVAL.main()
    except SystemExit as error:
        assert error.code == 2
    else:
        raise AssertionError("missing evaluation set should exit")

    assert "cannot read evaluation set" in capsys.readouterr().err
