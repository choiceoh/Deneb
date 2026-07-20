#!/usr/bin/env python3
"""Validate the Deneb picker -> provider -> Wormhole routing topology.

The two configuration files are individually valid JSON but form one runtime
graph.  A model selected by a Deneb role is a provider/model node; a provider
may point at the Wormhole listener; Wormhole then resolves the model component
against a client-facing route name before rewriting it to ``upstreamModel``.
This checker validates those cross-file edges without loading or printing keys.
"""

from __future__ import annotations

import argparse
import ipaddress
import json
import os
import sys
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import SplitResult, urlsplit

DEFAULT_WORMHOLE_LISTEN = ":18800"

ROLE_FIELDS = (
    ("main2", "main2Model"),
    ("lightweight", "lightweightModel"),
    ("tiny", "tinyModel"),
    ("coding", "codingModel"),
    ("fallback", "fallbackModel"),
    ("vision", "visionModel"),
)

# Mirrors modelpicker.builtinProviders. A configured provider with no explicit
# models inherits these entries in the native model picker.
BUILTIN_PROVIDER_MODELS: dict[str, tuple[str, ...]] = {
    "zai": ("glm-5.2",),
    "openrouter": (
        "anthropic/claude-opus-4.7",
        "anthropic/claude-sonnet-4.6",
        "google/gemini-3.1-pro",
    ),
    "kimi": ("kimi-for-coding",),
    "mimo-plan": ("mimo-v2.5-pro",),
}

# Mirrors modelrole.resolveAPIMode. Any explicit API value is normalized by the
# LLM client; only anthropic and anthropic-messages select Anthropic mode.
BUILTIN_ANTHROPIC_PROVIDERS = {
    "anthropic",
    "zai",
    "zai-subagent",
    "mimo",
    "mimo-plan",
    "kimi",
}


class ConfigShapeError(ValueError):
    """Raised when a relevant config value has an unusable JSON shape."""


@dataclass(frozen=True)
class ListenAddress:
    host: str
    port: int


@dataclass(frozen=True)
class RoleBinding:
    role: str
    model_id: str


@dataclass(frozen=True)
class ProviderNode:
    provider_id: str
    base_url: str
    protocol: str
    models: tuple[str, ...]
    endpoint: SplitResult | None
    targets_wormhole: bool
    reaches_listener: bool


@dataclass(frozen=True)
class RouteNode:
    name: str
    protocol: str
    raw_protocol: str
    upstream_model: str
    has_static_url: bool
    fleet: bool
    index: int


@dataclass(frozen=True)
class ModelRouteTopology:
    roles: tuple[RoleBinding, ...]
    providers: tuple[ProviderNode, ...]
    hidden_models: frozenset[str]
    routes: tuple[RouteNode, ...]
    wormhole_listen: ListenAddress
    wormhole_config_present: bool
    sparkfleet_configured: bool


@dataclass(frozen=True, order=True)
class Finding:
    code: str
    subject: str
    detail: str


@dataclass(frozen=True)
class CheckReport:
    topology: ModelRouteTopology
    findings: tuple[Finding, ...]

    @property
    def ok(self) -> bool:
        return not self.findings


def _object(value: object, label: str) -> dict[str, object]:
    if not isinstance(value, dict):
        raise ConfigShapeError(f"{label} must be an object")
    return value


def _optional_object(parent: dict[str, object], key: str, label: str) -> dict[str, object]:
    value = parent.get(key)
    if value is None:
        return {}
    return _object(value, label)


def _optional_string(parent: dict[str, object], key: str, label: str) -> str:
    value = parent.get(key)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ConfigShapeError(f"{label} must be a string")
    return value.strip()


def _optional_raw_string(parent: dict[str, object], key: str, label: str) -> str:
    value = parent.get(key)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ConfigShapeError(f"{label} must be a string")
    return value


def _model_primary(value: object, label: str) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value.strip()
    if isinstance(value, dict):
        primary = value.get("primary")
        if primary is None:
            return ""
        if not isinstance(primary, str):
            raise ConfigShapeError(f"{label}.primary must be a string")
        return primary.strip()
    raise ConfigShapeError(f"{label} must be a string or an object with primary")


def _dedupe(values: list[str]) -> tuple[str, ...]:
    return tuple(dict.fromkeys(value for value in values if value))


def _provider_protocol(provider_id: str, explicit_api: str) -> str:
    if explicit_api:
        if explicit_api.lower() in {"anthropic", "anthropic-messages"}:
            return "anthropic"
        return "openai"
    if provider_id in BUILTIN_ANTHROPIC_PROVIDERS:
        return "anthropic"
    return "openai"


def _parse_endpoint(raw_url: str) -> SplitResult | None:
    if not raw_url:
        return None
    try:
        endpoint = urlsplit(os.path.expandvars(raw_url))
        if endpoint.scheme not in {"http", "https"} or not endpoint.hostname:
            return None
        # Accessing .port validates malformed ports while retaining urlsplit's
        # IPv6 handling. The value itself is read again by _endpoint_port.
        _ = endpoint.port
        return endpoint
    except ValueError:
        return None


def _parse_listen(raw_listen: str) -> ListenAddress:
    listen = raw_listen or DEFAULT_WORMHOLE_LISTEN
    try:
        parsed = urlsplit(f"//{listen}")
        if parsed.port is None:
            raise ValueError
        return ListenAddress(host=(parsed.hostname or "").lower(), port=parsed.port)
    except ValueError as exc:
        raise ConfigShapeError(f"wormhole.listen is not a host:port value: {listen!r}") from exc


def _endpoint_port(endpoint: SplitResult) -> int:
    if endpoint.port is not None:
        return endpoint.port
    return 443 if endpoint.scheme == "https" else 80


def _is_local_host(host: str) -> bool:
    normalized = host.strip().lower()
    if normalized in {"", "localhost", "0.0.0.0", "::"}:
        return True
    try:
        address = ipaddress.ip_address(normalized)
    except ValueError:
        return False
    return address.is_loopback or address.is_unspecified


def _endpoint_reaches_listener(endpoint: SplitResult, listen: ListenAddress) -> bool:
    if _endpoint_port(endpoint) != listen.port:
        return False
    endpoint_host = (endpoint.hostname or "").lower()
    if endpoint_host == listen.host:
        return True
    if _is_local_host(endpoint_host) and _is_local_host(listen.host):
        return True
    return False


def _provider_targets_wormhole(
    provider_id: str, endpoint: SplitResult | None, listen: ListenAddress
) -> tuple[bool, bool]:
    explicit_wormhole_provider = provider_id == "wormhole" or provider_id.startswith(
        "wormhole-"
    )
    reaches_listener = endpoint is not None and _endpoint_reaches_listener(endpoint, listen)
    return explicit_wormhole_provider or reaches_listener, reaches_listener


def _extract_roles(deneb: dict[str, object]) -> tuple[RoleBinding, ...]:
    agents = _optional_object(deneb, "agents", "deneb.agents")
    defaults = _optional_object(agents, "defaults", "deneb.agents.defaults")

    default_model = _optional_string(
        agents, "defaultModel", "deneb.agents.defaultModel"
    ) or _model_primary(defaults.get("model"), "deneb.agents.defaults.model")

    roles: list[RoleBinding] = []
    if default_model:
        roles.append(RoleBinding("main", default_model))
    for role, field in ROLE_FIELDS:
        model_id = _optional_string(agents, field, f"deneb.agents.{field}")
        if model_id:
            roles.append(RoleBinding(role, model_id))

    subagents = _optional_object(defaults, "subagents", "deneb.agents.defaults.subagents")
    subagent_model = _model_primary(
        subagents.get("model"), "deneb.agents.defaults.subagents.model"
    )
    if subagent_model:
        roles.append(RoleBinding("subagent", subagent_model))
    return tuple(roles)


def _extract_hidden_models(deneb: dict[str, object]) -> frozenset[str]:
    models = _optional_object(deneb, "models", "deneb.models")
    raw_hidden = models.get("hiddenModels")
    if raw_hidden is None:
        return frozenset()
    if not isinstance(raw_hidden, list):
        raise ConfigShapeError("deneb.models.hiddenModels must be an array")
    hidden: set[str] = set()
    for index, value in enumerate(raw_hidden):
        if not isinstance(value, str):
            raise ConfigShapeError(f"deneb.models.hiddenModels[{index}] must be a string")
        if model_id := value.strip():
            hidden.add(model_id)
    return frozenset(hidden)


def _extract_providers(
    deneb: dict[str, object], listen: ListenAddress
) -> tuple[ProviderNode, ...]:
    models = _optional_object(deneb, "models", "deneb.models")
    raw_providers = _optional_object(models, "providers", "deneb.models.providers")
    providers: list[ProviderNode] = []
    for provider_id in sorted(raw_providers):
        raw_provider = _object(
            raw_providers[provider_id], f"deneb.models.providers.{provider_id}"
        )
        base_url = _optional_string(
            raw_provider, "baseUrl", f"deneb.models.providers.{provider_id}.baseUrl"
        )
        api = _optional_string(
            raw_provider, "api", f"deneb.models.providers.{provider_id}.api"
        )
        raw_models = raw_provider.get("models")
        model_ids: list[str] = []
        if raw_models is not None:
            if not isinstance(raw_models, list):
                raise ConfigShapeError(
                    f"deneb.models.providers.{provider_id}.models must be an array"
                )
            for index, raw_model in enumerate(raw_models):
                model = _object(
                    raw_model,
                    f"deneb.models.providers.{provider_id}.models[{index}]",
                )
                model_id = _optional_string(
                    model,
                    "id",
                    f"deneb.models.providers.{provider_id}.models[{index}].id",
                )
                if model_id:
                    model_ids.append(model_id)
        effective_models = _dedupe(model_ids)
        if not effective_models:
            effective_models = BUILTIN_PROVIDER_MODELS.get(provider_id, ())

        endpoint = _parse_endpoint(base_url)
        targets_wormhole, reaches_listener = _provider_targets_wormhole(
            provider_id, endpoint, listen
        )
        providers.append(
            ProviderNode(
                provider_id=provider_id,
                base_url=base_url,
                protocol=_provider_protocol(provider_id, api),
                models=effective_models,
                endpoint=endpoint,
                targets_wormhole=targets_wormhole,
                reaches_listener=reaches_listener,
            )
        )
    return tuple(providers)


def _extract_routes(wormhole: dict[str, object] | None) -> tuple[RouteNode, ...]:
    if wormhole is None:
        return ()
    raw_models = wormhole.get("models")
    if raw_models is None:
        return ()
    if not isinstance(raw_models, list):
        raise ConfigShapeError("wormhole.models must be an array")
    routes: list[RouteNode] = []
    for index, raw_route in enumerate(raw_models):
        route = _object(raw_route, f"wormhole.models[{index}]")
        name = _optional_raw_string(route, "name", f"wormhole.models[{index}].name")
        raw_protocol = _optional_raw_string(
            route, "protocol", f"wormhole.models[{index}].protocol"
        )
        protocol = "anthropic" if raw_protocol == "anthropic" else "openai"
        upstream_model = _optional_raw_string(
            route, "upstreamModel", f"wormhole.models[{index}].upstreamModel"
        )
        static_url = _optional_raw_string(route, "url", f"wormhole.models[{index}].url")
        raw_fleet = route.get("fleet")
        if raw_fleet is not None and not isinstance(raw_fleet, bool):
            raise ConfigShapeError(f"wormhole.models[{index}].fleet must be a boolean")
        routes.append(
            RouteNode(
                name=name,
                protocol=protocol,
                raw_protocol=raw_protocol,
                upstream_model=upstream_model or name,
                has_static_url=bool(static_url.strip()),
                fleet=bool(raw_fleet),
                index=index,
            )
        )
    return tuple(routes)


def build_topology(
    deneb: dict[str, object], wormhole: dict[str, object] | None
) -> ModelRouteTopology:
    """Build the relevant cross-config graph without inspecting credentials."""

    listen_raw = ""
    if wormhole is not None:
        listen_raw = _optional_raw_string(wormhole, "listen", "wormhole.listen")
        raw_sparkfleet = wormhole.get("sparkfleet")
        if raw_sparkfleet is not None:
            _object(raw_sparkfleet, "wormhole.sparkfleet")
    else:
        raw_sparkfleet = None
    listen = _parse_listen(listen_raw)
    return ModelRouteTopology(
        roles=_extract_roles(deneb),
        providers=_extract_providers(deneb, listen),
        hidden_models=_extract_hidden_models(deneb),
        routes=_extract_routes(wormhole),
        wormhole_listen=listen,
        wormhole_config_present=wormhole is not None,
        sparkfleet_configured=raw_sparkfleet is not None,
    )


def _split_model_id(model_id: str) -> tuple[str, str]:
    provider_id, separator, model = model_id.partition("/")
    if not separator:
        return "", model_id
    return provider_id, model


def validate_topology(topology: ModelRouteTopology) -> tuple[Finding, ...]:
    """Return deterministic violations of the picker and routing invariants."""

    findings: list[Finding] = []
    providers = {provider.provider_id: provider for provider in topology.providers}

    for binding in topology.roles:
        if binding.model_id in topology.hidden_models:
            findings.append(
                Finding(
                    "ROLE_MODEL_HIDDEN",
                    f"role={binding.role}",
                    f"{binding.model_id} is selected by the role but removed from the picker",
                )
            )

    route_groups: dict[str, list[RouteNode]] = {}
    for route in topology.routes:
        if not route.name:
            findings.append(
                Finding(
                    "ROUTE_NAME_EMPTY",
                    f"wormhole.models[{route.index}]",
                    "a client-facing route name is required",
                )
            )
            continue
        route_groups.setdefault(route.name, []).append(route)
        if route.raw_protocol not in {"", "openai", "anthropic"}:
            findings.append(
                Finding(
                    "ROUTE_PROTOCOL_INVALID",
                    f"route={route.name}",
                    f"protocol {route.raw_protocol!r} is treated as openai by Wormhole",
                )
            )
    for name, group in route_groups.items():
        if len(group) > 1:
            indexes = ",".join(str(route.index) for route in group)
            findings.append(
                Finding(
                    "ROUTE_NAME_DUPLICATE",
                    f"route={name}",
                    f"client lookup is ambiguous across wormhole.models indexes {indexes}",
                )
            )

    effective_routes = {name: group[-1] for name, group in route_groups.items()}
    wormhole_providers = [provider for provider in topology.providers if provider.targets_wormhole]

    for provider in wormhole_providers:
        if provider.endpoint is None:
            findings.append(
                Finding(
                    "PROVIDER_ENDPOINT_INVALID",
                    f"provider={provider.provider_id}",
                    "the Wormhole provider needs an absolute http(s) baseUrl",
                )
            )
            continue
        if not provider.reaches_listener:
            findings.append(
                Finding(
                    "PROVIDER_ENDPOINT_MISMATCH",
                    f"provider={provider.provider_id}",
                    "baseUrl does not reach the listener declared by wormhole.listen",
                )
            )
            continue
        path = provider.endpoint.path
        expected_path = "" if provider.protocol == "anthropic" else "/v1"
        if provider.protocol == "anthropic":
            path_matches = path.rstrip("/") == ""
        else:
            path_matches = path == "/v1"
        if not path_matches:
            findings.append(
                Finding(
                    "PROVIDER_PATH_MISMATCH",
                    f"provider={provider.provider_id}",
                    f"{provider.protocol} clients require Wormhole base path "
                    f"{expected_path or '/ (no /v1)'}",
                )
            )

    active_wormhole_providers = [
        provider for provider in wormhole_providers if provider.reaches_listener
    ]
    if active_wormhole_providers and not topology.wormhole_config_present:
        provider_ids = ",".join(provider.provider_id for provider in active_wormhole_providers)
        findings.append(
            Finding(
                "WORMHOLE_CONFIG_MISSING",
                f"providers={provider_ids}",
                "the Deneb provider targets Wormhole but its route config file is missing",
            )
        )
        return tuple(sorted(findings))

    checked_routes: set[tuple[str, str]] = set()

    def check_route(provider: ProviderNode, model: str) -> None:
        key = (provider.provider_id, model)
        if key in checked_routes:
            return
        checked_routes.add(key)
        route = effective_routes.get(model)
        if route is None:
            aliases = sorted(
                candidate.name
                for candidate in topology.routes
                if candidate.name and candidate.upstream_model == model
            )
            hint = ""
            if aliases:
                hint = (
                    f"; upstreamModel matches route {aliases[0]!r}, but clients look up name"
                )
            findings.append(
                Finding(
                    "CLIENT_ROUTE_MISSING",
                    f"model={provider.provider_id}/{model}",
                    f"Wormhole has no client-facing route named {model!r}{hint}",
                )
            )
            return
        if route.protocol != provider.protocol:
            findings.append(
                Finding(
                    "ROUTE_PROTOCOL_MISMATCH",
                    f"model={provider.provider_id}/{model}",
                    f"Deneb speaks {provider.protocol}, but route {model!r} speaks "
                    f"{route.protocol}",
                )
            )
        if not route.has_static_url and not (route.fleet and topology.sparkfleet_configured):
            findings.append(
                Finding(
                    "ROUTE_BACKEND_MISSING",
                    f"model={provider.provider_id}/{model}",
                    f"route {model!r} has neither a static url nor a configured fleet source",
                )
            )

    for provider in active_wormhole_providers:
        visible_models = list(provider.models)
        if provider.protocol == "openai":
            visible_models.extend(
                route.name
                for route in topology.routes
                if route.name and route.protocol == "openai"
            )
        for model in _dedupe(visible_models):
            full_model_id = f"{provider.provider_id}/{model}"
            if full_model_id not in topology.hidden_models:
                check_route(provider, model)

    for binding in topology.roles:
        provider_id, model = _split_model_id(binding.model_id)
        provider = providers.get(provider_id)
        if provider is None or not provider.reaches_listener:
            continue
        if provider.protocol == "anthropic" and model not in provider.models:
            findings.append(
                Finding(
                    "ANTHROPIC_ROLE_NOT_VISIBLE",
                    f"role={binding.role}",
                    f"{binding.model_id} must be declared in provider.models because "
                    "Anthropic routes have no /v1/models discovery",
                )
            )
        check_route(provider, model)

    return tuple(sorted(findings))


def check_topology(
    deneb: dict[str, object], wormhole: dict[str, object] | None
) -> CheckReport:
    topology = build_topology(deneb, wormhole)
    return CheckReport(topology=topology, findings=validate_topology(topology))


def _load_json_object(path: Path, label: str) -> dict[str, object]:
    try:
        with path.open(encoding="utf-8") as handle:
            value = json.load(handle)
    except json.JSONDecodeError as exc:
        raise ConfigShapeError(
            f"{label} is not valid JSON at line {exc.lineno}, column {exc.colno}"
        ) from exc
    return _object(value, label)


def _print_report(report: CheckReport) -> None:
    for finding in report.findings:
        print(f"ERROR {finding.code} {finding.subject}: {finding.detail}")
    topology = report.topology
    status = "PASS" if report.ok else "FAIL"
    wormhole_provider_count = sum(
        provider.reaches_listener for provider in topology.providers
    )
    print(
        f"{status} model route topology: roles={len(topology.roles)} "
        f"wormholeProviders={wormhole_provider_count} routes={len(topology.routes)} "
        f"violations={len(report.findings)}"
    )


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--deneb-config",
        type=Path,
        default=Path("~/.deneb/deneb.json"),
        help="Deneb config path (default: ~/.deneb/deneb.json)",
    )
    parser.add_argument(
        "--wormhole-config",
        type=Path,
        default=Path("~/.wormhole/config.json"),
        help="Wormhole config path (default: ~/.wormhole/config.json)",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    deneb_path = args.deneb_config.expanduser()
    wormhole_path = args.wormhole_config.expanduser()
    try:
        deneb = _load_json_object(deneb_path, "deneb config")
        wormhole = (
            _load_json_object(wormhole_path, "wormhole config")
            if wormhole_path.exists()
            else None
        )
        report = check_topology(deneb, wormhole)
    except (ConfigShapeError, OSError) as exc:
        print(f"ERROR MODEL_ROUTE_TOPOLOGY_CONFIG: {exc}", file=sys.stderr)
        return 2
    _print_report(report)
    return 0 if report.ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
