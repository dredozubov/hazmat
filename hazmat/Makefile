GOFLAGS ?= -trimpath

# Installed paths (must match constants in main.go)
HAZMAT_BIN    ?= /usr/local/bin/hazmat
LAUNCH_HELPER ?= /usr/local/libexec/hazmat-launch

.PHONY: all hazmat hazmat-launch install install-helper clean

all: hazmat hazmat-launch

hazmat:
	go build $(GOFLAGS) -o hazmat .

hazmat-launch:
	go build $(GOFLAGS) -o hazmat-launch ./cmd/hazmat-launch

# Install both binaries.  Run 'sudo make install' or install each separately.
# hazmat-launch must be installed before 'hazmat setup' is run so the
# setup step can narrow the sudoers rule to the helper's path.
install: install-helper
	install -m 0755 hazmat $(HAZMAT_BIN)
	@echo "Installed $(HAZMAT_BIN)"

install-helper: hazmat-launch
	sudo install -d -m 0755 $$(dirname $(LAUNCH_HELPER))
	sudo install -m 0755 hazmat-launch $(LAUNCH_HELPER)
	@echo "Installed $(LAUNCH_HELPER)"

clean:
	rm -f hazmat hazmat-launch
