# Volume Management Architecture

## Overview

*qemu-compose* supports two volume types: named volumes (persistent block storage) and bind mounts (9p filesystem sharing).

## Named Volumes

### Storage

- Location: `.qemu-compose/volumes/<volume-name>/volume.qcow2`
- Format: qcow2 (QEMU native, thin provisioning)
- Filesystem: ext4 (pre-formatted)
- Default size: 10G (if not specified)

### Lifecycle

1. **Creation**: `ensureVolumeExists()` called during `up`
   - Create qcow2 with `qemu-img create`
   - Format with ext4 using qemu-nbd + mkfs.ext4 (requires sudo)
   - Save metadata to `.qemu-compose/volumes.json`
2. **Attachment**: Passed to QEMU as virtio-blk device
   - Device names: `/dev/vdb`, `/dev/vdc`, etc. (after `/dev/vda` main disk)
   - Cloud-init mounts automatically
3. **Persistence**: Survive `destroy` command (only removed explicitly)

### Auto-Mount Behavior

- Always auto-mounted via cloud-init (cannot be disabled)
- Cloud-init generates mount entries in `/etc/fstab`
- Mount path specified in compose file (must be absolute)

### Read-Only Support

- Specified with `read_only: true` in volume spec
- Cloud-init mounts with `ro` flag

### Volume Metadata (volumes.json)

```json
{
  "postgres_data": {
    "name": "postgres_data",
    "size": "20G",
    "disk_path": "/path/to/.qemu-compose/volumes/postgres_data/volume.qcow2",
    "created": "timestamp"
  }
}
```

## Bind Mounts

### Technology

- Uses 9p (virtio-9p) filesystem sharing
- QEMU option: `-virtfs local,path=<host-path>,mount_tag=<tag>,security_model=passthrough`
- Security model: `passthrough` (requires matching UIDs between host and guest)

### Path Resolution

- **Relative paths** (e.g., `./config`): Resolved relative to compose file directory
- **Absolute paths** (e.g., `/host/path`): Used as-is
- Validation: `resolveBindMountPath()` checks path exists

### Mount Tags

- Sequential naming: `mount0`, `mount1`, etc.
- Used by cloud-init to mount with 9p filesystem type
- Format: `mount -t 9p -o trans=virtio,version=9p2000.L <mount_tag> <target>`

### Auto-Mount Behavior

- Default: `automount: true` (cloud-init generates mount commands)
- Can disable: `automount: false` (volume attached but not auto-mounted)
- When disabled, user must manually mount inside VM
- Custom mount options supported: `mount_options: "cache=loose,msize=104857600"`

### Read-Only Support

- Specified with `read_only: true`
- Cloud-init mounts with `ro` flag
- 9p mount option: `ro` added to mount options

## Volume Specification Formats

### Short Form (String)

```yaml
volumes:
  - volume_name:/path/in/vm              # Named volume
  - ./host/path:/path/in/vm              # Bind mount (relative)
  - /abs/host/path:/path/in/vm           # Bind mount (absolute)
  - ./config:/etc/app:ro                 # Bind mount with read-only
```

### Long Form (Map)

```yaml
volumes:
  - source: volume_name                  # Named volume or host path
    target: /path/in/vm                  # Mount path (must be absolute)
    read_only: false                     # Optional, default: false
    automount: true                      # Optional, default: true (bind mounts only)
    mount_options: ""                    # Optional, custom 9p options (bind mounts only)
```

## Detection Logic

- If source contains `/`, `\`, or starts with `.` → bind mount
- Otherwise → named volume reference

## Cloud-Init Integration

### Named Volumes

Cloud-init generates `/etc/fstab` entries:
```
/dev/vdb /mnt/data ext4 defaults 0 2
/dev/vdc /var/lib/postgresql/data ext4 ro 0 2
```

### Bind Mounts

Cloud-init generates mount entries:

```
mount0 /mnt/shared 9p trans=virtio,version=9p2000.L 0 0
mount1 /opt/scripts 9p trans=virtio,version=9p2000.L,ro 0 0
```

Also installs 9base package and loads 9p kernel modules:

```
bootcmd:
  - modprobe 9p
  - modprobe 9pnet_virtio
```

## Implementation Details

### Volume Mount Parsing

- `parseVMVolumes()` processes all volume specs for a VM
- Validates target paths (must be absolute)
- Resolves bind mount paths relative to compose file
- Ensures named volumes exist before VM starts
- Returns `[]VMVolumeMount` with all metadata

### QEMU Command Building

- Named volumes: `-drive file=<disk-path>,format=qcow2,if=virtio`
- Bind mounts: `-virtfs local,path=<host-path>,mount_tag=<tag>,security_model=passthrough`
- Devices/tags added in order to cloud-init config

### Cleanup

- `destroy` removes instance disks but preserves volumes
- Named volumes persist in `.qemu-compose/volumes/`
- Bind mounts are just host paths (no cleanup needed)
