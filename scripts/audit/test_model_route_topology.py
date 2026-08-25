"""Cross-config routing contracts for model_route_topology.py."""

from __future__ import annotations

import json
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

from model_route_topology import ConfigShapeError, check_topology, main


def deneb_config(
    *,
    provider_id: str = "kimi",
    base_url: str = "http://127.0.0.1:18800",
    api: str = "",
    model_ids: tuple[str, ...] = ("k3",),
    default_model: str = "kimi/k3",
    hidden: tuple[str, ...] = (),
    extra_providers: dict[str, object] | None = None,
) -> dict[str, object]:
    provider: dict[str, object] = {
        "baseUrl": base_url,
        "models": [{"id": model_id} for model_id in model_ids],
    }
    if api:
        provider["api"] = api
    providers: dict[str, object] = {provider_id: provider}
    providers.update(extra_providers or {})
    return {
        "agents": {"defaultModel": default_model},
        "models": {
            "providers": providers,
            "hiddenModels": list(hidden),
        },
    }


def wormhole_config(*routes: dict[str, object]) -> dict[str, object]:
    return {"listen": ":18800", "models": list(routes)}


def route(
    name: str,
    *,
    protocol: str = "anthropic",
    upstream_model: str | None = None,
    url: str | None = "https://upstream.example/v1",
    fallback: str | None = None,
    metered: bool = False,
) -> dict[str, object]:
    value: dict[str, object] = {"name": name, "protocol": protocol}
    if upstream_model is not None:
        value["upstreamModel"] = upstream_model
    if url is not None:
        value["url"] = url
    if fallback is not None:
        value["fallback"] = fallback
    if metered:
        value["metered"] = True
    return value


class ModelRouteTopologyTests(unittest.TestCase):
    def codes(self, deneb: dict[str, object], wormhole: dict[str, object] | None) -> set[str]:
        return {finding.code for finding in check_topology(deneb, wormhole).findings}

    def test_valid_k3_route_allows_a_different_upstream_model(self) -> None:
        report = check_topology(
            deneb_config(),
            wormhole_config(route("k3", upstream_model="kimi-k3-production")),
        )

        self.assertTrue(report.ok, report.findings)

    def test_hidden_role_model_fails_even_when_the_route_exists(self) -> None:
        codes = self.codes(
            deneb_config(hidden=("kimi/k3",)),
            wormhole_config(route("k3")),
        )

        self.assertIn("ROLE_MODEL_HIDDEN", codes)

    def test_upstream_model_cannot_substitute_for_the_client_route_name(self) -> None:
        report = check_topology(
            deneb_config(),
            wormhole_config(route("k3[1m]", upstream_model="k3")),
        )

        missing = [
            finding for finding in report.findings if finding.code == "CLIENT_ROUTE_MISSING"
        ]
        self.assertEqual(len(missing), 1)
        self.assertIn("upstreamModel matches route 'k3[1m]'", missing[0].detail)

    def test_same_route_name_with_the_wrong_protocol_fails(self) -> None:
        codes = self.codes(
            deneb_config(),
            wormhole_config(route("k3", protocol="openai")),
        )

        self.assertIn("ROUTE_PROTOCOL_MISMATCH", codes)

    def test_hidden_direct_vllm_model_does_not_hide_the_wormhole_route(self) -> None:
        deneb = deneb_config(
            provider_id="wormhole",
            base_url="http://127.0.0.1:18800/v1",
            api="openai",
            model_ids=("deepseek-v4-flash", "dsv4-nothink"),
            default_model="wormhole/deepseek-v4-flash",
            hidden=("vllm/deepseek-v4-flash",),
            extra_providers={
                "vllm": {
                    "baseUrl": "http://127.0.0.1:8000/v1",
                    "models": [{"id": "deepseek-v4-flash"}],
                }
            },
        )
        wormhole = wormhole_config(
            route("deepseek-v4-flash", protocol="openai"),
            route("dsv4-nothink", protocol="openai"),
        )

        report = check_topology(deneb, wormhole)

        self.assertTrue(report.ok, report.findings)

    def test_anthropic_role_must_be_declared_for_picker_visibility(self) -> None:
        codes = self.codes(
            deneb_config(model_ids=("kimi-for-coding",)),
            wormhole_config(route("k3"), route("kimi-for-coding")),
        )

        self.assertIn("ANTHROPIC_ROLE_NOT_VISIBLE", codes)

    def test_missing_wormhole_config_fails_only_when_a_provider_targets_it(self) -> None:
        self.assertIn("WORMHOLE_CONFIG_MISSING", self.codes(deneb_config(), None))

        direct = deneb_config(
            base_url="https://api.kimi.com/coding",
            model_ids=("k3",),
        )
        self.assertTrue(check_topology(direct, None).ok)

    def test_anthropic_provider_rejects_the_openai_v1_base_path(self) -> None:
        codes = self.codes(
            deneb_config(base_url="http://127.0.0.1:18800/v1"),
            wormhole_config(route("k3")),
        )

        self.assertIn("PROVIDER_PATH_MISMATCH", codes)

    def test_openai_provider_rejects_a_trailing_slash_that_creates_a_double_slash(self) -> None:
        codes = self.codes(
            deneb_config(
                provider_id="wormhole",
                base_url="http://127.0.0.1:18800/v1/",
                api="openai",
                default_model="wormhole/k3",
            ),
            wormhole_config(route("k3", protocol="openai")),
        )

        self.assertIn("PROVIDER_PATH_MISMATCH", codes)

    def test_wormhole_route_name_whitespace_is_not_normalized_away(self) -> None:
        report = check_topology(
            deneb_config(),
            wormhole_config(route(" k3 ", upstream_model="k3")),
        )

        self.assertIn("CLIENT_ROUTE_MISSING", {finding.code for finding in report.findings})

    def test_duplicate_route_names_fail_because_the_last_entry_wins(self) -> None:
        codes = self.codes(
            deneb_config(),
            wormhole_config(route("k3"), route("k3")),
        )

        self.assertIn("ROUTE_NAME_DUPLICATE", codes)

    def test_visible_route_requires_a_static_or_fleet_backend(self) -> None:
        codes = self.codes(
            deneb_config(),
            wormhole_config(route("k3", url=None)),
        )

        self.assertIn("ROUTE_BACKEND_MISSING", codes)

    def test_main2_and_subagent_bindings_are_checked_for_hidden_models(self) -> None:
        deneb = deneb_config(default_model="kimi/k3")
        agents = deneb["agents"]
        assert isinstance(agents, dict)
        agents["main2Model"] = "kimi/k3-fast"
        agents["defaults"] = {"subagents": {"model": {"primary": "kimi/k3-sub"}}}
        models = deneb["models"]
        assert isinstance(models, dict)
        models["hiddenModels"] = ["kimi/k3-fast", "kimi/k3-sub"]

        codes = [
            finding.code
            for finding in check_topology(
                deneb,
                wormhole_config(route("k3"), route("k3-fast"), route("k3-sub")),
            ).findings
        ]

        self.assertEqual(codes.count("ROLE_MODEL_HIDDEN"), 2)

    def test_invalid_relevant_config_shape_is_not_silently_ignored(self) -> None:
        deneb = deneb_config()
        models = deneb["models"]
        assert isinstance(models, dict)
        models["hiddenModels"] = "kimi/k3"

        with self.assertRaisesRegex(ConfigShapeError, "hiddenModels must be an array"):
            check_topology(deneb, wormhole_config(route("k3")))

    def test_cli_returns_two_for_malformed_json_without_echoing_contents(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            deneb_path = root / "deneb.json"
            wormhole_path = root / "wormhole.json"
            deneb_path.write_text('{"sentinel": "do-not-print",', encoding="utf-8")
            wormhole_path.write_text(json.dumps(wormhole_config(route("k3"))), encoding="utf-8")

            stdout = StringIO()
            stderr = StringIO()
            with redirect_stdout(stdout), redirect_stderr(stderr):
                exit_code = main(
                    [
                        "--deneb-config",
                        str(deneb_path),
                        "--wormhole-config",
                        str(wormhole_path),
                    ]
                )

        self.assertEqual(exit_code, 2)
        self.assertIn("not valid JSON", stderr.getvalue())
        self.assertNotIn("do-not-print", stdout.getvalue() + stderr.getvalue())

    # --- failover chain invariants (the 2026-08 metered-leak class) ---------- #

    def test_chain_into_a_metered_route_fails(self) -> None:
        """local -> dead local hop -> paid API: the wiring that leaked in August."""
        codes = self.codes(
            deneb_config(model_ids=("k3",)),
            wormhole_config(
                route("k3", fallback="k3-local"),
                route("k3-local", fallback="k3-api"),
                route("k3-api", metered=True),
            ),
        )

        self.assertIn("ROUTE_FALLBACK_METERED", codes)

    def test_metered_route_may_fall_back_to_another_metered_route(self) -> None:
        report = check_topology(
            deneb_config(model_ids=("k3",)),
            wormhole_config(
                route("k3", metered=True, fallback="k3-api"),
                route("k3-api", metered=True),
            ),
        )

        self.assertTrue(report.ok, report.findings)

    def test_fallback_naming_no_route_fails(self) -> None:
        codes = self.codes(
            deneb_config(model_ids=("k3",)),
            wormhole_config(route("k3", fallback="ghost")),
        )

        self.assertIn("ROUTE_FALLBACK_UNKNOWN", codes)

    def test_fallback_cycle_fails(self) -> None:
        codes = self.codes(
            deneb_config(model_ids=("k3",)),
            wormhole_config(
                route("k3", fallback="k3-b"),
                route("k3-b", fallback="k3"),
            ),
        )

        self.assertIn("ROUTE_FALLBACK_CYCLE", codes)

    def test_metered_must_be_a_boolean(self) -> None:
        wormhole = wormhole_config(route("k3"))
        wormhole["models"][0]["metered"] = "yes"

        with self.assertRaises(ConfigShapeError):
            check_topology(deneb_config(model_ids=("k3",)), wormhole)



if __name__ == "__main__":
    unittest.main()
