# ADR 003: Softnet and minimal host integration

Status: Superseded by ADR 013; ADR 015 is the current M1A network decision

Ordinary M1A sessions run with Softnet and disabled clipboard and audio. Filesystem shares, extra disks, Rosetta, VNC, host/bridged networking, Softnet exceptions, port exposure, nested virtualization, guest-agent bridges, and host service access are absent unless separately reviewed. SSH is key-only administration without X11, agent, tunnel, or TCP forwarding. These Tart settings realize backend-independent isolation properties; future backends must prove equivalent properties with their own mechanisms.
