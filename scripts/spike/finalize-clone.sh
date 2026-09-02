#!/usr/bin/env bash

set -euo pipefail

readonly acknowledgement="--acknowledge-clone-finalization"
readonly spike_marker="/etc/boxwarden-task0-spike"
serial_device="none"

usage() {
  printf 'Usage: finalize-clone.sh %s [--serial-device none|ttyS0|hvc0|ttyAMA0]\n' "${acknowledgement}"
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

[[ "${EUID}" -eq 0 ]] || die "must run as root inside the Task 0 guest"
[[ "${1:-}" == "${acknowledgement}" ]] || { usage >&2; exit 2; }
shift

while (( $# > 0 )); do
  case "$1" in
    --serial-device)
      (( $# >= 2 )) || die "--serial-device requires a value"
      serial_device="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

case "${serial_device}" in
  none|ttyS0|hvc0|ttyAMA0) ;;
  *) die "unsupported serial device: ${serial_device}" ;;
esac

[[ -f "${spike_marker}" ]] || die "refusing to finalize a guest without ${spike_marker}"

install -d -m 0755 /usr/local/libexec /var/lib/boxwarden-task0
cat >/usr/local/libexec/boxwarden-task0-firstboot-identity <<EOF
#!/usr/bin/env bash
set -euo pipefail

marker=/var/lib/boxwarden-task0/clone-ready
[[ -f "\${marker}" ]] || exit 0

systemd-machine-id-setup
ssh-keygen -A
machine_id="\$(cat /etc/machine-id)"
hostname="boxwarden-\${machine_id:0:12}"
hostnamectl set-hostname "\${hostname}"

serial_device="${serial_device}"
if [[ "\${serial_device}" != "none" && -c "/dev/\${serial_device}" ]]; then
  {
    printf 'BOXWARDEN_SSH_HOSTKEYS_V1_BEGIN\\n'
    for key in /etc/ssh/ssh_host_*_key.pub; do
      [[ -f "\${key}" ]] || continue
      ssh-keygen -lf "\${key}" -E sha256
    done
    printf 'BOXWARDEN_SSH_HOSTKEYS_V1_END\\n'
  } >"/dev/\${serial_device}"
fi

rm -f "\${marker}"
systemctl disable boxwarden-task0-firstboot-identity.service >/dev/null 2>&1 || true
EOF
chmod 0755 /usr/local/libexec/boxwarden-task0-firstboot-identity

cat >/etc/systemd/system/boxwarden-task0-firstboot-identity.service <<'EOF'
[Unit]
Description=Regenerate Boxwarden Task 0 clone identity
ConditionPathExists=/var/lib/boxwarden-task0/clone-ready
After=local-fs.target systemd-machine-id-commit.service
Before=ssh.service gdm3.service

[Service]
Type=oneshot
ExecStart=/usr/local/libexec/boxwarden-task0-firstboot-identity

[Install]
WantedBy=multi-user.target
EOF

if [[ "${serial_device}" != "none" ]]; then
  install -d -m 0755 "/etc/systemd/system/serial-getty@${serial_device}.service.d"
  cat >"/etc/systemd/system/serial-getty@${serial_device}.service.d/10-boxwarden-autologin.conf" <<'EOF'
[Unit]
After=boxwarden-task0-firstboot-identity.service

[Service]
ExecStart=
ExecStart=-/sbin/agetty --autologin boxwarden --noclear --keep-baud 115200,57600,38400,9600 - $TERM
EOF
  systemctl enable "serial-getty@${serial_device}.service"
fi
systemctl enable boxwarden-task0-firstboot-identity.service

rm -f /etc/ssh/ssh_host_*_key /etc/ssh/ssh_host_*_key.pub
rm -f /var/lib/systemd/random-seed
rm -f /var/lib/NetworkManager/*lease* /var/lib/dhcp/* 2>/dev/null || true
rm -rf /var/lib/cloud/instances /var/lib/cloud/instance
if command -v cloud-init >/dev/null 2>&1; then
  cloud-init clean --logs --seed
fi

rm -f /root/.bash_history /home/*/.bash_history
rm -rf /root/.cache /home/*/.cache
rm -rf /root/.config/chromium /home/*/.config/chromium
rm -rf /root/.config/google-chrome /home/*/.config/google-chrome
rm -rf /root/.mozilla /home/*/.mozilla
rm -rf /var/log/installer

journalctl --rotate || true
journalctl --vacuum-time=1s || true
find /var/log -type f -exec truncate -s 0 {} +

rm -f /var/lib/dbus/machine-id
: >/etc/machine-id
ln -s /etc/machine-id /var/lib/dbus/machine-id
touch /var/lib/boxwarden-task0/clone-ready
sync

printf 'clone-ready identity removed; power off without rebooting before cloning\n'
