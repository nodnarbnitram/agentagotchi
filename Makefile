PYTHON ?= python3
GO ?= go
PLUGIN_VALIDATOR ?= $(HOME)/.codex/skills/.system/plugin-creator/scripts/validate_plugin.py
VENV_DIR ?= work/venv
VENV_PYTHON := $(VENV_DIR)/bin/python
VENV_STAMP := $(VENV_DIR)/.dev-deps

.PHONY: all python-deps assets build-host firmware-build firmware-test plugin-validate provision-env test

all: test build-host

$(VENV_STAMP): requirements-dev.txt
	$(PYTHON) -m venv "$(VENV_DIR)"
	"$(VENV_PYTHON)" -m pip install --disable-pip-version-check -r requirements-dev.txt
	touch "$(VENV_STAMP)"

python-deps: $(VENV_STAMP)

assets: $(VENV_STAMP)
	"$(VENV_PYTHON)" tools/build_pet_assets.py

build-host:
	mkdir -p work/bin work/gocache
	GOCACHE="$(CURDIR)/work/gocache" GOOS=darwin GOARCH=arm64 \
		$(GO) build -trimpath -o work/bin/agentagotchi ./cmd/agentagotchi

provision-env: build-host
	scripts/provision-from-env.sh

firmware-build:
	idf.py -C firmware build

firmware-test:
	mkdir -p work/tests
	cc -std=c11 -Wall -Wextra -Werror \
		firmware/tests/test_sensor_math.c firmware/main/sensor_math.c \
		-lm -o work/tests/test_sensor_math
	work/tests/test_sensor_math

plugin-validate: $(VENV_STAMP)
	@test -f "$(PLUGIN_VALIDATOR)" || { \
		echo "Plugin validator not found: $(PLUGIN_VALIDATOR)" >&2; \
		echo "Install/use Codex plugin-creator or set PLUGIN_VALIDATOR." >&2; \
		exit 1; \
	}
	"$(VENV_PYTHON)" "$(PLUGIN_VALIDATOR)" plugin/agentagotchi-status

test: assets firmware-test
	mkdir -p work/gocache
	GOCACHE="$(CURDIR)/work/gocache" $(GO) test ./...
	GOCACHE="$(CURDIR)/work/gocache" $(GO) vet ./...
	"$(VENV_PYTHON)" -m unittest tools/test_pet_assets.py -v
	"$(VENV_PYTHON)" -m unittest tools/test_release_contracts.py -v
	$(MAKE) plugin-validate
