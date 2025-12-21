# Contributing to qemu-compose

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

## Getting Started

Once Mise is installed, clone the repository:

```
$ git clone https://github.com/qemu-compose/qemu-compose.git
$ cd qemu-compose
```

and run:

```bash
$ mise trust
$ mise install
```

This will automatically install the Go toolchain version specified in `.mise.toml`.

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

Start some Virtual Machines:

```bash
$ qemu-compose -f examples/qemu-compose.yaml up
...

$ qemu-compose -f examples/qemu-compose.yaml ps
```

Teardown:

```bash
$ qemu-compose -f examples/qemu-compose.yaml destroy
```
