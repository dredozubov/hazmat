VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS  ?= -trimpath
LDFLAGS  := -X main.version=$(VERSION)

# Installed paths (must match constants in main.go)
HAZMAT_BIN    ?= /usr/local/bin/hazmat
LAUNCH_HELPER ?= /usr/local/libexec/hazmat-launch

.PHONY: all hazmat hazmat-launch install install-helper clean test lint e2e e2e-bootstrap e2e-vm e2e-stack-matrix e2e-stack-matrix-detect e2e-stack-matrix-smoke hooks

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

lint:
	golangci-lint run ./...

e2e:
	@if [ "$(E2E_ACK)" != "1" ]; then \
		echo "Refusing to run destructive host lifecycle test." >&2; \
		echo "Use 'make e2e E2E_ACK=1' or prefer 'bash ../scripts/e2e-vm.sh --quick'." >&2; \
		exit 1; \
	fi
	HAZMAT_E2E_ACK_DESTRUCTIVE=1 bash ../scripts/e2e.sh

e2e-bootstrap:
	bash ../scripts/e2e-bootstrap.sh

e2e-vm: all
	bash ../scripts/e2e-vm.sh

e2e-stack-matrix:
	bash ../scripts/e2e-stack-matrix.sh $(STACKCHECK_ARGS)

e2e-stack-matrix-detect:
	bash ../scripts/e2e-stack-matrix.sh --detect $(STACKCHECK_ARGS)

e2e-stack-matrix-smoke:
	bash ../scripts/e2e-stack-matrix.sh --smoke $(STACKCHECK_ARGS)

hooks:
	cp ../scripts/pre-push ../.git/hooks/pre-push
	chmod +x ../.git/hooks/pre-push
	@echo "Installed pre-push hook (vet + test + lint + smoke tests)"

clean:
	rm -f hazmat hazmat-launch
