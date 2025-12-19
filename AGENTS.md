# AGENTS.md

## Project Overview

This project implements a CLI tool in Golang, similar to `docker-compose` or
[`podman-compose`](https://github.com/containers/podman-compose), but designed to launch and manage
Virtual Machines with [QEMU](https://www.qemu.org).

## Development Environment

- User OS: Linux Fedora Workstation (advanced user, CLI-first approach)
- Use [mise](https://mise.jdx.dev/) for managing Go toolchain and project dependencies
- Use Mise Tasks (defined in `.mise.toml`) for all development commands, scripts, and helpers
- Place Golang source code in `./src/`
- `./examples/` contains example `qemu-compose` YAML files
- Keep README.md update when you update the application

## Technical Choices

### Linux-Only Support

- **Platform:** qemu-compose only supports Linux
- **Rationale:** The tool relies on Linux-specific features (systemd, user sessions) and is designed
  for Linux server/workstation environments

### systemd Integration

- **Process Management:** VMs are managed as systemd user units using `systemd-run --user`
- **Unit Naming:** Each VM runs in a unit named `qemu-compose-<project>-<vm-name>`
- **Benefits:**
  - Automatic process lifecycle management
  - Built-in logging via journalctl
  - Resource control via cgroups
  - Clean shutdown handling
  - No need for custom daemon or background process management
- **Requirements:** systemd must be installed and running (checked by `doctor` command)

### Network Management

- **Library:** Use the [vishvananda/netlink](https://github.com/vishvananda/netlink) Go library for
  all network operations
- **Rationale:**
  - Avoids spawning external `ip` commands which require elevated privileges
  - Provides native Go API for creating/deleting bridges and TAP devices
  - More reliable error handling and state management
  - Better performance (no process spawning overhead)
- **Capabilities:** The `qemu-compose` binary requires `CAP_NET_ADMIN` capability to manage network
  interfaces
- **Setup:** Users must grant the capability once using:
  `sudo setcap cap_net_admin=ep $(which qemu-compose)`
- **Verification:** The `doctor` command checks if the capability is properly set

### DHCP and IP Management

- **DHCP Server:** Each network runs its own dnsmasq instance for DHCP and DNS services
- **systemd Integration:** dnsmasq instances are managed as systemd user units
- **Unit Naming:** Each dnsmasq unit is named `qemu-compose-dnsmasq-<project>-<network>`
- **Rationale:**
  - Automatic IP address assignment to VMs via DHCP
  - DNS resolution between VMs using hostnames
  - Isolated DHCP/DNS per network (no conflicts between projects)
  - Leverages systemd for lifecycle management (start/stop/restart)
  - Standard Linux tool (dnsmasq) widely available and well-tested
- **Configuration:**
  - dnsmasq binds to the bridge interface for each network
  - DHCP range is automatically calculated from the network subnet
  - DNS forwarding disabled (--no-resolv, --no-poll) for isolation
  - Each VM gets an IP from its network's DHCP pool
- **Lifecycle:**
  - dnsmasq starts when the network is created (`up` command)
  - dnsmasq stops when the network is destroyed (`destroy` command)
  - Metadata tracks dnsmasq state in `.qemu-compose/networks.json`
- **Requirements:** dnsmasq must be installed (checked by `doctor` command)

### Volume Management

qemu-compose supports two types of volumes:

#### Named Volumes

- **Storage:** Named volumes are stored in `.qemu-compose/volumes/<volume-name>/`
- **Format:** Created as qcow2 disk images (QEMU native format)
- **Filesystem:** Pre-formatted with ext4 filesystem for immediate use
- **Attachment:** Attached to VMs as virtio-blk devices (`/dev/vdb`, `/dev/vdc`, etc.)
- **Mounting:** Automatically mounted via cloud-init at specified paths
- **Lifecycle:** Persist across VM lifecycles (survive `destroy` unless explicitly removed)
- **Metadata:** Tracked in `.qemu-compose/volumes.json`
- **Default Size:** 10G if not specified in compose file
- **Read-Only Support:** Can be mounted read-only with `read_only: true`
- **Auto-mount Control:** Always auto-mounted (cannot be disabled for named volumes)
- **Formatting Process:**
  - Uses qemu-nbd to connect qcow2 image to NBD device
  - Formats with mkfs.ext4
  - Requires `sudo` for nbd operations (one-time setup per volume)
- **Rationale:**
  - Better performance than 9p for persistent storage
  - More "docker-like" behavior (block devices)
  - Native QEMU format (qcow2) with thin provisioning
  - Pre-formatted for immediate use (no manual formatting in VM)

#### Bind Mounts

- **Technology:** Use 9p (virtio-9p) filesystem sharing
- **Attachment:** Mounted via QEMU's `-virtfs` option with unique mount tags
- **Security Model:** `passthrough` (requires matching UIDs between host and guest)
- **Path Resolution:**
  - **Relative paths** (e.g., `./config`) are resolved **relative to the compose file location**,
    not the current working directory
  - **Absolute paths** (e.g., `/host/path`) are used as-is
- **Mounting:** Automatically mounted via cloud-init using 9p filesystem type (when
  `automount: true`)
- **Read-Only Support:** Can be mounted read-only with `read_only: true`
- **Auto-mount Control:** Can be disabled with `automount: false` for manual mounting inside the VM
- **Dependencies:** Requires 9p kernel modules and 9base package (installed via cloud-init)
- **Rationale:**
  - Simpler for sharing host directories/files
  - No disk image creation needed
  - Direct access to host filesystem
  - Useful for configuration files and development workflows
- **Limitations:**
  - Lower performance than virtio-blk
  - Requires matching UIDs between host and guest
  - Not suitable for high-I/O workloads

#### Volume Specification Format

Volumes can be specified in two formats:

**Short Form (String):**

```yaml
volumes:
  - volume_name:/path/in/vm
  - ./host/path:/path/in/vm
  - ./host/path:/path/in/vm:ro
```

**Long Form (Map):**

```yaml
volumes:
  - source: volume_name          # Named volume or host path
    target: /path/in/vm          # Mount path in VM (must be absolute)
    read_only: false             # Optional, default: false
    automount: true              # Optional, default: true (bind mounts only)
    mount_options: ""            # Optional, custom 9p mount options (bind mounts only)
```

**Short Form Parsing Rules:**

- Format: `<source>:<target>[:<flags>]`
- If source contains `/` or `\` or starts with `.`, it's a bind mount
- Otherwise, it's a named volume
- Optional flags: `ro` for read-only

**Long Form Features:**

- `source`: Named volume name or host path (relative or absolute)
- `target`: Mount path inside VM (must be absolute, starting with `/`)
- `read_only`: Boolean, default `false`
- `automount`: Boolean, default `true` (only applies to bind mounts)
- `mount_options`: String, custom 9p mount options (only applies to bind mounts)

**Examples:**

```yaml
volumes:
  # Short form - named volume
  - postgres_data:/var/lib/postgresql/data

  # Short form - bind mount (relative path)
  - ./config:/etc/myapp

  # Short form - bind mount with read-only flag
  - ./nginx.conf:/etc/nginx/nginx.conf:ro

  # Long form - named volume with read-only
  - source: app_data
    target: /mnt/shared
    read_only: true

  # Long form - bind mount with auto-mount disabled
  - source: ./scripts
    target: /opt/scripts
    automount: false

  # Long form - bind mount with custom mount options
  - source: ./cache
    target: /var/cache/app
    automount: true
    mount_options: "cache=loose,msize=104857600"
```

#### Volume Detection Logic

- If the source contains `/` or `\` or starts with `.`, it's treated as a bind mount
- Otherwise, it's treated as a named volume reference

#### Auto-mount Behavior

- **Named volumes:** Always auto-mounted via cloud-init (cannot be disabled)
- **Bind mounts:** Auto-mounted by default, can be disabled with `automount: false`
- When `automount: false`, the volume is still attached to QEMU with `-virtfs`, but cloud-init won't
  generate mount commands
- Users can manually mount with: `mount -t 9p -o trans=virtio,version=9p2000.L <mount_tag> <target>`

#### Mixed Volume Support

VMs can use both named volumes and bind mounts simultaneously. The implementation handles:

- Named volumes as virtio-blk devices (sequential device names: `/dev/vdb`, `/dev/vdc`, etc.)
- Bind mounts as 9p filesystems (sequential mount tags: `mount0`, `mount1`, etc.)
- Cloud-init configuration for both types in a single user-data file

### Non-Root Operation

- **Design Principle:** qemu-compose must work as a normal user without requiring `sudo`
- **Requirements:**
  - VMs run under the user's systemd user session (`systemd-run --user`)
  - Network operations use `CAP_NET_ADMIN` capability (granted via `setcap`)
  - All files and directories use user-owned paths (`~/.local/share/`, `./.qemu-compose/`)
  - QEMU runs with user privileges (no need for root)
  - dnsmasq runs under the user's systemd user session
- **Exception:** Named volume creation requires `sudo` for qemu-nbd and mkfs.ext4 operations
  (one-time setup per volume)
- **Benefits:**
  - Better security isolation
  - Follows principle of least privilege
  - Easier to use in development environments
  - No password prompts during normal operation (except volume creation)

### VM Lifecycle Management

- **Base Images:** Downloaded once to `~/.local/share/qemu-compose/images/` (global cache)
- **Instance Disks:** Created as QCOW2 COW (Copy-On-Write) overlays in `.qemu-compose/<vm-name>/`
  (project-local)
- **Disk Strategy:** Base images remain pristine; all changes are written to instance overlay disks
- **Cleanup:** `destroy` command removes instance disks but preserves base images and named volumes

## Code Standards

- All documentation (README, comments, etc.) must be written in English
- All code identifiers (variables, functions, types, etc.) must be in English
- Follow Go best practices and idiomatic patterns
- Use meaningful names that clearly express intent
- Always pin tool versions to specific stable releases (e.g., `go = "1.25.5"` instead of
  `go = "latest"`)

## Directory Structure

### Base Images Cache

- **Location:** `~/.local/share/qemu-compose/images/`
- **Purpose:** Store downloaded base VM images (qcow2, img files)
- **Scope:** Global, shared across all projects
- **Managed by:** `qemu-compose pull` command

### VM Instance Disks

- **Location:** `./.qemu-compose/<vm-name>/`
- **Purpose:** Store VM instance-specific disk images (COW overlays)
- **Scope:** Project-local, one directory per VM
- **Managed by:** `qemu-compose up` command

### Network Metadata

- **Location:** `./.qemu-compose/networks.json`
- **Purpose:** Store network configuration including allocated subnets and dnsmasq state
- **Scope:** Project-local, persists across VM lifecycles
- **Managed by:** Network creation/destruction commands

### Volume Storage

- **Location:** `./.qemu-compose/volumes/<volume-name>/`
- **Purpose:** Store named volume disk images (qcow2 files)
- **Scope:** Project-local, persists across VM lifecycles
- **Managed by:** Volume creation commands (automatic on first use)

### Volume Metadata

- **Location:** `./.qemu-compose/volumes.json`
- **Purpose:** Store metadata about named volumes (size, disk path, creation time)
- **Scope:** Project-local, persists across VM lifecycles
- **Managed by:** Volume creation/destruction commands

## Markdown writing

Bad:

```bash
mise install
```

Good:

```bash
$ mise install
```

## Version Control

- Write clear, descriptive commit messages
- D'ont use conventional commits format (feat:, fix:, docs:, etc.)

## qemu-compose.yaml example

```yaml
version: "1.0"
networks:
  default:
    driver: bridge
    subnet: auto

vms:
  fedora-vm:
    image: https://download.fedoraproject.org/pub/fedora/linux/releases/42/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-42-1.1.x86_64.qcow2
    cpu: 2
    memory: 1024
    disk:
      size: 5G
    networks:
      - default
    volumes:
      - ./volumes/fedora/:/mnt/

  ubuntu-vm:
    image: https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img
    cpu: 2
    memory: 2048
    volumes:
      - source: ./volumes/ubuntu/
        target: /mnt/
        automount: true

```
