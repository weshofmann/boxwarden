#!/usr/bin/env python3
"""Adjudicate one N1 cross-guest TCP receipt; never initiate a network probe.

The receipt must come from a separately reviewed, generation-bound live
procedure. This tool checks consistency and verdict rules, not provenance.
"""

import ipaddress
import json
import sys
import uuid


SCOPE = "one cross-guest TCP endpoint; no UDP, VPN or IPv6 claim"
EXIT = {"PASS": 0, "FAIL": 1, "INVALID": 2, "UNQUALIFIED": 3}
BINDING_KEYS = {"domain", "session_id", "backend_object", "generation", "address"}


class InvalidReceipt(ValueError):
    pass


def exact_keys(value, keys):
    if not isinstance(value, dict) or set(value) != keys:
        raise InvalidReceipt("receipt fields are missing or unexpected")


def canonical_uuid(value):
    if not isinstance(value, str) or str(uuid.UUID(value)) != value:
        raise InvalidReceipt("noncanonical session or generation UUID")


def binding(value):
    exact_keys(value, BINDING_KEYS)
    if value["domain"] != "n1qualification":
        raise InvalidReceipt("unexpected security domain")
    canonical_uuid(value["session_id"])
    canonical_uuid(value["generation"])
    if not isinstance(value["backend_object"], str) or not value["backend_object"]:
        raise InvalidReceipt("missing backend object")
    try:
        address = ipaddress.IPv4Address(value["address"])
    except (ValueError, TypeError) as error:
        raise InvalidReceipt("invalid guest IPv4 address") from error
    if (
        not address.is_private
        or address.is_loopback
        or address.is_link_local
        or address.is_multicast
        or address.is_unspecified
    ):
        raise InvalidReceipt("non-guest TCP endpoint")
    if str(address) != value["address"]:
        raise InvalidReceipt("noncanonical guest IPv4 address")
    return value


def bound_observation(value, owner, extra_keys):
    exact_keys(value, BINDING_KEYS | extra_keys)
    if {key: value[key] for key in BINDING_KEYS} != owner:
        raise InvalidReceipt("observation is bound to another guest or generation")


def verdict(receipt):
    exact_keys(
        receipt,
        {"version", "control", "candidate", "endpoint", "host_before", "candidate_probe", "host_after", "candidate_health"},
    )
    if type(receipt["version"]) is not int or receipt["version"] != 1:
        raise InvalidReceipt("unsupported receipt version")
    control = binding(receipt["control"])
    candidate = binding(receipt["candidate"])
    if (
        control["session_id"] == candidate["session_id"]
        or control["backend_object"] == candidate["backend_object"]
        or control["address"] == candidate["address"]
    ):
        raise InvalidReceipt("control and candidate are not distinct")
    endpoint = receipt["endpoint"]
    exact_keys(endpoint, {"address", "port"})
    if endpoint != {"address": control["address"], "port": 22}:
        raise InvalidReceipt("endpoint is not the control guest's SSH listener")

    for phase in ("host_before", "host_after"):
        observed = receipt[phase]
        bound_observation(observed, control, {"port", "ssh_exit", "identity_ok"})
        if (
            observed["port"] != 22
            or type(observed["ssh_exit"]) is not int
            or type(observed["identity_ok"]) is not bool
        ):
            raise InvalidReceipt("malformed host positive")
        if observed["ssh_exit"] != 0 or not observed["identity_ok"]:
            return "INVALID", "host strict-management positive missing"

    probe = receipt["candidate_probe"]
    extras = {"ssh_exit", "target_address", "target_port", "connect"}
    if isinstance(probe, dict) and "error" in probe:
        extras.add("error")
    bound_observation(probe, candidate, extras)
    if probe["target_address"] != endpoint["address"] or probe["target_port"] != 22:
        raise InvalidReceipt("candidate targeted another TCP endpoint")
    if type(probe["ssh_exit"]) is not int:
        raise InvalidReceipt("malformed candidate management SSH exit")
    if probe["connect"] == "connected":
        # A later transport failure cannot erase a recorded completed connect.
        return "FAIL", "candidate completed TCP connect"
    if probe["ssh_exit"] != 0:
        return "UNQUALIFIED", "candidate management SSH failed"
    if probe["connect"] not in ("timeout", "error"):
        raise InvalidReceipt("unknown candidate TCP outcome")
    if probe["connect"] == "error":
        if not isinstance(probe.get("error"), str) or not probe["error"]:
            raise InvalidReceipt("ambiguous error lacks detail")
        return "UNQUALIFIED", "candidate TCP error was not a timeout"
    if "error" in probe:
        raise InvalidReceipt("timeout receipt has contradictory error")

    health = receipt["candidate_health"]
    health_keys = {"ready", "management", "gateway_dns", "public_https"}
    bound_observation(health, candidate, health_keys)
    if any(type(health[key]) is not bool for key in health_keys):
        raise InvalidReceipt("candidate health evidence malformed")
    if not all(health[key] for key in health_keys):
        return "UNQUALIFIED", "candidate management or public compatibility missing"
    return "PASS", "timeout with both host positives and healthy candidate"


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise InvalidReceipt("duplicate receipt field")
        result[key] = value
    return result


def main():
    raw = sys.stdin.buffer.read(65537)
    if len(raw) > 65536:
        outcome = "INVALID", "receipt exceeds 64 KiB"
    else:
        try:
            outcome = verdict(json.loads(raw, object_pairs_hook=unique_object))
        except (InvalidReceipt, ValueError, TypeError, KeyError) as error:
            outcome = "INVALID", str(error)
    label, reason = outcome
    print(json.dumps({"verdict": label, "reason": reason, "scope": SCOPE}, sort_keys=True))
    return EXIT[label]


if __name__ == "__main__":
    sys.exit(main())
