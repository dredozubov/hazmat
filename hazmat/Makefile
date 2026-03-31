VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS  ?= -trimpath
LDFLAGS  := -X main.version=$(VERSION)

# Installed paths (must match constants in main.go)
HAZMAT_BIN    ?= /usr/local/bin/hazmat
LAUNCH_HELPER ?= /usr/local/libexec/hazmat-launch

.PHONY: all hazmat hazmat-launch install install-helper clean test e2e e2e-vm

all: hazmat hazmat-launch

hazmat:
	@rm -f hazmat
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o hazmat .

hazmat-launch:
	@rm -f hazmat-launch
	CGO_ENABLED=1 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o hazmat-launch ./cmd/hazmat-launch

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

test:
	go test ./...

e2e: all
	bash ../scripts/e2e.sh

e2e-vm: all
	bash ../scripts/e2e-vm.sh

clean:
	rm -f hazmat hazmat-launch
