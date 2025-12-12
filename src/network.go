package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vishvananda/netlink"
)

// NetworkMetadata stores network configuration
type NetworkMetadata struct {
	Subnet        string `json:"subnet"`
	Driver        string `json:"driver"`
	DnsmasqUnit   string `json:"dnsmasq_unit,omitempty"`
	DnsmasqActive bool   `json:"dnsmasq_active,omitempty"`
}

// getNetworkMetadataPath returns the path to the networks metadata file
func getNetworkMetadataPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	qemuComposeDir := filepath.Join(cwd, ".qemu-compose")
	if err := os.MkdirAll(qemuComposeDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create .qemu-compose directory: %w", err)
	}

	return filepath.Join(qemuComposeDir, "networks.json"), nil
}

// loadNetworkMetadata loads the network metadata from disk
func loadNetworkMetadata() (map[string]NetworkMetadata, error) {
	metadataPath, err := getNetworkMetadataPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]NetworkMetadata), nil
		}
		return nil, fmt.Errorf("failed to read network metadata: %w", err)
	}

	var metadata map[string]NetworkMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse network metadata: %w", err)
	}

	return metadata, nil
}

// saveNetworkMetadata saves the network metadata to disk
func saveNetworkMetadata(metadata map[string]NetworkMetadata) error {
	metadataPath, err := getNetworkMetadataPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal network metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write network metadata: %w", err)
	}

	return nil
}

// allocateSubnet allocates a new subnet from the pool
func allocateSubnet() (string, error) {
	metadata, err := loadNetworkMetadata()
	if err != nil {
		return "", err
	}

	// Collect all allocated subnets
	allocatedSubnets := make(map[string]bool)
	for _, net := range metadata {
		allocatedSubnets[net.Subnet] = true
	}

	// Find first available subnet in the pool
	// Start from 172.16.0.0/24 and increment
	// This gives us 4096 possible /24 subnets (172.16.0.0 - 172.31.255.255)
	for i := 0; i < 4096; i++ {
		// Calculate subnet: 172.X.Y.0/24
		thirdOctet := 16 + (i / 256)
		fourthOctet := i % 256
		subnet := fmt.Sprintf("172.%d.%d.0/24", thirdOctet, fourthOctet)

		if !allocatedSubnets[subnet] {
			logger.Printf("Allocated subnet: %s", subnet)
			return subnet, nil
		}
	}

	return "", fmt.Errorf("no available subnets in pool (172.16.0.0/12)")
}

// resolveNetworkSubnet resolves the subnet for a network
// If subnet is "auto", allocates a new subnet from the pool
func resolveNetworkSubnet(networkName string, network Network) (string, error) {
	if network.Subnet != "auto" && network.Subnet != "" {
		return network.Subnet, nil
	}

	// Load existing metadata
	metadata, err := loadNetworkMetadata()
	if err != nil {
		return "", err
	}

	// Check if we already allocated a subnet for this network
	if existing, exists := metadata[networkName]; exists {
		logger.Printf("Reusing existing subnet for network %s: %s", networkName, existing.Subnet)
		return existing.Subnet, nil
	}

	// Allocate a new subnet
	subnet, err := allocateSubnet()
	if err != nil {
		return "", err
	}

	// Save the allocation
	metadata[networkName] = NetworkMetadata{
		Subnet: subnet,
		Driver: network.Driver,
	}

	if err := saveNetworkMetadata(metadata); err != nil {
		return "", fmt.Errorf("failed to save network metadata: %w", err)
	}

	logger.Printf("Allocated new subnet for network %s: %s", networkName, subnet)
	return subnet, nil
}

// getDnsmasqUnitName returns the systemd unit name for a network's dnsmasq instance
func getDnsmasqUnitName(networkName string) string {
	projectName := getProjectName()
	sanitizedProject := strings.ReplaceAll(projectName, " ", "-")
	sanitizedNetwork := strings.ReplaceAll(networkName, " ", "-")
	return fmt.Sprintf("qemu-compose-dnsmasq-%s-%s", sanitizedProject, sanitizedNetwork)
}

// getVMIPAddress returns the IP address assigned to a VM via DHCP
// Returns empty string if IP cannot be determined
func getVMIPAddress(vmName string, vm VM) string {
	// Only works for bridge networking
	if len(vm.Networks) == 0 {
		return ""
	}

	// Get the first network's dnsmasq unit
	networkName := vm.Networks[0]
	unitName := getDnsmasqUnitName(networkName)

	// Check if dnsmasq is running
	if !isDnsmasqRunning(networkName) {
		return ""
	}

	// Get dnsmasq logs to find DHCP lease
	cmd := exec.Command("sudo", "journalctl", "-u", unitName, "-n", "100", "--no-pager")
	output, err := cmd.Output()
	if err != nil {
		logger.Printf("Failed to get dnsmasq logs for %s: %v", networkName, err)
		return ""
	}

	// Parse logs for DHCP REPLY lines
	// Format: "dnsmasq-dhcp[PID]: DHCPREPLY(bridge) IP MAC hostname"
	lines := strings.Split(string(output), "\n")
	
	// Get TAP device MAC address to match against DHCP leases
	tapName := getTAPName(vmName, 0)
	tap, err := netlink.LinkByName(tapName)
	if err != nil {
		logger.Printf("Failed to get TAP device %s: %v", tapName, err)
		return ""
	}
	
	tapMAC := tap.Attrs().HardwareAddr.String()
	logger.Printf("Looking for DHCP lease for TAP %s with MAC %s", tapName, tapMAC)

	// Search for most recent DHCP reply for this MAC
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if !strings.Contains(line, "DHCPREPLY") && !strings.Contains(line, "DHCPACK") {
			continue
		}

		// Try to extract IP address from the log line
		// Format examples:
		// "dnsmasq-dhcp[123]: DHCPREPLY(qc-proj-net) 172.16.0.10 52:54:00:12:34:56"
		// "dnsmasq-dhcp[123]: DHCPACK(qc-proj-net) 172.16.0.10 52:54:00:12:34:56 hostname"
		
		parts := strings.Fields(line)
		for i, part := range parts {
			// Look for IP address pattern
			if net.ParseIP(part) != nil && i+1 < len(parts) {
				// Check if next field is a MAC address
				mac := strings.ToLower(parts[i+1])
				if strings.Contains(mac, ":") && strings.ToLower(tapMAC) == mac {
					logger.Printf("Found IP %s for VM %s (MAC: %s)", part, vmName, mac)
					return part
				}
			}
		}
	}

	logger.Printf("No DHCP lease found for VM %s", vmName)
	return ""
}

// startDnsmasq starts a dnsmasq instance for a network
func startDnsmasq(networkName string, subnet string) error {
	bridgeName := getBridgeName(networkName)
	unitName := getDnsmasqUnitName(networkName)

	logger.Printf("Starting dnsmasq for network %s (bridge: %s, subnet: %s)", networkName, bridgeName, subnet)

	// Check if dnsmasq is already running
	if isDnsmasqRunning(networkName) {
		logger.Printf("Dnsmasq already running for network: %s", networkName)
		return nil
	}

	// Parse subnet to get DHCP range
	ip, ipNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return fmt.Errorf("failed to parse subnet %s: %w", subnet, err)
	}

	// Calculate DHCP range: .10 to .250
	ip4 := ip.To4()
	if ip4 == nil {
		return fmt.Errorf("subnet %s is not a valid IPv4 address", subnet)
	}
	
	startIP := make(net.IP, 4)
	endIP := make(net.IP, 4)
	gateway := make(net.IP, 4)
	
	copy(startIP, ip4)
	copy(endIP, ip4)
	copy(gateway, ip4)
	
	startIP[3] = 10
	endIP[3] = 250
	gateway[3] = 1

	dhcpRange := fmt.Sprintf("%s,%s,12h", startIP.String(), endIP.String())

	// Get network mask
	ones, _ := ipNet.Mask.Size()
	_ = ones // We'll use the mask directly
	netmask := net.IP(ipNet.Mask).String()

	// Build dnsmasq command - requires sudo to bind to port 67 (DHCP)
	args := []string{
		"sudo",
		"systemd-run",
		"--system", // Use system manager (requires sudo)
		"--unit=" + unitName,
		"--description=qemu-compose dnsmasq for network: " + networkName,
		"--collect",
		"--property=KillMode=mixed",
		"--property=Type=simple",
		"dnsmasq",
		"--interface=" + bridgeName,
		"--bind-interfaces",
		"--dhcp-range=" + dhcpRange,
		"--dhcp-option=1," + netmask,          // Subnet mask
		"--dhcp-option=3," + gateway.String(), // Gateway
		"--dhcp-option=6," + gateway.String(), // DNS server (bridge IP)
		"--port=0",                             // Disable DNS
		"--leasefile-ro",                       // Don't write lease file (read-only mode)
		"--no-daemon",
		"--log-dhcp",
		"--log-facility=-", // Log to stderr (captured by systemd)
	}

	logger.Printf("Executing: %s", strings.Join(args, " "))

	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start dnsmasq (requires sudo): %w\nOutput: %s\n\nTo enable passwordless sudo for dnsmasq, run: qemu-compose doctor", err, string(output))
	}

	// Update metadata
	metadata, err := loadNetworkMetadata()
	if err != nil {
		return err
	}

	if netMeta, exists := metadata[networkName]; exists {
		netMeta.DnsmasqUnit = unitName
		netMeta.DnsmasqActive = true
		metadata[networkName] = netMeta
		if err := saveNetworkMetadata(metadata); err != nil {
			logger.Printf("Warning: failed to save dnsmasq metadata: %v", err)
		}
	}

	logger.Printf("Dnsmasq started successfully for network: %s (unit: %s)", networkName, unitName)
	return nil
}

// stopDnsmasq stops the dnsmasq instance for a network
func stopDnsmasq(networkName string) error {
	unitName := getDnsmasqUnitName(networkName)
	logger.Printf("Stopping dnsmasq for network: %s (unit: %s)", networkName, unitName)

	cmd := exec.Command("sudo", "systemctl", "stop", unitName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Don't fail if unit doesn't exist
		if !strings.Contains(string(output), "not loaded") && !strings.Contains(string(output), "not found") {
			logger.Printf("Warning: failed to stop dnsmasq unit %s: %v", unitName, err)
		}
	}

	// Update metadata
	metadata, err := loadNetworkMetadata()
	if err != nil {
		return err
	}

	if netMeta, exists := metadata[networkName]; exists {
		netMeta.DnsmasqActive = false
		metadata[networkName] = netMeta
		if err := saveNetworkMetadata(metadata); err != nil {
			logger.Printf("Warning: failed to save dnsmasq metadata: %v", err)
		}
	}

	logger.Printf("Dnsmasq stopped for network: %s", networkName)
	return nil
}

// isDnsmasqRunning checks if dnsmasq is running for a network
func isDnsmasqRunning(networkName string) bool {
	unitName := getDnsmasqUnitName(networkName)

	cmd := exec.Command("sudo", "systemctl", "is-active", unitName)
	output, err := cmd.Output()

	if err != nil {
		return false
	}

	return strings.TrimSpace(string(output)) == "active"
}

// setupNAT configures NAT/masquerading for a bridge network to enable internet access
func setupNAT(networkName string, subnet string) error {
	bridgeName := getBridgeName(networkName)
	logger.Printf("Setting up NAT for network %s (bridge: %s, subnet: %s)", networkName, bridgeName, subnet)

	// Enable IP forwarding
	cmd := exec.Command("sudo", "sysctl", "-w", "net.ipv4.ip_forward=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable IP forwarding: %w\nOutput: %s", err, string(output))
	}
	logger.Printf("IP forwarding enabled")

	// Add NAT rule (MASQUERADE)
	// Check if rule already exists first
	checkCmd := exec.Command("sudo", "iptables", "-t", "nat", "-C", "POSTROUTING", "-s", subnet, "-j", "MASQUERADE")
	if err := checkCmd.Run(); err != nil {
		// Rule doesn't exist, add it
		cmd = exec.Command("sudo", "iptables", "-t", "nat", "-A", "POSTROUTING", "-s", subnet, "-j", "MASQUERADE")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to add NAT rule: %w\nOutput: %s", err, string(output))
		}
		logger.Printf("Added NAT rule for subnet: %s", subnet)
	} else {
		logger.Printf("NAT rule already exists for subnet: %s", subnet)
	}

	// Allow forwarding from bridge
	checkCmd = exec.Command("sudo", "iptables", "-C", "FORWARD", "-i", bridgeName, "-j", "ACCEPT")
	if err := checkCmd.Run(); err != nil {
		cmd = exec.Command("sudo", "iptables", "-A", "FORWARD", "-i", bridgeName, "-j", "ACCEPT")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to add forward rule (input): %w\nOutput: %s", err, string(output))
		}
		logger.Printf("Added forward rule for bridge input: %s", bridgeName)
	}

	// Allow forwarding to bridge
	checkCmd = exec.Command("sudo", "iptables", "-C", "FORWARD", "-o", bridgeName, "-j", "ACCEPT")
	if err := checkCmd.Run(); err != nil {
		cmd = exec.Command("sudo", "iptables", "-A", "FORWARD", "-o", bridgeName, "-j", "ACCEPT")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to add forward rule (output): %w\nOutput: %s", err, string(output))
		}
		logger.Printf("Added forward rule for bridge output: %s", bridgeName)
	}

	logger.Printf("NAT setup completed for network: %s", networkName)
	return nil
}

// cleanupNAT removes NAT rules for a bridge network
func cleanupNAT(networkName string, subnet string) error {
	bridgeName := getBridgeName(networkName)
	logger.Printf("Cleaning up NAT for network %s (bridge: %s, subnet: %s)", networkName, bridgeName, subnet)

	// Remove NAT rule
	cmd := exec.Command("sudo", "iptables", "-t", "nat", "-D", "POSTROUTING", "-s", subnet, "-j", "MASQUERADE")
	if output, err := cmd.CombinedOutput(); err != nil {
		// Don't fail if rule doesn't exist
		if !strings.Contains(string(output), "does a matching rule exist") {
			logger.Printf("Warning: failed to remove NAT rule: %v", err)
		}
	}

	// Remove forward rules
	cmd = exec.Command("sudo", "iptables", "-D", "FORWARD", "-i", bridgeName, "-j", "ACCEPT")
	if output, err := cmd.CombinedOutput(); err != nil {
		if !strings.Contains(string(output), "does a matching rule exist") {
			logger.Printf("Warning: failed to remove forward rule (input): %v", err)
		}
	}

	cmd = exec.Command("sudo", "iptables", "-D", "FORWARD", "-o", bridgeName, "-j", "ACCEPT")
	if output, err := cmd.CombinedOutput(); err != nil {
		if !strings.Contains(string(output), "does a matching rule exist") {
			logger.Printf("Warning: failed to remove forward rule (output): %v", err)
		}
	}

	logger.Printf("NAT cleanup completed for network: %s", networkName)
	return nil
}

// createBridge creates a network bridge interface
func createBridge(networkName string, config *ComposeConfig) error {
	network, exists := config.Networks[networkName]
	if !exists {
		return fmt.Errorf("network not found in config: %s", networkName)
	}

	bridgeName := getBridgeName(networkName)
	logger.Printf("Creating bridge: %s", bridgeName)

	// Check if bridge already exists
	bridgeExists := true
	if _, err := netlink.LinkByName(bridgeName); err != nil {
		bridgeExists = false
	}

	if !bridgeExists {
		// Create bridge
		bridge := &netlink.Bridge{
			LinkAttrs: netlink.LinkAttrs{
				Name: bridgeName,
			},
		}

		if err := netlink.LinkAdd(bridge); err != nil {
			return fmt.Errorf("failed to create bridge %s: %w", bridgeName, err)
		}

		// Set bridge up
		if err := netlink.LinkSetUp(bridge); err != nil {
			return fmt.Errorf("failed to bring up bridge %s: %w", bridgeName, err)
		}
	} else {
		logger.Printf("Bridge already exists: %s", bridgeName)
	}

	// Resolve subnet (handles "auto" allocation)
	subnet, err := resolveNetworkSubnet(networkName, network)
	if err != nil {
		return fmt.Errorf("failed to resolve subnet for network %s: %w", networkName, err)
	}

	// Get bridge link
	bridge, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("failed to get bridge %s: %w", bridgeName, err)
	}

	// Assign IP address to bridge
	if subnet != "" {
		bridgeIPStr := getBridgeIP(subnet)
		addr, err := netlink.ParseAddr(bridgeIPStr)
		if err != nil {
			return fmt.Errorf("failed to parse bridge IP %s: %w", bridgeIPStr, err)
		}

		if err := netlink.AddrAdd(bridge, addr); err != nil {
			// Ignore error if address already exists
			if !strings.Contains(err.Error(), "file exists") {
				return fmt.Errorf("failed to assign IP to bridge %s: %w", bridgeName, err)
			}
		}
		logger.Printf("Assigned IP %s to bridge %s", bridgeIPStr, bridgeName)

		// Start dnsmasq for this network
		if err := startDnsmasq(networkName, subnet); err != nil {
			logger.Printf("Warning: failed to start dnsmasq for network %s: %v", networkName, err)
			// Don't fail bridge creation if dnsmasq fails
		}

		// Setup NAT for internet access
		if err := setupNAT(networkName, subnet); err != nil {
			logger.Printf("Warning: failed to setup NAT for network %s: %v", networkName, err)
			// Don't fail bridge creation if NAT setup fails
		}
	}

	logger.Printf("Bridge created successfully: %s", bridgeName)
	return nil
}

// deleteBridge removes a network bridge interface
func deleteBridge(networkName string) error {
	bridgeName := getBridgeName(networkName)
	logger.Printf("Deleting bridge: %s", bridgeName)

	// Stop dnsmasq first
	if err := stopDnsmasq(networkName); err != nil {
		logger.Printf("Warning: failed to stop dnsmasq for network %s: %v", networkName, err)
	}

	// Cleanup NAT rules
	metadata, err := loadNetworkMetadata()
	if err == nil {
		if netMeta, exists := metadata[networkName]; exists {
			if err := cleanupNAT(networkName, netMeta.Subnet); err != nil {
				logger.Printf("Warning: failed to cleanup NAT for network %s: %v", networkName, err)
			}
		}
	}

	// Check if bridge exists
	link, err := netlink.LinkByName(bridgeName)
	if err != nil {
		logger.Printf("Bridge does not exist: %s", bridgeName)
		return nil
	}

	// Set bridge down
	if err := netlink.LinkSetDown(link); err != nil {
		logger.Printf("Warning: failed to bring down bridge %s: %v", bridgeName, err)
	}

	// Delete bridge
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete bridge %s: %w", bridgeName, err)
	}

	logger.Printf("Bridge deleted successfully: %s", bridgeName)
	return nil
}

// createTAPDevice creates a TAP device for a VM
func createTAPDevice(vmName, networkName string, networkIndex int) (string, error) {
	tapName := getTAPName(vmName, networkIndex)
	logger.Printf("Creating TAP device: %s for VM: %s on network: %s", tapName, vmName, networkName)

	// Check if TAP already exists
	if _, err := netlink.LinkByName(tapName); err == nil {
		logger.Printf("TAP device already exists: %s", tapName)
		return tapName, nil
	}

	// Get current user ID
	uid := os.Getuid()
	gid := os.Getgid()

	// Create TAP device
	tap := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{
			Name: tapName,
		},
		Mode:  netlink.TUNTAP_MODE_TAP,
		Owner: uint32(uid),
		Group: uint32(gid),
	}

	if err := netlink.LinkAdd(tap); err != nil {
		return "", fmt.Errorf("failed to create TAP device %s: %w", tapName, err)
	}

	// Set TAP device up
	if err := netlink.LinkSetUp(tap); err != nil {
		return "", fmt.Errorf("failed to bring up TAP device %s: %w", tapName, err)
	}

	logger.Printf("TAP device created successfully: %s (owner: %d:%d)", tapName, uid, gid)
	return tapName, nil
}

// deleteTAPDevice removes a TAP device
func deleteTAPDevice(tapName string) error {
	logger.Printf("Deleting TAP device: %s", tapName)

	// Check if TAP exists
	link, err := netlink.LinkByName(tapName)
	if err != nil {
		logger.Printf("TAP device does not exist: %s", tapName)
		return nil
	}

	// Set TAP down
	if err := netlink.LinkSetDown(link); err != nil {
		logger.Printf("Warning: failed to bring down TAP device %s: %v", tapName, err)
	}

	// Delete TAP device
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete TAP device %s: %w", tapName, err)
	}

	logger.Printf("TAP device deleted successfully: %s", tapName)
	return nil
}

// attachTAPToBridge attaches a TAP device to a bridge
func attachTAPToBridge(tapName, networkName string) error {
	bridgeName := getBridgeName(networkName)
	logger.Printf("Attaching TAP device %s to bridge %s", tapName, bridgeName)

	// Get TAP device
	tap, err := netlink.LinkByName(tapName)
	if err != nil {
		return fmt.Errorf("failed to find TAP device %s: %w", tapName, err)
	}

	// Get bridge
	bridge, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("failed to find bridge %s: %w", bridgeName, err)
	}

	// Attach TAP to bridge
	if err := netlink.LinkSetMaster(tap, bridge); err != nil {
		return fmt.Errorf("failed to attach TAP %s to bridge %s: %w", tapName, bridgeName, err)
	}

	logger.Printf("TAP device attached successfully: %s -> %s", tapName, bridgeName)
	return nil
}

// getBridgeName returns the bridge interface name for a network
func getBridgeName(networkName string) string {
	projectName := getProjectName()
	// Sanitize names for network interfaces (max 15 chars, alphanumeric + dash)
	sanitizedProject := strings.ReplaceAll(projectName, " ", "-")
	sanitizedNetwork := strings.ReplaceAll(networkName, " ", "-")

	// Keep it short to fit in 15 char limit
	bridgeName := fmt.Sprintf("qc-%s-%s", sanitizedProject, sanitizedNetwork)
	if len(bridgeName) > 15 {
		// Truncate if too long
		bridgeName = bridgeName[:15]
	}

	return bridgeName
}

// getTAPName returns the TAP device name for a VM using hash-based naming with VM name suffix
func getTAPName(vmName string, networkIndex int) string {
	projectName := getProjectName()

	// Create a unique identifier combining project, VM name, and network index
	identifier := fmt.Sprintf("%s-%s-%d", projectName, vmName, networkIndex)

	// Generate MD5 hash and take first 4 characters for uniqueness
	hash := fmt.Sprintf("%x", md5.Sum([]byte(identifier)))[:4]

	// Sanitize VM name
	sanitizedVM := strings.ReplaceAll(vmName, " ", "-")

	// Format: tap-<hash>-<vmname> (try to fit in 15 char limit)
	// Reserve 9 chars for "tap-" + hash + "-", leaving 6 for VM name
	maxVMLen := 6
	if len(sanitizedVM) > maxVMLen {
		sanitizedVM = sanitizedVM[:maxVMLen]
	}

	return fmt.Sprintf("tap-%s-%s", hash, sanitizedVM)
}

// getBridgeIP returns the bridge IP address from a subnet
// For example: "192.168.100.0/24" -> "192.168.100.1/24"
func getBridgeIP(subnet string) string {
	parts := strings.Split(subnet, "/")
	if len(parts) != 2 {
		return subnet
	}

	ip := net.ParseIP(parts[0])
	if ip == nil {
		return subnet
	}

	// Convert to 4-byte representation
	ip = ip.To4()
	if ip == nil {
		return subnet
	}

	// Set last octet to 1 for bridge IP
	ip[3] = 1

	return fmt.Sprintf("%s/%s", ip.String(), parts[1])
}

// setupVMNetworks creates all network infrastructure for a VM
func setupVMNetworks(vmName string, vm VM, config *ComposeConfig) error {
	if len(vm.Networks) == 0 {
		logger.Printf("No networks configured for VM: %s, will use user-mode networking", vmName)
		return nil
	}

	logger.Printf("Setting up %d network(s) for VM: %s", len(vm.Networks), vmName)

	for i, networkName := range vm.Networks {
		// Create bridge if it doesn't exist
		if err := createBridge(networkName, config); err != nil {
			return fmt.Errorf("failed to create bridge for network %s: %w", networkName, err)
		}

		// Create TAP device
		tapName, err := createTAPDevice(vmName, networkName, i)
		if err != nil {
			return fmt.Errorf("failed to create TAP device for network %s: %w", networkName, err)
		}

		// Attach TAP to bridge
		if err := attachTAPToBridge(tapName, networkName); err != nil {
			return fmt.Errorf("failed to attach TAP to bridge for network %s: %w", networkName, err)
		}
	}

	logger.Printf("Network setup completed for VM: %s", vmName)
	return nil
}

// cleanupVMNetworks removes all network infrastructure for a VM
func cleanupVMNetworks(vmName string, vm VM) error {
	if len(vm.Networks) == 0 {
		logger.Printf("No networks configured for VM: %s, nothing to clean up", vmName)
		return nil
	}

	logger.Printf("Cleaning up %d network(s) for VM: %s", len(vm.Networks), vmName)

	for i := range vm.Networks {
		tapName := getTAPName(vmName, i)
		if err := deleteTAPDevice(tapName); err != nil {
			logger.Printf("Warning: failed to delete TAP device %s: %v", tapName, err)
		}
	}

	logger.Printf("Network cleanup completed for VM: %s", vmName)
	return nil
}

// cleanupProjectNetworks removes all bridges for a project
func cleanupProjectNetworks(config *ComposeConfig) error {
	if len(config.Networks) == 0 {
		logger.Printf("No networks defined in project")
		return nil
	}

	logger.Printf("Cleaning up %d project network(s)", len(config.Networks))

	for networkName := range config.Networks {
		if err := deleteBridge(networkName); err != nil {
			logger.Printf("Warning: failed to delete bridge for network %s: %v", networkName, err)
		}
	}

	logger.Printf("Project network cleanup completed")
	return nil
}
