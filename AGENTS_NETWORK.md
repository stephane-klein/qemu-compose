# Network Management Architecture

## Overview

*qemu-compose* supports two networking modes: user-mode (default) and bridge networking with DHCP.

## User-Mode Networking

- QEMU's built-in user-mode networking (`-netdev user`)
- Automatic SSH port forwarding: host port 2222+ → guest port 22
- No special privileges required
- VMs cannot communicate with each other
- Used when `networks:` is not specified in VM config

## Bridge Networking

### Architecture

```
VM1 (TAP: tap-xxxx-vm1) ─┐
VM2 (TAP: tap-xxxx-vm2) ─┼─ Bridge (qc-project-network) ─ dnsmasq (DHCP/DNS)
VM3 (TAP: tap-xxxx-vm3) ─┘
```

### Components

#### Bridge Interface

- Created with netlink: `netlink.LinkAdd(&netlink.Bridge{...})`
- Named: `qc-<project>-<network>` (max 15 chars)
- Assigned IP: `.1` of subnet (e.g., 172.16.0.1 for 172.16.0.0/24)
- Brought up with `netlink.LinkSetUp()`

#### TAP Devices

- Created per VM per network: `netlink.LinkAdd(&netlink.Tuntap{...})`
- Named: `tap-<hash>-<vmname>` (max 15 chars, hash ensures uniqueness)
- Owner: current user (UID/GID)
- Attached to bridge: `netlink.LinkSetMaster(tap, bridge)`
- MAC address: Generated from MD5(project-vm-networkindex)

#### dnsmasq DHCP/DNS Server

- Runs as systemd user unit: `qemu-compose-dnsmasq-<project>-<network>`
- Binds to bridge interface
- DHCP range: `.10` to `.250` of subnet
- DNS: Provides hostname resolution for VMs on same network
- Metadata stored in `.qemu-compose/networks.json`

### Subnet Allocation

- When `subnet: auto`, allocates from `172.16.0.0/12` pool (4096 possible /24 subnets)
- Allocation stored in `.qemu-compose/networks.json` for reuse across restarts
- Manual subnets supported: `subnet: 192.168.100.0/24`

### NAT/Masquerading

- Enables IP forwarding: `sysctl -w net.ipv4.ip_forward=1`
- Adds iptables rules for MASQUERADE and FORWARD
- Allows VMs to access external networks

### Network Metadata (networks.json)

```json
{
  "network-name": {
    "subnet": "172.16.0.0/24",
    "driver": "bridge",
    "dnsmasq_unit": "qemu-compose-dnsmasq-project-network",
    "dnsmasq_active": true
  }
}
```

## Implementation Details

### Bridge Creation Flow

1. `createBridge(networkName, config)` called during `up`
2. Check if bridge exists with `netlink.LinkByName()`
3. Create bridge with `netlink.LinkAdd()`
4. Bring up with `netlink.LinkSetUp()`
5. Resolve subnet (handle `auto` allocation)
6. Assign IP to bridge with `netlink.AddrAdd()`
7. Start dnsmasq with `systemd-run --user`
8. Setup NAT with iptables

### TAP Device Creation Flow

1. `createTAPDevice(vmName, networkName, networkIndex)` called during `up`
2. Generate unique TAP name using hash
3. Create with `netlink.LinkAdd(&netlink.Tuntap{...})`
4. Set owner to current user
5. Bring up with `netlink.LinkSetUp()`
6. Attach to bridge with `netlink.LinkSetMaster()`

### Cleanup Flow

1. `stopVM()` calls `cleanupVMNetworks()` to delete TAP devices
2. `destroy` (all VMs) calls `deleteBridge()` for each network
3. `deleteBridge()` stops dnsmasq, removes NAT rules, deletes bridge
4. Network metadata updated to reflect changes

## QEMU Integration

- TAP devices passed to QEMU: `-netdev tap,id=net0,ifname=tap-xxxx-vm1,script=no,downscript=no`
- Virtual NIC added: `-device virtio-net-pci,netdev=net0,mac=52:54:00:xx:xx:xx`
- MAC address ensures cloud-init network configuration matches correct interface

## Capabilities Required

- `CAP_NET_ADMIN`: Required for bridge/TAP operations
- Grant with: `sudo setcap cap_net_admin+ep $(which qemu-compose)`
- Or run with sudo: `sudo qemu-compose up`
