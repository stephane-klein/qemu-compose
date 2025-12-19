# qemu-compose.yaml file specification

## File Format

- Format: YAML 3.0
- Default filenames: `qemu-compose.yaml` or `qemu-compose.yml`
- Location: Project root directory
- Relative paths in volumes: Resolved from compose file directory

## Root Structure

```yaml
version: "1.0"
vms:
  <vm-name>: <VM>

networks: # Optional
  <network-name>: <Network>

volumes: # Optional
  <volume-name>: <Volume>
```

## Network Object

```yaml
networks:
  frontend:
    driver: bridge                    # Required: "bridge" (only option currently)
    subnet: auto                      # Optional: "auto" or CIDR (e.g., "192.168.100.0/24")
                                      # Default: "auto" (allocates from 172.16.0.0/12)
```

### Subnet Allocation

- `subnet: auto`: Allocates unique /24 from 172.16.0.0/12 pool
- `subnet: 192.168.100.0/24`: Manual subnet specification
- Allocations stored in `.qemu-compose/networks.json` for reuse

## Volume Object

```yaml
volumes:
  postgres_data:
    size: 20G                         # Optional: disk size (default: "10G")
                                      # Examples: "5G", "100G", "1T"
```

## VM Object

```yaml
vms:
  fedora-vm:
    image: <string>                   # Required: HTTP/HTTPS URL or local path
    cpu: <int>                        # Required: number of vCPUs
    memory: <int>                     # Required: RAM in MB
    disk:                             # Optional: disk configuration
      size: <string>                  # Disk size (e.g., "8G", "50G")
                                      # default value is 10Go
    networks:                         # Optional: list of network names
      - frontend
      - backend
    volumes:                          # Optional: volume mounts
      - <VolumeMount>
```

The 10G default disk size is not a size problem because QCOW2 images allocate disk space dynamically
on the host as the VM actually uses it, rather than reserving the full amount upfront.

## VolumeMount Object

```yaml
volumes:
  - source: volume_name               # Required: volume name or host path
    target: /path/in/vm               # Required: mount path (must be absolute)
    read_only: false                  # Optional: default false
    automount: true                   # Optional: default true (bind mounts only)
    mount_options: ""                 # Optional: custom 9p options (bind mounts only)
```

### Volume Detection

- If source contains `/`, `\`, or starts with `.` → bind mount
- Otherwise → named volume reference

### Bind Mount Options

- `automount: true` (default): Cloud-init auto-mounts via 9p
- `automount: false`: Volume attached but not auto-mounted (manual mount required)
- `mount_options`: Custom 9p mount options (e.g., `"cache=loose,msize=104857600"`)
- `read_only: true`: Mount as read-only

## Complete Example

```yaml
version: "1.0"
networks:
  default:
    driver: bridge
    subnet: auto

volumes:
  vm1_data:
    size: 20G

vms:
  fedora-vm:
    image: https://download.fedoraproject.org/pub/fedora/linux/releases/42/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-42-1.1.x86_64.qcow2
    cpu: 2
    memory: 2048
    disk:
      size: 5G
    networks:
      - default
    volumes:
      - vm1_data:/var/lib/

  ubuntu-vm:
    image: https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img
    cpu: 2
    memory: 2048
    networks:
      - default
    volumes:
      - ./volumes/ubuntu/:/mnt/

```

## Notes

- All paths in volumes are relative to the compose file directory
- VM names and network names must be valid systemd unit names (alphanumeric + dash)
- Image URLs must be HTTP/HTTPS (local paths not yet supported)
- Default cloud-init users: fedora, ubuntu, debian, centos, cloud-user (detected from image URL)
- All VMs get passwordless sudo access
- SSH key pair generated automatically in `.qemu-compose/ssh/`
