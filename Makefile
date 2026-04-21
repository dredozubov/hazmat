VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS  ?= -trimpath
LDFLAGS  := -X main.version=$(VERSION)
HOST_PLATFORM := $(shell uname -s | tr '[:upper:]' '[:lower:]')
INSTALL_PLATFORM ?= $(HOST_PLATFORM)

APP_DIR               := hazmat
HAZMAT_BUILD_BIN      := $(APP_DIR)/hazmat
HAZMAT_BUILD_HELPER   := $(APP_DIR)/hazmat-launch
USER_PREFIX           ?= $(HOME)/.local
USER_BINDIR           ?= $(USER_PREFIX)/bin
USER_LIBEXECDIR       ?= $(USER_PREFIX)/libexec
SYSTEM_PREFIX         ?= /usr/local
SYSTEM_BINDIR         ?= $(SYSTEM_PREFIX)/bin
SYSTEM_LIBEXECDIR     ?= $(SYSTEM_PREFIX)/libexec
USER_HAZMAT_BIN       ?= $(USER_BINDIR)/hazmat
USER_LAUNCH_HELPER    ?= $(USER_LIBEXECDIR)/hazmat-launch
SYSTEM_HAZMAT_BIN     ?= $(SYSTEM_BINDIR)/hazmat
SYSTEM_LAUNCH_HELPER  ?= $(SYSTEM_LIBEXECDIR)/hazmat-launch

.PHONY: all hazmat hazmat-launch check-install-platform install install-system install-helper uninstall uninstall-system clean test linux-compile lint e2e e2e-bootstrap e2e-vm e2e-stack-matrix e2e-stack-matrix-detect e2e-stack-matrix-smoke test-entrypoint-guards check-hostexec hooks

all: hazmat hazmat-launch

hazmat:
	@rm -f $(HAZMAT_BUILD_BIN)
	cd $(APP_DIR) && go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o hazmat .

hazmat-launch:
	@rm -f $(HAZMAT_BUILD_HELPER)
	cd $(APP_DIR) && CGO_ENABLED=1 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o hazmat-launch ./cmd/hazmat-launch

check-install-platform:
	@platform=$$(printf '%s' "$(INSTALL_PLATFORM)" | tr '[:upper:]' '[:lower:]'); \
	if [ "$$platform" != "darwin" ]; then \
		echo "error: install artifacts are only supported for darwin right now." >&2; \
		echo "Linux setup/rollback resources must be modeled in MC_SetupRollback and implemented before Linux install artifacts are enabled." >&2; \
		exit 1; \
	fi

# Install both Darwin binaries under ~/.local without sudo.
install: check-install-platform hazmat hazmat-launch
	install -d -m 0755 $(USER_BINDIR)
	install -d -m 0755 $(USER_LIBEXECDIR)
	install -m 0755 $(HAZMAT_BUILD_BIN) $(USER_HAZMAT_BIN)
	install -m 0755 $(HAZMAT_BUILD_HELPER) $(USER_LAUNCH_HELPER)
	@echo "Installed $(USER_HAZMAT_BIN)"
	@echo "Installed $(USER_LAUNCH_HELPER)"

# Install both Darwin binaries under /usr/local. Run with sudo when needed.
install-system: check-install-platform hazmat hazmat-launch
	install -d -m 0755 $(SYSTEM_BINDIR)
	install -d -m 0755 $(SYSTEM_LIBEXECDIR)
	install -m 0755 $(HAZMAT_BUILD_BIN) $(SYSTEM_HAZMAT_BIN)
	install -m 0755 $(HAZMAT_BUILD_HELPER) $(SYSTEM_LAUNCH_HELPER)
	@echo "Installed $(SYSTEM_HAZMAT_BIN)"
	@echo "Installed $(SYSTEM_LAUNCH_HELPER)"

# Backward-compatible helper-only system install for setup workflows.
install-helper: check-install-platform hazmat-launch
	install -d -m 0755 $(SYSTEM_LIBEXECDIR)
	install -m 0755 $(HAZMAT_BUILD_HELPER) $(SYSTEM_LAUNCH_HELPER)
	@echo "Installed $(SYSTEM_LAUNCH_HELPER)"

uninstall:
	rm -f $(USER_HAZMAT_BIN)
	rm -f $(USER_LAUNCH_HELPER)
	@echo "Uninstalled $(USER_HAZMAT_BIN) and $(USER_LAUNCH_HELPER)"

uninstall-system:
	rm -f $(SYSTEM_HAZMAT_BIN)
	rm -f $(SYSTEM_LAUNCH_HELPER)
	@echo "Uninstalled $(SYSTEM_HAZMAT_BIN) and $(SYSTEM_LAUNCH_HELPER)"

test:
	cd $(APP_DIR) && go test ./...

linux-compile:
	bash scripts/check-linux-compile.sh

lint:
	cd $(APP_DIR) && golangci-lint run ./...

e2e:
	@if [ "$(E2E_ACK)" != "1" ]; then \
		echo "Refusing to run destructive host lifecycle test." >&2; \
		echo "Use 'make e2e E2E_ACK=1' or prefer 'bash scripts/e2e-vm.sh --quick'." >&2; \
		exit 1; \
	fi
	HAZMAT_E2E_ACK_DESTRUCTIVE=1 bash scripts/e2e.sh

e2e-bootstrap:
	bash scripts/e2e-bootstrap.sh

e2e-vm: all
	bash scripts/e2e-vm.sh

e2e-stack-matrix:
	bash scripts/e2e-stack-matrix.sh $(STACKCHECK_ARGS)

e2e-stack-matrix-detect:
	bash scripts/e2e-stack-matrix.sh --detect $(STACKCHECK_ARGS)

e2e-stack-matrix-smoke:
	bash scripts/e2e-stack-matrix.sh --smoke $(STACKCHECK_ARGS)

test-entrypoint-guards:
	bash scripts/test-entrypoint-guards.sh

check-hostexec:
	bash scripts/check-hostexec.sh

hooks:
	install -m 0755 scripts/pre-commit .git/hooks/pre-commit
	install -m 0755 scripts/pre-push .git/hooks/pre-push
	@echo "Installed pre-commit and pre-push hooks"

clean:
	rm -f $(HAZMAT_BUILD_BIN) $(HAZMAT_BUILD_HELPER)
