# ADR 002: Ubuntu 24.04 ARM64 with guest-local GUI

Status: Accepted; extended by ADR 018

Use Ubuntu 24.04 ARM64 under Tart. Host interaction is Tart display/input. The desktop uses Ubuntu supported configuration and documented XWayland compatibility rather than host X11 forwarding, Xorg forcing, or experimental native Wayland work.
