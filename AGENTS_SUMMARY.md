# qemu-compose Project Summary

## Overview

_qemu-compose_ is a CLI tool (written in Go) that orchestrates QEMU virtual machines using
declarative YAML configuration, similar to docker-compose but for VMs on Linux.

## Key Technical Choices

### Linux-Only

- Relies on Linux-specific features (systemd, netlink, iptables)
- Designed for Linux server/workstation environments

### systemd Integration

- VMs run as systemd user units: `qemu-compose-<project>-<vm-name>`
- Automatic process lifecycle, logging via journalctl, resource control via cgroups
- dnsmasq instances also managed as systemd user units: `qemu-compose-dnsmasq-<project>-<network>`

### VM Shutdown Behavior

- **Default (Graceful)**: `stop` command uses SSH to execute `sudo systemctl poweroff` inside the VM
- **Forced**: `stop --force` sends SIGTERM to QEMU process for immediate termination
- Graceful shutdown prevents filesystem corruption and data loss
- **Destroy Behavior:** The `destroy` command always uses forced shutdown

### Network Management

- Uses vishvananda/netlink Go library (no external `ip` commands)
- Requires CAP_NET_ADMIN capability: `sudo setcap cap_net_admin=ep $(which qemu-compose)`
- Two networking modes:
  - **User-mode**: Default, no privileges needed, automatic SSH port forwarding
  - **Bridge**: TAP devices + bridges + dnsmasq for DHCP/DNS

### Volume Management

- **Named volumes**: qcow2 disk images, pre-formatted ext4, auto-mounted via cloud-init
- **Bind mounts**: 9p (virtio-9p) filesystem sharing, relative paths resolved from compose file
  location

### Non-Root Operation

- VMs run under user's systemd session
- Exception: Named volume creation requires sudo for qemu-nbd and mkfs.ext4

## Directory Structure

| Location                              | Purpose                                        | Scope         |
| ------------------------------------- | ---------------------------------------------- | ------------- |
| `~/.local/share/qemu-compose/images/` | Base image cache                               | Global        |
| `.qemu-compose/<vm-name>/`            | Instance disks, cloud-init ISO, console socket | Project-local |
| `.qemu-compose/ssh/`                  | Project SSH key pair                           | Project-local |
| `.qemu-compose/networks.json`         | Network metadata (subnets, dnsmasq state)      | Project-local |
| `.qemu-compose/volumes/`              | Named volume disk images                       | Project-local |
| `.qemu-compose/volumes.json`          | Volume metadata                                | Project-local |

## Code Standards

- English-only: documentation, comments, identifiers
- Go best practices and idiomatic patterns
- Pinned tool versions (e.g., `go = "1.23.5"`)
- Meaningful names expressing intent

## VM Lifecycle

1. **Pull**: Download base images to cache
2. **Up**: Create COW overlay disks, generate cloud-init ISO, setup networks/volumes, start via
   systemd-run
3. **SSH/Console**: Connect to running VM
4. **Stop**: Graceful shutdown via SSH (`sudo systemctl poweroff`), or forced with `--force` flag
5. **Destroy**: Stop VM, remove instance disks, keep volumes and base images
