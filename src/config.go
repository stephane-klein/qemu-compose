package main

import (
	"fmt"
	"strings"
)

// ComposeConfig represents the root structure of qemu-compose.yaml
type ComposeConfig struct {
	Version  string             `yaml:"version"`
	Networks map[string]Network `yaml:"networks,omitempty"`
	Volumes  map[string]Volume  `yaml:"volumes,omitempty"`
	VMs      map[string]VM      `yaml:"vms"`
}

// Network represents a network configuration
type Network struct {
	Driver string `yaml:"driver"`
	Subnet string `yaml:"subnet"`
}

// Volume represents a volume configuration
type Volume struct {
	Size string `yaml:"size,omitempty"` // Size for named volumes (e.g., "10G", "100G")
}

// VM represents a virtual machine configuration
type VM struct {
	Image       string        `yaml:"image"`
	CPU         int           `yaml:"cpu"`
	Memory      int           `yaml:"memory"`
	Networks    []string      `yaml:"networks,omitempty"`
	Ports       []string      `yaml:"ports,omitempty"`
	DependsOn   []string      `yaml:"depends_on,omitempty"`
	Volumes     []VolumeMount `yaml:"volumes,omitempty"`
	Environment []string      `yaml:"environment,omitempty"`
	Provision   []Provision   `yaml:"provision,omitempty"`
	Disk        *Disk         `yaml:"disk,omitempty"`
	Healthcheck *Healthcheck  `yaml:"healthcheck,omitempty"`
	SSH         *SSH          `yaml:"ssh,omitempty"`
}

// VolumeMount represents a volume mount specification
// It can be unmarshaled from either a string (short form) or a map (long form)
type VolumeMount struct {
	Source       string `yaml:"source"`
	Target       string `yaml:"target"`
	ReadOnly     bool   `yaml:"read_only,omitempty"`
	Automount    *bool  `yaml:"automount,omitempty"`
	MountOptions string `yaml:"mount_options,omitempty"`
}

// UnmarshalYAML implements custom unmarshaling for VolumeMount
// Supports both short form (string) and long form (map)
func (v *VolumeMount) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try to unmarshal as string (short form)
	var shortForm string
	if err := unmarshal(&shortForm); err == nil {
		return v.parseShortForm(shortForm)
	}

	// Try to unmarshal as map (long form)
	type volumeMountAlias VolumeMount
	var longForm volumeMountAlias
	if err := unmarshal(&longForm); err != nil {
		return err
	}

	*v = VolumeMount(longForm)
	return nil
}

// parseShortForm parses the short form volume syntax
// Format: <source>:<target>[:<flags>]
// Flags: ro (read-only)
func (v *VolumeMount) parseShortForm(spec string) error {
	parts := strings.Split(spec, ":")

	if len(parts) < 2 {
		return fmt.Errorf("invalid volume spec: %s (expected format: source:target or source:target:flags)", spec)
	}

	v.Source = parts[0]
	v.Target = parts[1]
	v.ReadOnly = false
	v.Automount = nil // Use default (true)
	v.MountOptions = ""

	// Parse optional flags
	if len(parts) >= 3 {
		for _, flag := range parts[2:] {
			switch flag {
			case "ro":
				v.ReadOnly = true
			default:
				return fmt.Errorf("unknown volume flag: %s", flag)
			}
		}
	}

	return nil
}

// Provision represents provisioning configuration
type Provision struct {
	Type   string `yaml:"type"`
	Inline string `yaml:"inline,omitempty"`
}

// Disk represents disk configuration
type Disk struct {
	Size string `yaml:"size"`
}

// Healthcheck represents healthcheck configuration
type Healthcheck struct {
	Test     []string `yaml:"test"`
	Interval string   `yaml:"interval"`
	Timeout  string   `yaml:"timeout"`
	Retries  int      `yaml:"retries"`
}

// SSH represents SSH configuration
type SSH struct {
	Port int `yaml:"port,omitempty"` // Optional: manual port override
}
