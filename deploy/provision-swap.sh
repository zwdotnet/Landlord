#!/bin/sh
# Explicitly authorized host change; preserve the previous host configuration.
set -eu
test "$(id -u)" = 0
test "$(hostname)" = hcss-ecs-b4e4
landlord_swap=/swapfile
landlord_sysctl=/etc/sysctl.d/99-landlord-swap.conf
if swapon --show=NAME --noheadings | grep -Fxq "$landlord_swap"; then
  swapon --show
  exit 0
fi
test ! -e "$landlord_swap"
test ! -e "$landlord_sysctl"
landlord_free_kb=$(df --output=avail / | tail -1 | tr -d ' ')
test "$landlord_free_kb" -ge 8388608
landlord_swap_backup="/var/backups/landlord/swap-$(date +%Y%m%d-%H%M%S)"
install -d -m 700 "$landlord_swap_backup"
cp -a /etc/fstab "$landlord_swap_backup/fstab.before"
cp -a /etc/sysctl.conf "$landlord_swap_backup/sysctl.conf.before"
sysctl -n vm.swappiness > "$landlord_swap_backup/swappiness.before"
umask 077
fallocate -l 4G "$landlord_swap"
chmod 600 "$landlord_swap"
mkswap "$landlord_swap"
swapon "$landlord_swap"
if ! awk '$1=="/swapfile" {found=1} END {exit !found}' /etc/fstab; then
  printf '\n/swapfile none swap sw,nofail 0 0\n' >> /etc/fstab
fi
printf '# Local disk swap for memory pressure; authorized 2026-11-11.\nvm.swappiness = 10\n' > "$landlord_sysctl"
chmod 644 "$landlord_sysctl"
python3 - <<'PY'
from pathlib import Path
import re
p = Path('/etc/sysctl.conf')
s = p.read_text()
s = re.sub(r'(?m)^\s*vm\.swappiness\s*=\s*\d+\s*$', 'vm.swappiness=10', s)
p.write_text(s)
PY
sysctl -p "$landlord_sysctl"
findmnt --verify --tab-file /etc/fstab
systemctl daemon-reload
swapon --show --bytes
free -m
printf 'Configuration backup: %s\n' "$landlord_swap_backup"
