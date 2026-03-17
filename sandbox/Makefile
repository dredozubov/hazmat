GOFLAGS ?= -trimpath

# Installed paths (must match constants in main.go)
SANDBOX_BIN   ?= /usr/local/bin/sandbox
LAUNCH_HELPER ?= /usr/local/libexec/sandbox-launch

.PHONY: all sandbox sandbox-launch install install-helper clean

all: sandbox sandbox-launch

sandbox:
	go build $(GOFLAGS) -o sandbox .

sandbox-launch:
	go build $(GOFLAGS) -o sandbox-launch ./cmd/sandbox-launch

# Install both binaries.  Run 'sudo make install' or install each separately.
# sandbox-launch must be installed before 'sandbox setup' is run so the
# setup step can narrow the sudoers rule to the helper's path.
install: install-helper
	install -m 0755 sandbox $(SANDBOX_BIN)
	@echo "Installed $(SANDBOX_BIN)"

install-helper: sandbox-launch
	sudo install -d -m 0755 $$(dirname $(LAUNCH_HELPER))
	sudo install -m 0755 sandbox-launch $(LAUNCH_HELPER)
	@echo "Installed $(LAUNCH_HELPER)"

clean:
	rm -f sandbox sandbox-launch
