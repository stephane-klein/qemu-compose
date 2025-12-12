![qemu-compose logo](./logo.png)

# qemu-compose

_qemu-compose_ is an attempt to implement a [`docker-compose`](https://docs.docker.com/compose/) 
equivalent for running [QEMU](https://www.qemu.org) VMs on Linux.

This project is still in the prototype stage.

## Why I created this project

I've been using [Vagrant](https://developer.hashicorp.com/vagrant) since 2012 and appreciate its approach,
but I find the `Vagrantfile` format (Ruby) less intuitive than `docker-compose.yaml`.
The modern infrastructure tooling ecosystem (Docker, Podman, Kubernetes, Terraform...) has adopted Go as the
standard for system tools, while Vagrant remains rooted in Ruby.
  
I've also explored [libvirt](https://libvirt.org/) with `virt-manager` and `virt-install`. While powerful,
these tools use verbose XML and their commands are far removed from native QEMU options. I was missing a simple,
declarative configuration format like `docker-compose.yaml`.
  
**qemu-compose** attempts to fill this gap: a readable YAML format for orchestrating QEMU VMs,
without excessive abstraction layers, implemented in Go.

## Prerequisites

### Installing Mise on Fedora Workstation

This project uses [Mise](https://mise.jdx.dev/) to manage the Go toolchain and project dependencies.

To install Mise on Fedora Workstation:

```bash
# Install mise using the official installer
$ curl https://mise.run | sh

# Add mise to your shell (for bash)
$ echo 'eval "$(~/.local/bin/mise activate bash)"' >> ~/.bashrc
$ source ~/.bashrc

# Or for zsh
$ echo 'eval "$(~/.local/bin/mise activate zsh)"' >> ~/.zshrc
$ source ~/.zshrc
```

Once Mise is installed, navigate to the project directory and run:

```bash
$ mise trust
$ mise install
```

This will automatically install the Go toolchain version specified in `.mise.toml`.

## Getting Started

Build the project:

```bash
$ mise run build
[build] $ go build -o qemu-compose ./src
```

Run the binary:

```bash
$ qemu-compose --help
qemu-compose is a CLI tool to orchestrate QEMU virtual machines using a declarative YAML configuration.

Usage:
  qemu-compose [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  console     Attach to a VM's serial console
  destroy     Stop and remove VMs
  doctor      Check system dependencies
  help        Help about any command
  image       Manage images
  inspect     Display detailed information about a VM
  network     Manage networks
  ps          List VMs
  pull        Pull VM images
  ssh         Connect to a VM via SSH
  stop        Stop VMs
  up          Create and start VMs

Flags:
      --debug             Enable debug logging (can also use QEMU_COMPOSE_DEBUG=true)
  -f, --file string       Specify an alternate compose file (default: qemu-compose.yaml or qemu-compose.yml)
  -h, --help              help for qemu-compose

Use "qemu-compose [command] --help" for more information about a command.
```

### Specifying a Compose File

There are three ways to specify which compose file to use, in order of precedence:

1. **Command-line flag** (highest priority):
   ```bash
   $ qemu-compose -f /path/to/my-compose.yaml up
   ```

2. **Environment variable**:
   ```bash
   $ export QEMU_COMPOSE_FILE=/path/to/my-compose.yaml
   $ qemu-compose up
   ```

3. **Default files** (lowest priority):
   - `qemu-compose.yaml`
   - `qemu-compose.yml`

When using Mise for development, the `QEMU_COMPOSE_FILE` environment variable is automatically set to `./examples/qemu-compose.yaml` (see `.mise.toml`).

### Checking System Dependencies

Verify that all required dependencies are installed:

```bash
$ qemu-compose doctor
Checking system dependencies...

✅ Operating System: Linux
✅ systemd: found at /usr/bin/systemctl
✅ systemd-run: found at /usr/bin/systemd-run
✅ QEMU: found at /usr/bin/qemu-system-x86_64
✅ qemu-img: found at /usr/bin/qemu-img
✅ genisoimage: found at /usr/bin/genisoimage
✅ ssh-keygen: found at /usr/bin/ssh-keygen
✅ dnsmasq: found at /usr/bin/dnsmasq
✅ ip: found at /usr/bin/ip
✅ CAP_NET_ADMIN: granted via capability on /path/to/qemu-compose

✅ All system dependencies are satisfied!
```

### Managing Images

#### Listing Cached Images

List all VM base images stored in the local cache:

```bash
$ qemu-compose image ls
Image cache directory: /home/user/.local/share/qemu-compose/images

FILENAME                                           SIZE            PATH
------------------------------------------------------------------------------------------------------------------------
Fedora-Cloud-Base-Generic-42-1.1.x86_64.qcow2     450.2 MB        /home/user/.local/share/qemu-compose/images/Fedora-Cloud-Base-Generic-42-1.1.x86_64.qcow2
noble-server-cloudimg-amd64.img                   890.5 MB        /home/user/.local/share/qemu-compose/images/noble-server-cloudimg-amd64.img

Total: 2 image(s)
```

The `image ls` command displays:
- The cache directory location (`~/.local/share/qemu-compose/images/`)
- A table with filename, human-readable size, and full path for each cached image
- Total count of cached images

This is useful for:
- Checking which images are already downloaded
- Finding the full path to cached images
- Verifying disk space usage
- Cleaning up unused images manually

#### Pulling VM Images

Download VM images defined in your compose file:

```bash
$ qemu-compose pull
Pulling 2 image(s) from qemu-compose.yaml
Target directory: /home/user/.local/share/qemu-compose/images/

fedora-vm            100% |████████████████| (1.2 GB/1.2 GB, 45 MB/s)
ubuntu-vm            100% |████████████████| (890 MB/890 MB, 42 MB/s)

✓ All images pulled successfully
```

Or pull a specific image by URL:

```bash
$ qemu-compose pull https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img
Pulling image: https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img
Target directory: /home/user/.local/share/qemu-compose/images/

image                100% |████████████████| (890 MB/890 MB, 42 MB/s)
```

Force re-download even if the image already exists:

```bash
$ qemu-compose pull --force
```

Images are cached in `~/.local/share/qemu-compose/images/` and won't be re-downloaded if they already exist (unless you use the `--force` flag).

### Starting VMs

Start all VMs defined in your compose file:

```bash
$ qemu-compose up
Using compose file: qemu-compose.yaml
Project: myproject
Starting 2 VM(s)...

VM: fedora-vm
  ✓ Started (unit: qemu-compose-myproject-fedora-vm)
  Networking: bridge mode (networks: default)
  Note: VM will obtain IP via DHCP on the bridge network
  View logs: journalctl --user -u qemu-compose-myproject-fedora-vm -f
  Attach to console: qemu-compose console fedora-vm

VM: ubuntu-vm
  ✓ Started (unit: qemu-compose-myproject-ubuntu-vm)
  Networking: bridge mode (networks: default)
  Note: VM will obtain IP via DHCP on the bridge network
  View logs: journalctl --user -u qemu-compose-myproject-ubuntu-vm -f
  Attach to console: qemu-compose console ubuntu-vm

✓ All VMs started successfully
```

VMs are managed by systemd as user units. Each VM runs in its own systemd unit with a predictable name pattern: `qemu-compose-<project>-<vm-name>`.

### Volume Support

qemu-compose supports two types of volumes:

#### Named Volumes

Named volumes are persistent storage managed by qemu-compose. They are created as qcow2 disk images, pre-formatted with ext4, and automatically mounted in VMs.

Example configuration:

```yaml
volumes:
  postgres_data:
    size: 20G
  app_data:
    size: 10G

vms:
  database:
    image: https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img
    cpu: 4
    memory: 4096
    volumes:
      # Short form
      - postgres_data:/var/lib/postgresql/data
      
      # Long form with read-only
      - source: app_data
        target: /mnt/shared
        read_only: true
```

Named volumes:
- Are stored in `.qemu-compose/volumes/<volume-name>/`
- Persist across VM lifecycles (survive `destroy` unless explicitly removed)
- Are pre-formatted with ext4 filesystem
- Default to 10G if size is not specified
- Are always auto-mounted (cannot be disabled)
- Can be mounted read-only

#### Bind Mounts

Bind mounts allow you to mount host directories or files directly into VMs using 9p (virtio-9p) filesystem sharing.

Example configuration:

```yaml
vms:
  web:
    image: https://download.fedoraproject.org/pub/fedora/linux/releases/42/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-42-1.1.x86_64.qcow2
    cpu: 2
    memory: 2048
    volumes:
      # Short form - auto-mounted by default
      - ./config:/etc/myapp
      
      # Short form with read-only flag
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      
      # Long form with auto-mount disabled
      - source: ./scripts
        target: /opt/scripts
        automount: false
      
      # Long form with custom mount options
      - source: ./cache
        target: /var/cache/app
        automount: true
        mount_options: "cache=loose,msize=104857600"
```

Bind mounts:
- Use 9p (virtio-9p) filesystem sharing
- Support both relative and absolute paths
- **Relative paths are resolved relative to the compose file location**, not the current working directory
- Are auto-mounted by default via cloud-init
- Can disable auto-mount with `automount: false` for manual mounting
- Can specify custom 9p mount options
- Can be mounted read-only
- Require matching UIDs between host and guest (uses `security_model=passthrough`)

#### Volume Specification Formats

**Short Form (String):**

```yaml
volumes:
  # Named volume
  - volume_name:/path/in/vm
  
  # Bind mount (relative path)
  - ./host/path:/path/in/vm
  
  # Bind mount (absolute path)
  - /abs/host/path:/path/in/vm
  
  # With read-only flag
  - ./config:/etc/app:ro
```

**Long Form (Map):**

```yaml
volumes:
  - source: volume_name          # Named volume or host path
    target: /path/in/vm          # Mount path in VM (must be absolute)
    read_only: false             # Optional, default: false
    automount: true              # Optional, default: true (bind mounts only)
    mount_options: ""            # Optional, custom 9p options (bind mounts only)
```

**Long Form Options:**

- `source` (required): Named volume name or host path (relative or absolute)
- `target` (required): Mount path inside VM (must be absolute, starting with `/`)
- `read_only` (optional): Boolean, default `false`. Makes the mount read-only
- `automount` (optional): Boolean, default `true`. Only applies to bind mounts. When `false`, the volume is attached but not automatically mounted via cloud-init
- `mount_options` (optional): String, custom 9p mount options. Only applies to bind mounts. Example: `"cache=loose,msize=104857600"`

**Auto-mount Behavior:**

- **Named volumes:** Always auto-mounted via cloud-init (cannot be disabled)
- **Bind mounts:** Auto-mounted by default, can be disabled with `automount: false`
- When `automount: false`, the volume is still attached to QEMU with `-virtfs`, but cloud-init won't generate mount commands
- Users can manually mount with: `mount -t 9p -o trans=virtio,version=9p2000.L <mount_tag> <target>`

**Important**: When using relative paths in bind mounts (e.g., `./config`), the path is resolved relative to the directory containing the compose YAML file, not the directory where you run the `qemu-compose` command. This ensures consistent behavior regardless of where you invoke the command.

The mount path inside the VM must always be an absolute path starting with `/`.

### Networking Modes

qemu-compose supports two networking modes:

#### User-Mode Networking (Default)

When no `networks:` are specified in the VM configuration, qemu-compose uses QEMU's user-mode networking with automatic SSH port forwarding. This mode:

- Requires no special privileges
- Automatically allocates SSH ports starting from 2222
- Provides easy SSH access via `qemu-compose ssh <vm-name>`
- VMs cannot communicate directly with each other

Example:

```yaml
vms:
  myvm:
    image: https://download.fedoraproject.org/pub/fedora/linux/releases/42/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-42-1.1.x86_64.qcow2
    cpu: 2
    memory: 2048
    # No networks specified = user-mode networking
```

#### Bridge Networking with DHCP

When `networks:` are specified, qemu-compose creates TAP devices and bridges for VM networking, and automatically starts a dnsmasq instance per network to provide DHCP and DNS services. This mode:

- Requires CAP_NET_ADMIN capability or sudo privileges
- VMs automatically obtain IP addresses via DHCP
- VMs can communicate directly with each other
- DNS resolution between VMs using hostnames
- More production-like networking setup

Example with manual subnet:

```yaml
networks:
  frontend:
    driver: bridge
    subnet: 192.168.100.0/24

vms:
  web:
    image: https://download.fedoraproject.org/pub/fedora/linux/releases/42/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-42-1.1.x86_64.qcow2
    cpu: 2
    memory: 2048
    networks:
      - frontend
```

Example with automatic subnet allocation:

```yaml
networks:
  frontend:
    driver: bridge
    subnet: auto  # Automatically allocates a subnet from 172.16.0.0/12 pool

vms:
  web:
    image: https://download.fedoraproject.org/pub/fedora/linux/releases/42/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-42-1.1.x86_64.qcow2
    cpu: 2
    memory: 2048
    networks:
      - frontend
```

**Automatic Subnet Allocation:**

When `subnet: auto` is specified, qemu-compose automatically allocates a unique subnet from the `172.16.0.0/12` pool (172.16.0.0 - 172.31.255.255). This provides 4096 possible /24 subnets.

Allocated subnets are stored in `.qemu-compose/networks.json` and reused across restarts, ensuring consistency. Each network gets a unique subnet, avoiding conflicts.

**DHCP and DNS Services:**

Each network automatically gets its own dnsmasq instance running as a systemd user unit (`qemu-compose-dnsmasq-<project>-<network>`). This provides:

- **DHCP**: Automatic IP address assignment to VMs
- **DNS**: Hostname resolution between VMs on the same network
- **Isolation**: Each network has its own DHCP/DNS namespace

The dnsmasq instance:
- Binds to the bridge interface for the network
- Serves DHCP from a calculated range within the subnet
- Provides DNS resolution for VM hostnames
- Runs under the user's systemd session (no root required)

**Granting Network Capabilities:**

To use bridge networking without sudo, grant the CAP_NET_ADMIN capability:

```bash
$ sudo setcap cap_net_admin+ep $(which qemu-compose)
```

Or run qemu-compose with sudo:

```bash
$ sudo qemu-compose up
```

### Cloud-init Configuration

qemu-compose automatically configures cloud-init for supported cloud images. The default credentials are:

- **Fedora Cloud images**: username `fedora`, password `password`
- **Ubuntu Cloud images**: username `ubuntu`, password `password`
- **Debian Cloud images**: username `debian`, password `password`
- **CentOS Cloud images**: username `centos`, password `password`
- **RHEL Cloud images**: username `cloud-user`, password `password`

The OS type is automatically detected from the image URL. All users are configured with passwordless sudo access.

### SSH Access

qemu-compose automatically generates an SSH key pair for each project and configures VMs to accept it. The SSH key is stored at `.qemu-compose/ssh/id_ed25519`.

#### Connecting via SSH (User-Mode Networking)

For VMs using user-mode networking, connect using the `ssh` command:

```bash
$ qemu-compose ssh fedora-vm
```

This automatically:
- Uses the project SSH key at `.qemu-compose/ssh/id_ed25519`
- Connects to the correct port (allocated when the VM started)
- Uses the appropriate default user for the VM's OS (fedora, ubuntu, debian, etc.)

You can also connect manually using the SSH command shown in the `up` output:

```bash
$ ssh -i .qemu-compose/ssh/id_ed25519 -p 2222 fedora@localhost
```

#### Connecting via SSH (Bridge Networking)

For VMs using bridge networking, you need to connect directly to the VM's IP address on the bridge network. The `qemu-compose ssh` command is not available for bridge-networked VMs.

First, find the VM's IP address (via console or DHCP logs), then connect:

```bash
$ ssh -i .qemu-compose/ssh/id_ed25519 fedora@<vm-ip-address>
```

The VM will have obtained its IP address automatically via DHCP from the dnsmasq instance running on the bridge network.

### Viewing VM Logs

View logs for a specific VM using journalctl:

```bash
$ journalctl --user -u qemu-compose-myproject-fedora-vm -f
```

View logs for a dnsmasq instance:

```bash
$ journalctl --user -u qemu-compose-dnsmasq-myproject-default -f
```

### Attaching to VM Console

Attach to a running VM's serial console with full read/write access:

```bash
$ qemu-compose console fedora-vm
Connected to VM console: fedora-vm
Press Ctrl+] to detach

[You can now interact with the VM's console]
```

This provides direct access to the VM's serial console. You can:
- View boot messages
- Login to the VM (use the default credentials listed above)
- Execute commands
- Press Ctrl+] to detach from the console

Note: The console connects via a Unix socket created when the VM starts at `.qemu-compose/<vm-name>/console.sock`.

### Listing VMs

List all VMs and their status:

```bash
$ qemu-compose ps
Using compose file: qemu-compose.yaml
Project: myproject

NAME                 STATUS          IP ADDRESS      CPU        MEMORY     DISK       SYSTEMD UNIT
--------------------------------------------------------------------------------------------------------------
fedora-vm            ready           172.16.0.10     2          2048       8G         qemu-compose-myproject-fedora-vm
ubuntu-vm            starting        172.16.0.11     2          2048       8G         qemu-compose-myproject-ubuntu-vm
```

The `STATUS` column shows the current state of each VM:

- **not-created**: VM instance has not been created yet (no disk exists)
- **stopped**: VM instance exists but is not running
- **starting**: VM is running but SSH is not yet accessible (still booting/provisioning)
- **ready**: VM is running and SSH is accessible
- **active**: VM is running (shown when SSH readiness check is skipped)
- **unknown**: Status could not be determined

The `IP ADDRESS` column shows the IP address for VMs using bridge networking. For user-mode networking VMs, it shows `-`.

The `DISK` column shows the allocated disk size for each VM instance. VMs that haven't been created yet show `-` for the disk size.

**Note**: The `ps` command checks SSH connectivity in parallel using goroutines to determine if VMs are ready. This may add a small delay (up to 2 seconds per VM) but provides accurate status information.

### Inspecting VMs

Display detailed information about a specific VM:

```bash
$ qemu-compose inspect fedora-vm
VM: fedora-vm
Project: myproject
================================================================================

Status:
  State: ready
  Systemd Unit: qemu-compose-myproject-fedora-vm

Configuration:
  CPU: 2
  Memory: 2048 MB
  Image: https://download.fedoraproject.org/pub/fedora/linux/releases/42/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-42-1.1.x86_64.qcow2
  OS Type: fedora
  Default User: fedora

Disk:
  Size: 8G
  Instance Disk: /path/to/.qemu-compose/fedora-vm/disk.qcow2
  Base Image: /home/user/.local/share/qemu-compose/images/Fedora-Cloud-Base-Generic-42-1.1.x86_64.qcow2
  Cloud-Init ISO: /path/to/.qemu-compose/fedora-vm/cloud-init.iso

Networks:
  - default:
      Driver: bridge
      Bridge: qc-myproject-default
      TAP Device: tap-myproject-fedora-vm-0
      Subnet: 172.16.0.0/24
      DHCP: running
  IP Address: 172.16.0.10

Volumes:
  - ./volumes/fedora/ -> /mnt/ (bind)
      Host Path: /path/to/volumes/fedora/
      Automount: true

Console:
  Socket: /path/to/.qemu-compose/fedora-vm/console.sock
  Attach: qemu-compose console fedora-vm

Logs:
  View: journalctl --user -u qemu-compose-myproject-fedora-vm -f
```

The `inspect` command displays comprehensive information about a VM including:

- **Status**: Current state and systemd unit name
- **Configuration**: CPU, memory, image URL, OS type, default user
- **Disk**: Disk size, instance disk path, base image path, cloud-init ISO path
- **Networks**: Network configuration, bridge names, TAP devices, subnets, DHCP status, IP address
- **Ports**: Port mappings (if configured)
- **Volumes**: Volume mounts with type (bind/named), paths, sizes, mount options
- **Environment**: Environment variables (if configured)
- **Provisioning**: Provisioning scripts (if configured)
- **Dependencies**: VM dependencies (if configured)
- **Console**: Console socket path and attach command
- **Logs**: Command to view logs

You can also output the information in JSON format for programmatic use:

```bash
$ qemu-compose inspect fedora-vm --format json
{
  "name": "fedora-vm",
  "project": "myproject",
  "status": "ready",
  "cpu": 2,
  "memory": 2048,
  "image": "https://download.fedoraproject.org/pub/fedora/linux/releases/42/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-42-1.1.x86_64.qcow2",
  "os_type": "fedora",
  "default_user": "fedora",
  ...
}
```

### Stopping VMs

Stop all VMs without removing their instance disks:

```bash
$ qemu-compose stop
Using compose file: qemu-compose.yaml
Project: myproject
Stopping 2 VM(s)...

VM: fedora-vm
  ✓ Stopped

VM: ubuntu-vm
  ✓ Stopped

✓ All VMs stopped successfully
```

The instance disks remain in `.qemu-compose/<vm-name>/` and can be reused when you run `up` again.

For VMs using bridge networking, TAP devices are automatically cleaned up when the VM is stopped. Network bridges and dnsmasq instances remain running.

### Destroying VMs

Stop all VMs and remove their instance disks:

```bash
$ qemu-compose destroy
Using compose file: qemu-compose.yaml
Project: myproject
Stopping and removing 2 VM(s)...

VM: fedora-vm
  ✓ Stopped
  ✓ Instance disk removed

VM: ubuntu-vm
  ✓ Stopped
  ✓ Instance disk removed

✓ All VMs stopped and removed successfully
```

This removes the `.qemu-compose/<vm-name>/` directories. Base images in `~/.local/share/qemu-compose/images/` are preserved.

Network bridges, dnsmasq instances, and subnet allocations are not automatically removed. They persist across VM lifecycles and can be reused by other VMs in the same project. To clean up network resources, manually delete `.qemu-compose/networks.json` and stop dnsmasq units:

```bash
$ systemctl --user stop qemu-compose-dnsmasq-myproject-default
```

Named volumes are also preserved and must be explicitly removed if desired.

### Managing VMs with systemctl

Since VMs are managed by systemd, you can also use standard systemctl commands:

```bash
# Check VM status
$ systemctl --user status qemu-compose-myproject-fedora-vm

# Stop a VM
$ systemctl --user stop qemu-compose-myproject-fedora-vm

# Restart a VM
$ systemctl --user restart qemu-compose-myproject-fedora-vm

# View logs
$ journalctl --user -u qemu-compose-myproject-fedora-vm -f
```

You can also manage dnsmasq instances:

```bash
# Check dnsmasq status
$ systemctl --user status qemu-compose-dnsmasq-myproject-default

# Stop dnsmasq
$ systemctl --user stop qemu-compose-dnsmasq-myproject-default

# View dnsmasq logs
$ journalctl --user -u qemu-compose-dnsmasq-myproject-default -f
```

### Debug Mode

Enable debug logging to see detailed execution information:

```bash
# Using the --debug flag
$ qemu-compose --debug up

# Using environment variable
$ QEMU_COMPOSE_DEBUG=true qemu-compose up
```

## How It Works

### Directory Structure

**Base Images Cache:**

- Location: `~/.local/share/qemu-compose/images/`
- Purpose: Store downloaded base VM images (qcow2, img files)
- Scope: Global, shared across all projects

**VM Instance Disks:**

- Location: `./.qemu-compose/<vm-name>/`
- Purpose: Store VM instance-specific disk images (COW overlays), cloud-init ISO, console socket, and SSH keys
- Scope: Project-local, one directory per VM

**Project SSH Keys:**

- Location: `./.qemu-compose/ssh/`
- Purpose: Store project-specific SSH key pair (id_ed25519 and id_ed25519.pub)
- Scope: Project-local, shared across all VMs in the project

**Network Metadata:**

- Location: `./.qemu-compose/networks.json`
- Purpose: Store allocated subnets for networks with `subnet: auto` and dnsmasq state
- Scope: Project-local, persists across VM lifecycles

**Volume Storage:**

- Location: `./.qemu-compose/volumes/<volume-name>/`
- Purpose: Store named volume disk images (qcow2 files)
- Scope: Project-local, persists across VM lifecycles

**Volume Metadata:**

- Location: `./.qemu-compose/volumes.json`
- Purpose: Store metadata about named volumes (size, disk path, creation time)
- Scope: Project-local, persists across VM lifecycles

### VM Lifecycle

1. **Pull**: Downloads base images to `~/.local/share/qemu-compose/images/`
2. **Up**: 
   - Generates project SSH key pair if it doesn't exist
   - Creates COW overlay disks in `.qemu-compose/<vm-name>/disk.qcow2`
   - Creates named volumes if they don't exist
   - Validates bind mount paths (relative to compose file location)
   - Generates cloud-init ISO with SSH key configuration and volume mounts
   - For user-mode networking: Allocates SSH port for the VM
   - For bridge networking: Creates bridges and TAP devices, allocates subnets if needed, starts dnsmasq instances
   - Starts VMs using `systemd-run`
3. **SSH**: Connects to a running VM using the project SSH key and allocated port (user-mode networking only)
4. **Console**: Connects to the VM's serial console via Unix socket at `.qemu-compose/<vm-name>/console.sock`
5. **Inspect**: Displays detailed information about a VM's configuration, status, networks, volumes, and runtime state
6. **Stop**: Stops VMs, cleans up TAP devices (bridge networking), but keeps instance disks, volumes, bridges, and dnsmasq instances
7. **Destroy**: Stops VMs and removes instance disks (`.qemu-compose/<vm-name>/`), but keeps volumes

Each VM runs as a systemd user unit, providing:

- Process lifecycle management
- Automatic logging via journalctl
- Resource control via cgroups
- Clean shutdown handling

Each network's dnsmasq instance runs as a systemd user unit (`qemu-compose-dnsmasq-<project>-<network>`), providing:

- Automatic DHCP service for IP address assignment
- DNS resolution for VM hostnames
- Isolated networking per network
- Managed lifecycle (starts with network creation, stops with network destruction)

### VM Status Detection

The `ps` command determines VM readiness by:

1. Checking if the VM instance exists (disk created)
2. Querying systemd for the unit's active state
3. Testing SSH connectivity with a 2-second timeout (user-mode networking only)
4. Running status checks in parallel using goroutines for better performance

This provides accurate, real-time status information about whether VMs are ready to accept SSH connections.

### Network Architecture

**User-Mode Networking:**

- QEMU's built-in user-mode networking (`-netdev user`)
- Automatic port forwarding for SSH (host port 2222+ → guest port 22)
- No special privileges required
- Isolated VMs (cannot communicate with each other)

**Bridge Networking:**

- Linux bridge interfaces (created with `ip link add type bridge`)
- TAP devices for each VM network interface (created with `ip tuntap add`)
- TAP devices attached to bridges (using `ip link set master`)
- dnsmasq instance per network for DHCP and DNS services
- VMs obtain IP addresses automatically via DHCP
- Requires CAP_NET_ADMIN capability or sudo

**Automatic Subnet Allocation:**

- When `subnet: auto` is specified, subnets are allocated from the `172.16.0.0/12` pool
- Allocated subnets are stored in `.qemu-compose/networks.json`
- Subnets are reused across restarts for consistency
- Each network gets a unique /24 subnet (e.g., 172.16.0.0/24, 172.16.1.0/24, etc.)
- Bridge interface gets .1 address (e.g., 172.16.0.1 for 172.16.0.0/24)

**DHCP Configuration:**

- dnsmasq binds to the bridge interface for each network
- DHCP range is automatically calculated from the subnet (e.g., .2 to .254)
- DNS forwarding is disabled for network isolation
- Each VM gets a unique IP address from the DHCP pool
- Hostnames are resolved via dnsmasq's built-in DNS server

Bridge and TAP device naming:
- Bridges: `qc-<project>-<network>` (max 15 chars)
- TAP devices: `tap-<project>-<vm>-<index>` (max 15 chars)

dnsmasq unit naming:
- Units: `qemu-compose-dnsmasq-<project>-<network>`

### Volume Architecture

**Named Volumes:**

- Created as qcow2 disk images in `.qemu-compose/volumes/<volume-name>/`
- Pre-formatted with ext4 filesystem using qemu-nbd and mkfs.ext4
- Attached to VMs as virtio-blk devices (`/dev/vdb`, `/dev/vdc`, etc.)
- Automatically mounted via cloud-init at specified paths
- Persist across VM lifecycles (survive `destroy`)
- Metadata tracked in `.qemu-compose/volumes.json`

**Bind Mounts:**

- Use 9p (virtio-9p) filesystem sharing
- Attached to VMs via QEMU's `-virtfs` option
- Mount tags: `mount0`, `mount1`, etc.
- Security model: `passthrough` (requires matching UIDs)
- Automatically mounted via cloud-init using 9p filesystem type (when `automount: true`)
- Relative paths resolved relative to compose file location
- Absolute paths used as-is
- Can be disabled from auto-mounting with `automount: false`

## Development approach

This project is an experiment in AI-assisted development. I implemented it using [Aider](https://aider.chat/) with
the Claude Sonnet 4.5 model. This is my first time building a project this way.

See the [`AGENTS.md`](AGENTS.md) file for the guidelines I provided to the AI during development.

### A few numbers

For version `0.1.0` of `qemu-compose`, it took:

- 12 hours of human work
- 270 iterations (messages sent to Sonnet 4.5)
- Total tokens sent: 8,605,300
- Total tokens received: 804,891
- Total cost: $37.8600
