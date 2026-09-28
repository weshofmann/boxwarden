# N1 acceptance matrix

All candidate rows initially pending. Stock evidence is context, not candidate
qualification. Record exact source/build and test command for each promotion.

| Case | Deterministic boundary tests | Host-only / isolated guest gate |
|---|---|---|
| DHCP discover, ACK, renewal, lease expiry | MAC/source/ports, trusted ACK, unicast renewal | fresh address + renewal after lease interval |
| DNS UDP and TCP53, fallback | gateway advertised, UDP/TCP framing, fragments denied | UDP, TCP, truncated/large answer fallback |
| Resolver changes, scoped DNS, VPN, DNS64 | trusted metadata and DNS payload/path preservation | separate observed environment rows; no VPN changes without approval |
| Public HTTPS | public destination passes stock rule after host ceiling | owned/appropriate public endpoint through selected host route |
| Management + clipboard | host SYN/handshake, exact tuple response, expiry/capacity/address removal and renewal | pinned SSH and controlled synthetic clipboard in owned guest |
| Host service denial | gateway and host-public aliases denied; forged ACK/source22 denied | same owned listener reachable stock control, denied candidate |
| Private/link-local and other sessions | private/link-local, multicast and broadcast packet fixtures; no override | two owned guests, positive control where applicable |
| Root resistance | arbitrary bytes, spoof MAC/IP, unknown protocol/VLAN/IPv6 | guest-root route/firewall changes cannot lift restriction |
| Parser bypasses | short lengths, checksums, options, all fragment forms, bounded fuzz | no hostile real-host probing |
| Initialization/metadata failure | alias rejection, missing/failed address refresh, no forwarding | failed startup remains non-ready; no stock fallback |
| Start/stop/restart/rebuild | exact child argv and admission mismatch tests | repeat lifecycle with policy and listener controls |

Evidence labels: SOURCE INSPECTION; DETERMINISTIC; HOST-ONLY INTEGRATION;
REAL ISOLATED GUEST; AWAITING ATTENDED DEPLOYMENT. Native IPv6 and effectively
IPv6-only upstream remain unsupported/unqualified; no insecure fallback.
